package perf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const (
	errMissing   = "missing notification"
	errDuplicate = "duplicate notification"
	errClosed    = "connection closed"

	// notificationDrain is how long the notifications of a head first seen before the end of a test are
	// still awaited, so that a slow subscription is not counted as missing.
	notificationDrain = 2 * time.Second
)

// subscriptionTypes returns the subscription type of each eth_subscribe request.
func subscriptionTypes(reqs [][]byte) ([]string, error) {
	types := make([]string, len(reqs))
	for i, req := range reqs {
		var r struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(req, &r); err != nil || r.Method != "eth_subscribe" || len(r.Params) == 0 {
			return nil, fmt.Errorf("not an eth_subscribe request: %.100s", req)
		}
		if err := json.Unmarshal(r.Params[0], &types[i]); err != nil {
			return nil, fmt.Errorf("eth_subscribe without a type: %.100s", req)
		}
		if types[i] != "newHeads" {
			return nil, fmt.Errorf("eth_subscribe %s: only newHeads is measured", types[i])
		}
	}
	return types, nil
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
	hash      string
	timestamp time.Time
}

// connection is what one WebSocket connection of a test received.
type connection struct {
	notes    []notification
	rejected map[int]string // subscription index -> error message of its eth_subscribe
	closed   bool           // closed by the node before the end of the test
}

// runSubscriptions opens conns WebSocket connections, sends the eth_subscribe requests of reqs on each one,
// listens for duration and returns one result per notification, missing notification and duplicate:
// its latency is the arrival time minus the block timestamp. The results of each type are also split into
// a latency row and a fan-out row, the arrival time minus the first arrival of the same head.
func runSubscriptions(ctx context.Context, url string, reqs [][]byte, conns int, duration time.Duration, outputFile string) (*vegeta.Metrics, attackResults, error) {
	types, err := subscriptionTypes(reqs)
	if err != nil {
		return nil, attackResults{}, err
	}
	sockets := make([]*websocket.Conn, conns)
	subIDs := make([]map[string]int, conns) // subscription id -> index of the subscription
	received := make([]connection, conns)
	defer func() {
		for _, c := range sockets {
			if c != nil {
				c.Close()
			}
		}
	}()
	var wg sync.WaitGroup
	errs := make([]error, conns)
	for i := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sockets[i], subIDs[i], received[i].rejected, errs[i] = subscribe(ctx, url, reqs, i*len(reqs))
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, attackResults{}, err
	}

	start := time.Now()
	end := start.Add(duration)
	stop := context.AfterFunc(ctx, func() {
		for _, c := range sockets {
			c.SetReadDeadline(time.Now()) //nolint:errcheck
		}
	})
	defer stop()
	for i, c := range sockets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			received[i].notes, received[i].closed = listen(c, subIDs[i], end.Add(notificationDrain))
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, attackResults{}, ctx.Err()
	}
	return measure(received, types, url, start, end, outputFile)
}

// subscribe dials url and sends reqs, waiting for the answer of each, and returns the subscription ids and
// the error messages of the rejected ones, by subscription index, numbered from first.
func subscribe(ctx context.Context, url string, reqs [][]byte, first int) (*websocket.Conn, map[string]int, map[int]string, error) {
	c, resp, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("websocket dial %s: %w", url, err)
	}
	ids, rejected := map[string]int{}, map[int]string{}
	for i, req := range reqs {
		if err := c.WriteMessage(websocket.TextMessage, req); err != nil {
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
				rejected[first+i] = resp.Error.Message
			} else {
				ids[resp.Result] = first + i
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
func listen(c *websocket.Conn, ids map[string]int, deadline time.Time) ([]notification, bool) {
	c.SetReadDeadline(deadline) //nolint:errcheck
	var notes []notification
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			var netErr net.Error
			return notes, !errors.As(err, &netErr) || !netErr.Timeout()
		}
		arrival := time.Now()
		var n struct {
			Params struct {
				Subscription string `json:"subscription"`
				Result       struct {
					Number    string `json:"number"`
					Hash      string `json:"hash"`
					Timestamp string `json:"timestamp"`
				} `json:"result"`
			} `json:"params"`
		}
		if json.Unmarshal(msg, &n) != nil {
			continue
		}
		sub, ok := ids[n.Params.Subscription]
		if !ok {
			continue
		}
		seconds, err := strconv.ParseUint(strings.TrimPrefix(n.Params.Result.Timestamp, "0x"), 16, 64)
		if err != nil {
			continue
		}
		number, err := strconv.ParseUint(strings.TrimPrefix(n.Params.Result.Number, "0x"), 16, 64)
		if err != nil {
			continue
		}
		notes = append(notes, notification{sub: sub, arrival: arrival, number: number, hash: n.Params.Result.Hash, timestamp: time.Unix(int64(seconds), 0)})
	}
}

