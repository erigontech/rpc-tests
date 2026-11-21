package perf

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const (
	errMissing      = "missing notification"
	errDuplicate    = "duplicate notification"
	errClosed       = "connection closed"
	errNoTimestamp  = "no block timestamp"
	errGranularity  = "more than one notification per block"
	errReceiptCount = "wrong receipt count"

	// notificationDrain is how long the notifications of a block first seen before the end of a test are
	// still awaited, so that a slow subscription is not counted as missing.
	notificationDrain = 2 * time.Second
)

const (
	subNewHeads = "newHeads"
	subLogs     = "logs"
	subReceipts = "transactionReceipts"
)

// subscriptionLine is one eth_subscribe request of a pattern.
type subscriptionLine struct {
	typ    string
	filter map[string]any // the filter of a logs subscription
	group  int            // the first line with the same params: the fan-out compares the subscriptions of a group
}

// subscriptionLines returns the type, and the filter of a logs subscription, of each eth_subscribe request.
func subscriptionLines(reqs [][]byte) ([]subscriptionLine, error) {
	lines := make([]subscriptionLine, len(reqs))
	groups := map[string]int{} // params, re-encoded with sorted keys -> first line
	for i, req := range reqs {
		var r struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(req, &r); err != nil || r.Method != "eth_subscribe" || len(r.Params) == 0 {
			return nil, fmt.Errorf("not an eth_subscribe request: %.100s", req)
		}
		if err := json.Unmarshal(r.Params[0], &lines[i].typ); err != nil {
			return nil, fmt.Errorf("eth_subscribe without a type: %.100s", req)
		}
		var params any
		if err := json.Unmarshal(req, &struct {
			Params *any `json:"params"`
		}{&params}); err != nil {
			return nil, err
		}
		key, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		group, ok := groups[string(key)]
		if !ok {
			group = i
			groups[string(key)] = i
		}
		lines[i].group = group
		switch lines[i].typ {
		case subNewHeads:
		case subLogs:
			lines[i].filter = map[string]any{}
			if len(r.Params) > 1 {
				if err := json.Unmarshal(r.Params[1], &lines[i].filter); err != nil {
					return nil, fmt.Errorf("eth_subscribe logs with an invalid filter: %.100s", req)
				}
			}
		case subReceipts:
			var filter struct {
				TransactionHashes []string `json:"transactionHashes"`
			}
			if len(r.Params) > 1 && (json.Unmarshal(r.Params[1], &filter) != nil || len(filter.TransactionHashes) > 0) {
				return nil, fmt.Errorf("eth_subscribe transactionReceipts with a transactionHashes filter is not measured: %.100s", req)
			}
		default:
			return nil, fmt.Errorf("eth_subscribe %s: only newHeads, logs and transactionReceipts are measured", lines[i].typ)
		}
	}
	return lines, nil
}

// DefaultWSPort is the WebSocket port of a subscription test when its address has none: the default of
// Erigon, Geth and Reth.
const DefaultWSPort = "8546"

// WSURL returns the WebSocket URL of a subscription test: wsAddress, host or host:port, or else the host of
// the client address, on port 8546 unless it has one.
func WSURL(clientAddress, wsAddress string) string {
	address := wsAddress
	if address == "" {
		address = clientAddress
		if host, _, err := net.SplitHostPort(clientAddress); err == nil {
			address = host
		}
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, DefaultWSPort)
	}
	return "ws://" + address
}

type notification struct {
	sub       int // index of the subscription among all those of the test
	arrival   time.Time
	number    uint64
	blockHash string
	key       string    // the event: the block hash of a head, the block hash and the index of a log
	timestamp time.Time // the block timestamp, zero when the notification has none
	receipts  int       // the receipts of a transactionReceipts notification
	// perReceipt is a transactionReceipts notification with one receipt object instead of a list; spread is,
	// once the receipts of a block are merged, the time from the first to the last one.
	perReceipt bool
	spread     time.Duration
}

// connection is what one WebSocket connection of a test received.
type connection struct {
	class    string // the client class, empty without classes
	lines    []int  // the lines the connection sends
	pid      int    // the process that read the connection
	notes    []notification
	rejected map[int]string // subscription index -> error message of its eth_subscribe
	closed   bool           // closed by the node before the end of the test
}

