package perf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// SubscribeWorkerEnv, when set, makes rpc_perf a worker process that reads some connections of a
// subscription test for its parent: see RunSubscribeWorker.
const SubscribeWorkerEnv = "RPC_PERF_SUBSCRIBE_WORKER"

// heavyConnections tells, for each connection of plan, whether it subscribes to large notifications:
// transactionReceipts, or logs without a filter.
func heavyConnections(plan []clientConnection, lines []subscriptionLine) []bool {
	heavy := make([]bool, len(plan))
	for i, p := range plan {
		for _, j := range p.lines {
			if l := lines[j]; l.typ == subReceipts || (l.typ == subLogs && len(l.filter) == 0) {
				heavy[i] = true
			}
		}
	}
	return heavy
}

// splitCPUs gives half of cpus, rounded down, to the light connections and the rest to the heavy ones, or
// nothing when there are fewer than two. The light connections are many and their notifications arrive
// in a burst at each head, so their reads need as many cores as the few heavy connections need to parse.
func splitCPUs(cpus []int) (light, heavy []int) {
	if len(cpus) < 2 {
		return nil, nil
	}
	n := len(cpus) / 2
	return cpus[:n], cpus[n:]
}

// parseWorkersOn is the parse workers of a process pinned to cpus: one per core, as parseWorkers.
func parseWorkersOn(cpus []int) int {
	if len(cpus) == 0 {
		return parseWorkers
	}
	return len(cpus)
}

// collectSplit is collect with the heavy connections read by a worker process pinned to cpus of their
// own, so that parsing their notifications does not delay the arrival of the light ones.
func collectSplit(ctx context.Context, url string, reqs [][]byte, lines []subscriptionLine, received []connection, heavy []bool, lightCPUs, heavyCPUs []int, duration time.Duration) (time.Time, time.Time, error) {
	subs := subscriptions(received, len(reqs))
	var near, far []int // the connections read here and by the worker
	for i, h := range heavy {
		if h {
			far = append(far, i)
		} else {
			near = append(near, i)
		}
	}
	w, err := startWorker(ctx, workerSpec{URL: url, Reqs: reqs, Subs: pick(subs, far), CPUs: heavyCPUs})
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	defer w.stop()
	sockets, ids, rejected, err := subscribeAll(ctx, url, reqs, pick(subs, near))
	defer closeAll(sockets)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	var ready workerReady
	if err := w.receive(&ready); err != nil {
		return time.Time{}, time.Time{}, err
	}
	if ready.Error != "" {
		return time.Time{}, time.Time{}, errors.New(ready.Error)
	}
	restore, err := pinProcess(lightCPUs)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	defer restore()

	start := time.Now()
	end := start.Add(duration)
	if err := w.enc.Encode(workerStart{Deadline: end.Add(notificationDrain)}); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("subscribe worker: %w", err)
	}
	listened := listenAll(ctx, sockets, ids, lines, end.Add(notificationDrain), parseWorkersOn(lightCPUs))
	if ctx.Err() != nil {
		return time.Time{}, time.Time{}, ctx.Err()
	}
	var result workerResult
	if err := w.receive(&result); err != nil {
		return time.Time{}, time.Time{}, err
	}
	for k, i := range near {
		received[i].rejected, received[i].notes, received[i].closed = rejected[k], listened[k].notes, listened[k].closed
	}
	for k, i := range far {
		c := result.Conns[k]
		received[i].pid, received[i].rejected, received[i].closed = result.Pid, ready.Rejected[k], c.Closed
		for _, n := range c.Notes {
			received[i].notes = append(received[i].notes, n.notification())
		}
	}
	return start, end, nil
}

func pick[T any](items []T, indexes []int) []T {
	out := make([]T, len(indexes))
	for k, i := range indexes {
		out[k] = items[i]
	}
	return out
}

// The messages of a worker: the parent sends workerSpec, the worker answers workerReady once subscribed,
// the parent sends workerStart, the worker answers workerResult after the deadline.
type workerSpec struct {
	URL  string   `json:"url"`
	Reqs [][]byte `json:"reqs"`
	Subs [][]int  `json:"subs"` // the subscriptions of each connection
	CPUs []int    `json:"cpus"`
}

type workerReady struct {
	Rejected []map[int]string `json:"rejected"`
	Error    string           `json:"error,omitempty"`
}