// measure turns the notifications of the heads first seen between start and end into results. Every
// subscription is expected to receive every block number from the lowest to the highest of those heads.
func measure(conns []connection, types []string, url string, start, end time.Time, outputFile string) (*vegeta.Metrics, attackResults, error) {
	first := map[string]time.Time{}
	for _, conn := range conns {
		for _, n := range conn.notes {
			if t, ok := first[n.hash]; !ok || n.arrival.Before(t) {
				first[n.hash] = n.arrival
			}
		}
	}
	lo, hi := uint64(math.MaxUint64), uint64(0)
	for _, conn := range conns {
		for _, n := range conn.notes {
			if t := first[n.hash]; t.Before(start) || t.After(end) {
				delete(first, n.hash)
				continue
			}
			lo, hi = min(lo, n.number), max(hi, n.number)
		}
	}

	out, err := os.Create(outputFile)
	if err != nil {
		return nil, attackResults{}, fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()
	encoder := vegeta.NewEncoder(out)
	var metrics vegeta.Metrics
	results := attackResults{rpcErrors: map[string]int{}, apis: map[string]*apiResult{}}
	add := func(sub int, r *vegeta.Result, fanOut time.Duration, fastest bool) {
		typ := types[sub%len(types)]
		r.Attack, r.URL, r.Method = "eth_subscribe", url, "WS"
		if r.Timestamp.IsZero() {
			r.Timestamp = end
		}
		metrics.Add(r)
		if err := encoder.Encode(r); err != nil {
			log.Printf("Warning: failed to encode result: %v", err)
		}
		row := results.row(typ + " latency")
		row.requests++
		row.latencies.Add(r.Latency)
		if r.Error != "" {
			row.errors[r.Error]++
			return
		}
		results.ok.Add(r.Latency)
		results.okRequests++
		if !fastest {
			row := results.row(typ + " fan-out")
			row.requests++
			row.latencies.Add(fanOut)
		}
	}

	for i, conn := range conns {
		seen := map[int]map[string]bool{}
		numbers := map[int]map[uint64]bool{}
		for _, n := range conn.notes {
			t, ok := first[n.hash]
			if !ok {
				continue
			}
			if seen[n.sub] == nil {
				seen[n.sub], numbers[n.sub] = map[string]bool{}, map[uint64]bool{}
			}
			r := &vegeta.Result{Code: 200, Timestamp: n.arrival, Latency: n.arrival.Sub(n.timestamp)}
			if seen[n.sub][n.hash] {
				r.Code, r.Error = 0, errDuplicate
			}
			seen[n.sub][n.hash], numbers[n.sub][n.number] = true, true
			add(n.sub, r, n.arrival.Sub(t), n.arrival.Equal(t))
		}
		for j := range types {
			sub := i*len(types) + j
			if msg, ok := conn.rejected[sub]; ok {
				add(sub, &vegeta.Result{Error: "rejected subscribe: " + RPCErrorKind(msg)}, 0, false)
				continue
			}
			if conn.closed {
				add(sub, &vegeta.Result{Error: errClosed}, 0, false)
			}
			for number := lo; number <= hi && len(first) > 0; number++ {
				if !numbers[sub][number] {
					add(sub, &vegeta.Result{Error: errMissing}, 0, false)
				}
			}
		}
	}
	metrics.Close()
	return &metrics, results, nil
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