// runSubscriptions opens conns WebSocket connections, sends the eth_subscribe requests of reqs on each one,
// listens for duration and returns one result per notification, missing notification and duplicate:
// its latency is the arrival time minus the block timestamp. The results of each type are also split into
// a latency row, a fan-out row, the arrival time minus the first arrival of the same event on the same
// pattern line, and for logs and receipts a row of the arrival time minus that of the head on the same
// connection and one minus the first arrival of the head on any connection.
func runSubscriptions(ctx context.Context, url, httpURL string, reqs [][]byte, conns int, duration time.Duration, outputFile string) (*vegeta.Metrics, attackResults, error) {
	return runClientSubscriptions(ctx, url, httpURL, reqs, nil, 0, conns, duration, outputFile)
}

// runClientSubscriptions is runSubscriptions with the connections split into the client classes of
// clients, each connection sending the requests of its class and subscribing in the order drawn from seed;
// the results of a class are also in rows named after it.
func runClientSubscriptions(ctx context.Context, url, httpURL string, reqs [][]byte, clients []ClientClass, seed uint64, conns int, duration time.Duration, outputFile string) (*vegeta.Metrics, attackResults, error) {
	lines, err := subscriptionLines(reqs)
	if err != nil {
		return nil, attackResults{}, err
	}
	plan, err := clientConnections(conns, lines, clients)
	if err != nil {
		return nil, attackResults{}, err
	}
	received, start, end, err := collect(ctx, url, reqs, lines, plan, seed, duration)
	if err != nil {
		return nil, attackResults{}, err
	}
	return measure(ctx, received, lines, url, httpURL, start, end, outputFile)
}

// collect opens the connections of plan, sends their eth_subscribe requests and returns what each one
// received in the test, from start to end.
func collect(ctx context.Context, url string, reqs [][]byte, lines []subscriptionLine, plan []clientConnection, seed uint64, duration time.Duration) ([]connection, time.Time, time.Time, error) {
	received := make([]connection, len(plan))
	for i, p := range plan {
		received[i].class, received[i].lines, received[i].pid = p.class, p.lines, os.Getpid()
	}
	heavy := heavyConnections(plan, lines)
	if len(allowedCPUs()) >= 2 && slices.Contains(heavy, true) && slices.Contains(heavy, false) {
		start, end, err := collectSplit(ctx, url, reqs, lines, received, heavy, subscribeOrder(len(plan), seed), duration)
		return received, start, end, err
	}
	sockets, subIDs, rejected, err := subscribeAll(ctx, url, reqs, subscriptions(received, len(reqs)))
	defer closeAll(sockets)
	if err != nil {
		return nil, time.Time{}, time.Time{}, err
	}
	start := time.Now()
	end := start.Add(duration)
	listened := listenAll(ctx, sockets, subIDs, lines, end.Add(notificationDrain), parseWorkers)
	if ctx.Err() != nil {
		return nil, time.Time{}, time.Time{}, ctx.Err()
	}
	for i := range received {
		received[i].rejected, received[i].notes, received[i].closed = rejected[i], listened[i].notes, listened[i].closed
	}
	return received, start, end, nil
}

// subscribeOrder is the random order, drawn from seed, of n connections to subscribe in: real clients do not
// connect grouped by kind.
func subscribeOrder(n int, seed uint64) []int {
	return rand.New(rand.NewPCG(seed, 0)).Perm(n)
}

// subscriptions returns the subscription indexes each connection sends: line j of connection i is
// subscription i*lineCount+j.
func subscriptions(conns []connection, lineCount int) [][]int {
	subs := make([][]int, len(conns))
	for i, p := range conns {
		for _, j := range p.lines {
			subs[i] = append(subs[i], i*lineCount+j)
		}
	}
	return subs
}

// subscribeAll opens one connection per entry of subs and sends its subscriptions, all at once.
func subscribeAll(ctx context.Context, url string, reqs [][]byte, subs [][]int) ([]*websocket.Conn, []map[string]int, []map[int]string, error) {
	sockets := make([]*websocket.Conn, len(subs))
	ids := make([]map[string]int, len(subs)) // subscription id -> index of the subscription
	rejected := make([]map[int]string, len(subs))
	errs := make([]error, len(subs))
	var wg sync.WaitGroup
	for i := range subs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sockets[i], ids[i], rejected[i], errs[i] = subscribe(ctx, url, reqs, subs[i])
		}()
	}
	wg.Wait()
	return sockets, ids, rejected, errors.Join(errs...)
}