type workerStart struct {
	Deadline time.Time `json:"deadline"`
}

type workerResult struct {
	Pid   int          `json:"pid"`
	Conns []workerConn `json:"conns"`
}

type workerConn struct {
	Notes  []wireNote `json:"notes"`
	Closed bool       `json:"closed"`
}

// wireNote is a notification as a worker sends it: times in Unix nanoseconds, 0 for no block timestamp.
type wireNote struct {
	Sub        int    `json:"s"`
	Arrival    int64  `json:"a"`
	Number     uint64 `json:"n"`
	BlockHash  string `json:"h"`
	Key        string `json:"k"`
	Timestamp  int64  `json:"t,omitempty"`
	Receipts   int    `json:"r,omitempty"`
	PerReceipt bool   `json:"p,omitempty"`
}

func newWireNote(n notification) wireNote {
	w := wireNote{Sub: n.sub, Arrival: n.arrival.UnixNano(), Number: n.number, BlockHash: n.blockHash, Key: n.key, Receipts: n.receipts, PerReceipt: n.perReceipt}
	if !n.timestamp.IsZero() {
		w.Timestamp = n.timestamp.UnixNano()
	}
	return w
}

func (w wireNote) notification() notification {
	n := notification{sub: w.Sub, arrival: time.Unix(0, w.Arrival), number: w.Number, blockHash: w.BlockHash, key: w.Key, receipts: w.Receipts, perReceipt: w.PerReceipt}
	if w.Timestamp != 0 {
		n.timestamp = time.Unix(0, w.Timestamp)
	}
	return n
}

type worker struct {
	cmd *exec.Cmd
	enc *json.Encoder
	dec *json.Decoder
}

// startWorker runs this executable again as a subscribe worker and sends it spec.
func startWorker(ctx context.Context, spec workerSpec) (*worker, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("subscribe worker: %w", err)
	}
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), SubscribeWorkerEnv+"=1")
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("subscribe worker: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("subscribe worker: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("subscribe worker: %w", err)
	}
	w := &worker{cmd: cmd, enc: json.NewEncoder(stdin), dec: json.NewDecoder(stdout)}
	if err := w.enc.Encode(spec); err != nil {
		w.stop()
		return nil, fmt.Errorf("subscribe worker: %w", err)
	}
	return w, nil
}

func (w *worker) receive(v any) error {
	if err := w.dec.Decode(v); err != nil {
		return fmt.Errorf("subscribe worker: %w", err)
	}
	return nil
}

func (w *worker) stop() {
	w.cmd.Process.Kill() //nolint:errcheck
	w.cmd.Wait()         //nolint:errcheck
}

// RunSubscribeWorker is the subscribe worker of a parent rpc_perf, talking over in and out: it opens and
// reads the connections of the spec it receives, on the CPUs of the spec.
func RunSubscribeWorker(in io.Reader, out io.Writer) error {
	dec, enc := json.NewDecoder(in), json.NewEncoder(out)
	var spec workerSpec
	if err := dec.Decode(&spec); err != nil {
		return fmt.Errorf("subscribe worker spec: %w", err)
	}
	if _, err := pinProcess(spec.CPUs); err != nil {
		return err
	}
	lines, err := subscriptionLines(spec.Reqs)
	if err != nil {
		return err
	}
	ctx := context.Background()
	sockets, ids, rejected, err := subscribeAll(ctx, spec.URL, spec.Reqs, spec.Subs)
	defer closeAll(sockets)
	ready := workerReady{Rejected: rejected}
	if err != nil {
		ready.Error = err.Error()
	}
	if err := enc.Encode(ready); err != nil || ready.Error != "" {
		return err
	}
	var start workerStart
	if err := dec.Decode(&start); err != nil {
		return fmt.Errorf("subscribe worker start: %w", err)
	}
	result := workerResult{Pid: os.Getpid()}
	for _, c := range listenAll(ctx, sockets, ids, lines, start.Deadline, parseWorkersOn(spec.CPUs)) {
		wc := workerConn{Closed: c.closed, Notes: make([]wireNote, len(c.notes))}
		for k, n := range c.notes {
			wc.Notes[k] = newWireNote(n)
		}
		result.Conns = append(result.Conns, wc)
	}
	return enc.Encode(result)
}