func closeAll(sockets []*websocket.Conn) {
	for _, c := range sockets {
		if c != nil {
			c.Close()
		}
	}
}

// listenAll reads the notifications of every socket until deadline, parsing at most workers at once.
func listenAll(ctx context.Context, sockets []*websocket.Conn, ids []map[string]int, lines []subscriptionLine, deadline time.Time, workers int) []connection {
	stop := context.AfterFunc(ctx, func() {
		for _, c := range sockets {
			c.SetReadDeadline(time.Now()) //nolint:errcheck
		}
	})
	defer stop()
	out := make([]connection, len(sockets))
	parsing := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, c := range sockets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i].notes, out[i].closed = listen(c, ids[i], lines, deadline, parsing)
		}()
	}
	wg.Wait()
	return out
}

// subscribe dials url and sends the request of each subscription of subs, the request of a subscription
// being its index modulo len(reqs), waiting for the answer of each, and returns the subscription ids and
// the error messages of the rejected ones, by subscription index.
func subscribe(ctx context.Context, url string, reqs [][]byte, subs []int) (*websocket.Conn, map[string]int, map[int]string, error) {
	c, resp, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("websocket dial %s: %w", url, err)
	}
	ids, rejected := map[string]int{}, map[int]string{}
	for _, sub := range subs {
		if err := c.WriteMessage(websocket.TextMessage, reqs[sub%len(reqs)]); err != nil {
			return c, nil, nil, fmt.Errorf("eth_subscribe: %w", err)
		}
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return c, nil, nil, fmt.Errorf("eth_subscribe: %w", err)
			}
			var resp struct {
				Method string    `json:"method"`
				Result string    `json:"result"`
				Error  *rpcError `json:"error"`
			}
			if err := json.Unmarshal(msg, &resp); err != nil {
				return c, nil, nil, fmt.Errorf("eth_subscribe: %w", err)
			}
			if resp.Method != "" {
				continue // a notification of an earlier subscription, before the test starts
			}
			if resp.Error != nil {
				rejected[sub] = resp.Error.Message
			} else {
				ids[resp.Result] = sub
			}
			break
		}
	}
	return c, ids, rejected, nil
}

type rpcError struct {
	Message string `json:"message"`
}

// listen reads the notifications of c until deadline, and tells whether the node closed c before.
// A frame keeps the time it was read and is parsed only once the reads are over, so that parsing takes
// no CPU from the reads of the other connections, which would take their arrival late. parsing bounds
// the notifications parsed at once over all the connections.
func listen(c *websocket.Conn, ids map[string]int, lines []subscriptionLine, deadline time.Time, parsing chan struct{}) ([]notification, bool) {
	type frame struct {
		msg     []byte
		arrival time.Time
	}
	var frames []frame
	var closed bool
	c.SetReadDeadline(deadline) //nolint:errcheck
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			var netErr net.Error
			closed = !errors.As(err, &netErr) || !netErr.Timeout()
			break
		}
		frames = append(frames, frame{msg, time.Now()})
	}
	var notes []notification
	for _, f := range frames {
		parsing <- struct{}{}
		note, ok := parseFrame(f.msg, ids, lines)
		<-parsing
		if ok {
			note.arrival = f.arrival
			notes = append(notes, note)
		}
	}
	return notes, closed
}

// parseFrame returns the notification of a frame, and false for a frame that is not a counted
// notification of one of ids.
func parseFrame(msg []byte, ids map[string]int, lines []subscriptionLine) (notification, bool) {
	var n struct {
		Params struct {
			Subscription string          `json:"subscription"`
			Result       json.RawMessage `json:"result"`
		} `json:"params"`
	}
	if json.Unmarshal(msg, &n) != nil {
		return notification{}, false
	}
	sub, ok := ids[n.Params.Subscription]
	if !ok {
		return notification{}, false
	}
	note, number, stamp, ok := parseResult(lines[sub%len(lines)].typ, n.Params.Result)
	if !ok {
		return notification{}, false
	}
	note.sub = sub
	var err error
	if note.number, err = parseHex(number); err != nil {
		return notification{}, false
	}
	if stamp != "" {
		seconds, err := parseHex(stamp)
		if err != nil {
			return notification{}, false
		}
		note.timestamp = time.Unix(int64(seconds), 0)
	}
	return note, true
}

type notifiedLog struct {
	BlockNumber    string `json:"blockNumber"`
	BlockHash      string `json:"blockHash"`
	BlockTimestamp string `json:"blockTimestamp"`
	LogIndex       string `json:"logIndex"`
	Removed        bool   `json:"removed"`
}

// parseResult parses the result of a notification; a test can make it slow.
var parseResult = parseNotification

// parseWorkers is how many notifications are parsed at once over all the connections, once the reads are
// over: one per core. A test can change it.
var parseWorkers = runtime.GOMAXPROCS(0)

// parseNotification returns the notification of a result of a subscription of typ, with its block number and
// block timestamp in hex, and false for a result that is not counted: a removed log, an empty list of receipts.
func parseNotification(typ string, result json.RawMessage) (note notification, number, stamp string, ok bool) {
	switch typ {
	case subNewHeads:
		var h struct {
			Number    string `json:"number"`
			Hash      string `json:"hash"`
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(result, &h) != nil {
			return note, "", "", false
		}
		note.blockHash, note.key = h.Hash, h.Hash
		return note, h.Number, h.Timestamp, true
	case subLogs:
		var lg notifiedLog
		if json.Unmarshal(result, &lg) != nil || lg.Removed {
			return note, "", "", false
		}
		note.blockHash, note.key = lg.BlockHash, lg.BlockHash+"/"+lg.LogIndex
		return note, lg.BlockNumber, lg.BlockTimestamp, true
	case subReceipts:
		var receipts []notifiedReceipt
		if trimmed := bytes.TrimSpace(result); len(trimmed) > 0 && trimmed[0] == '{' {
			var receipt notifiedReceipt
			if json.Unmarshal(result, &receipt) != nil {
				return note, "", "", false
			}
			receipts, note.perReceipt = []notifiedReceipt{receipt}, true
		} else if json.Unmarshal(result, &receipts) != nil || len(receipts) == 0 {
			return note, "", "", false
		}
		// A receipt has no block timestamp of its own on every client: take the one of a log when it lacks it.
		for _, r := range receipts {
			if stamp = r.BlockTimestamp; stamp == "" && len(r.Logs) > 0 {
				stamp = r.Logs[0].BlockTimestamp
			}
			if stamp != "" {
				break
			}
		}
		note.blockHash, note.key, note.receipts = receipts[0].BlockHash, receipts[0].BlockHash, len(receipts)
		return note, receipts[0].BlockNumber, stamp, true
	}
	return note, "", "", false
}

type notifiedReceipt struct {
	BlockNumber    string        `json:"blockNumber"`
	BlockHash      string        `json:"blockHash"`
	BlockTimestamp string        `json:"blockTimestamp"`
	Logs           []notifiedLog `json:"logs"`
}

// mergeReceiptObjects turns the receipts that a subscription received one per notification into one
// notification per block, as the list the other clients send: it arrives with the last receipt of the block.
func mergeReceiptObjects(notes []notification) []notification {
	merged := make([]notification, 0, len(notes))
	type block struct {
		sub  int
		hash string
	}
	blocks := map[block]int{} // the index of the merged notification
	for _, n := range notes {
		if !n.perReceipt {
			merged = append(merged, n)
			continue
		}
		key := block{n.sub, n.blockHash}
		i, ok := blocks[key]
		if !ok {
			blocks[key] = len(merged)
			merged = append(merged, n)
			continue
		}
		m := &merged[i]
		m.receipts++
		m.spread += n.arrival.Sub(m.arrival)
		m.arrival = n.arrival
		if m.timestamp.IsZero() {
			m.timestamp = n.timestamp
		}
	}
	return merged
}

func parseHex(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}

// measure turns the notifications of the blocks first seen between start and end into results. Every
// subscription is expected to receive every block number from the lowest to the highest of those blocks:
// a newHeads subscription its head, a logs subscription the logs eth_getLogs returns for its filter.
func measure(ctx context.Context, conns []connection, lines []subscriptionLine, url, httpURL string, start, end time.Time, outputFile string) (*vegeta.Metrics, attackResults, error) {
	for i := range conns {
		conns[i].notes = mergeReceiptObjects(conns[i].notes)
	}
	blockFirst := map[string]time.Time{}
	headFirst := map[string]time.Time{}  // by block hash, the first arrival of its head on any connection
	eventFirst := map[string]time.Time{} // by pattern line and event
	for _, conn := range conns {
		for _, n := range conn.notes {
			if t, ok := blockFirst[n.blockHash]; !ok || n.arrival.Before(t) {
				blockFirst[n.blockHash] = n.arrival
			}
			if t, ok := headFirst[n.blockHash]; lines[n.sub%len(lines)].typ == subNewHeads && (!ok || n.arrival.Before(t)) {
				headFirst[n.blockHash] = n.arrival
			}
			event := eventKey(n, lines)
			if t, ok := eventFirst[event]; !ok || n.arrival.Before(t) {
				eventFirst[event] = n.arrival
			}
		}
	}
	inWindow := func(n notification) bool {
		t := blockFirst[n.blockHash]
		return !t.Before(start) && !t.After(end)
	}
	lo, hi := uint64(math.MaxUint64), uint64(0)
	for _, conn := range conns {
		for _, n := range conn.notes {
			if inWindow(n) {
				lo, hi = min(lo, n.number), max(hi, n.number)
			}
		}
	}
	expected, err := expectedLogs(ctx, httpURL, lines, lo, hi)
	if err != nil {
		return nil, attackResults{}, err
	}
	txCounts, err := expectedTxCounts(ctx, httpURL, lines, lo, hi)
	if err != nil {
		return nil, attackResults{}, err
	}

	out, err := os.Create(outputFile)
	if err != nil {
		return nil, attackResults{}, fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()
	encoder := vegeta.NewEncoder(out)
	var metrics vegeta.Metrics
	results := attackResults{rpcErrors: map[string]int{}, apis: map[string]*apiResult{}}
	class := ""
	rowsOf := func(name string) []*apiResult {
		if class == "" {
			return []*apiResult{results.row(name)}
		}
		return []*apiResult{results.row(name), results.row(name + " " + class)}
	}
	add := func(sub int, r *vegeta.Result) (row string) {
		typ := lines[sub%len(lines)].typ
		r.Attack, r.URL, r.Method = "eth_subscribe", url, "WS"
		if r.Timestamp.IsZero() {
			r.Timestamp = end
		}
		metrics.Add(r)
		if err := encoder.Encode(r); err != nil {
			log.Printf("Warning: failed to encode result: %v", err)
		}
		for _, a := range rowsOf(typ + " latency") {
			a.add(r.Latency, r.Error)
		}
		if r.Error == "" {
			results.ok.Add(r.Latency)
			results.okRequests++
		}
		return typ
	}

	for i, conn := range conns {
		class = conn.class
		seen := map[int]map[string]bool{}    // by subscription, the events received
		numbers := map[int]map[uint64]bool{} // by subscription, the block numbers received
		receipts := map[int]map[uint64]int{} // by subscription and block number, the receipts received
		heads := map[string]time.Time{}      // by block hash, the arrival of the head on this connection
		for _, n := range conn.notes {
			if lines[n.sub%len(lines)].typ == subNewHeads {
				if t, ok := heads[n.blockHash]; !ok || n.arrival.Before(t) {
					heads[n.blockHash] = n.arrival
				}
			}
		}
		for _, n := range conn.notes {
			if !inWindow(n) {
				continue
			}
			if seen[n.sub] == nil {
				seen[n.sub], numbers[n.sub], receipts[n.sub] = map[string]bool{}, map[uint64]bool{}, map[uint64]int{}
			}
			r := &vegeta.Result{Code: 200, Timestamp: n.arrival}
			if !n.timestamp.IsZero() {
				r.Latency = n.arrival.Sub(n.timestamp)
			}
			receipts[n.sub][n.number] += n.receipts
			switch {
			case seen[n.sub][n.key] && n.receipts > 0:
				r.Code, r.Error = 0, errGranularity
			case seen[n.sub][n.key]:
				r.Code, r.Error = 0, errDuplicate
			case n.timestamp.IsZero():
				r.Code, r.Error = 0, errNoTimestamp
			}
			seen[n.sub][n.key], numbers[n.sub][n.number] = true, true
			typ := add(n.sub, r)
			if r.Code != 200 {
				continue
			}
			if first := eventFirst[eventKey(n, lines)]; !n.arrival.Equal(first) {
				for _, a := range rowsOf(typ + " fan-out") {
					a.add(n.arrival.Sub(first), "")
				}
			}
			if head, ok := heads[n.blockHash]; ok && typ != subNewHeads {
				for _, a := range rowsOf(typ + " vs newHeads") {
					a.add(n.arrival.Sub(head), "")
				}
			}
			if head, ok := headFirst[n.blockHash]; ok && typ != subNewHeads {
				for _, a := range rowsOf(typ + " vs first newHeads") {
					a.add(n.arrival.Sub(head), "")
				}
			}
			if n.perReceipt {
				for _, a := range rowsOf(typ + " per receipt") {
					a.add(n.spread, "")
				}
			}
		}
		for _, j := range conn.lines {
			line := lines[j]
			sub := i*len(lines) + j
			if msg, ok := conn.rejected[sub]; ok {
				add(sub, &vegeta.Result{Error: "rejected subscribe: " + RPCErrorKind(msg)})
				continue
			}
			if conn.closed {
				add(sub, &vegeta.Result{Error: errClosed})
			}
			for number := lo; number <= hi; number++ {
				switch {
				case line.typ == subNewHeads && !numbers[sub][number]:
					add(sub, &vegeta.Result{Error: errMissing})
				case line.typ != subReceipts || txCounts[number] == 0:
				case !numbers[sub][number]:
					add(sub, &vegeta.Result{Error: errMissing})
				case receipts[sub][number] != txCounts[number]:
					add(sub, &vegeta.Result{Error: errReceiptCount})
				}
				for _, key := range expected[j][number] {
					if !seen[sub][key] {
						add(sub, &vegeta.Result{Error: errMissing})
					}
				}
			}
		}
	}
	metrics.Close()
	return &metrics, results, nil
}

func eventKey(n notification, lines []subscriptionLine) string {
	return strconv.Itoa(lines[n.sub%len(lines)].group) + "/" + n.key
}

// expectedLogs reads over HTTP, for each logs line and each block from lo to hi, the logs eth_getLogs
// returns for the filter of the line, as the events a subscription of the line must receive.
func expectedLogs(ctx context.Context, url string, lines []subscriptionLine, lo, hi uint64) (map[int]map[uint64][]string, error) {
	expected := map[int]map[uint64][]string{}
	for j, line := range lines {
		if line.typ != subLogs {
			continue
		}
		expected[j] = map[uint64][]string{}
		for number := lo; number <= hi && lo <= hi; number++ {
			filter := maps.Clone(line.filter)
			filter["fromBlock"], filter["toBlock"] = fmt.Sprintf("0x%x", number), fmt.Sprintf("0x%x", number)
			var logs []struct {
				BlockHash string `json:"blockHash"`
				LogIndex  string `json:"logIndex"`
			}
			if err := httpCall(ctx, url, "eth_getLogs", []any{filter}, &logs); err != nil {
				return nil, err
			}
			for _, lg := range logs {
				expected[j][number] = append(expected[j][number], lg.BlockHash+"/"+lg.LogIndex)
			}
		}
	}
	return expected, nil
}

// expectedTxCounts reads over HTTP the number of transactions of each block from lo to hi, the receipts a
// transactionReceipts subscription must receive, when lines have one.
func expectedTxCounts(ctx context.Context, url string, lines []subscriptionLine, lo, hi uint64) (map[uint64]int, error) {
	counts := map[uint64]int{}
	if !slices.ContainsFunc(lines, func(l subscriptionLine) bool { return l.typ == subReceipts }) {
		return counts, nil
	}
	for number := lo; number <= hi && lo <= hi; number++ {
		var count string
		if err := httpCall(ctx, url, "eth_getBlockTransactionCountByNumber", []any{fmt.Sprintf("0x%x", number)}, &count); err != nil {
			return nil, err
		}
		n, err := parseHex(count)
		if err != nil {
			return nil, fmt.Errorf("eth_getBlockTransactionCountByNumber 0x%x: %w", number, err)
		}
		counts[number] = int(n)
	}
	return counts, nil
}

// httpCall posts a JSON-RPC request to url and decodes its result into result.
func httpCall(ctx context.Context, url, method string, params []any, result any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if r.Error != nil {
		return fmt.Errorf("%s: %s", method, r.Error.Message)
	}
	return json.Unmarshal(r.Result, result)
}

// row returns the result row of key, created on first use.
func (r *attackResults) row(key string) *apiResult {
	a := r.apis[key]
	if a == nil {
		a = &apiResult{errors: map[string]int{}}
		r.apis[key] = a
	}
	return a
}

// formatSubscriptionResults returns the table of the rows of a subscription test, by name. Only the latency
// rows have a share, of all the notifications: the other rows count the same notifications again.
func formatSubscriptionResults(rows map[string]*apiResult, morePercentiles bool) []string {
	names := slices.Sorted(maps.Keys(rows))
	notifications := 0
	for _, name := range names {
		if strings.HasSuffix(name, " latency") {
			notifications += rows[name].requests
		}
	}
	return formatShares("api", names, rows, morePercentiles, func(name string) string {
		if !strings.HasSuffix(name, " latency") {
			return "-"
		}
		return fmt.Sprintf("%5.1f%%", 100*float64(rows[name].requests)/float64(notifications))
	})
}

// add counts one result of the row: its latency and its error, if any.
func (a *apiResult) add(latency time.Duration, err string) {
	a.requests++
	a.latencies.Add(latency)
	if err != "" {
		a.errors[err]++
	}
}

// clientConnection is the class of a connection and the lines it sends.
type clientConnection struct {
	class string
	lines []int
}

// clientConnections returns, for each of conns connections, its class and the lines it sends: every line
// without classes. The connections are split by share, the rest going to the largest remainders, and each
// class gets at least one, taken from the class with the most.
func clientConnections(conns int, lines []subscriptionLine, clients []ClientClass) ([]clientConnection, error) {
	out := make([]clientConnection, conns)
	if len(clients) == 0 {
		all := make([]int, len(lines))
		for j := range all {
			all[j] = j
		}
		for i := range out {
			out[i].lines = all
		}
		return out, nil
	}
	if conns < len(clients) {
		return nil, fmt.Errorf("%d connections for %d client classes: want at least one each", conns, len(clients))
	}
	covered, total := 0, 0.0
	for _, c := range clients {
		if c.Share <= 0 || c.Count <= 0 || c.First != covered || c.First+c.Count > len(lines) {
			return nil, fmt.Errorf("client class %q: want a positive share and the lines right after the previous class, within the %d requests", c.Name, len(lines))
		}
		covered += c.Count
		total += c.Share
	}
	if covered != len(lines) {
		return nil, fmt.Errorf("client classes cover %d of the %d requests", covered, len(lines))
	}
	counts := make([]int, len(clients))
	rest := make([]float64, len(clients))
	assigned := 0
	for k, c := range clients {
		exact := float64(conns) * c.Share / total
		counts[k] = int(exact)
		rest[k] = exact - float64(counts[k])
		assigned += counts[k]
	}
	order := make([]int, len(clients))
	for k := range order {
		order[k] = k
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(rest[b], rest[a]) })
	for _, k := range order[:conns-assigned] {
		counts[k]++
	}
	for k := range counts {
		if counts[k] == 0 {
			counts[slices.Index(counts, slices.Max(counts))]--
			counts[k] = 1
		}
	}
	i := 0
	for k, c := range clients {
		var logs, others []int
		for j := c.First; j < c.First+c.Count; j++ {
			if c.Pick > 0 && lines[j].typ == subLogs {
				logs = append(logs, j)
			} else {
				others = append(others, j)
			}
		}
		for n := range counts[k] {
			sent := slices.Clone(others)
			for t := range min(c.Pick, len(logs)) {
				sent = append(sent, logs[(n*c.Pick+t)%len(logs)])
			}
			slices.Sort(sent)
			out[i] = clientConnection{class: c.Name, lines: sent}
			i++
		}
	}
	return out, nil
}
