package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	newHeadsRequest = `{"jsonrpc":"2.0","method":"eth_subscribe","params":["newHeads"],"id":1}`
	logsRequest     = `{"jsonrpc":"2.0","method":"eth_subscribe","params":["logs",{"address":"0x02"}],"id":1}`
)

type fakeHead struct {
	number    uint64
	hash      string
	timestamp time.Time
	logs      int // logs of the block, each matching every logs subscription
}

// wsNode is a WebSocket node that, once conns connections have subscribed, sends heads to every
// subscription of each connection, after the delay of the connection.
type wsNode struct {
	conns      int
	heads      []fakeHead
	delay      func(conn int) time.Duration
	skip       func(conn int, h fakeHead) bool // the head is not sent to this connection
	twice      func(conn int, h fakeHead) bool // the head is sent twice to this connection
	reject     func(conn int) bool             // the subscribe of this connection is answered with an error
	closeAfter func(conn int) int              // the connection is closed after this many heads; 0 = never
	skipLog    func(conn int, h fakeHead, index int) bool
	noLogTime  bool // logs are sent without blockTimestamp
	lines      int  // subscriptions per connection, the lines of the pattern; 0 = 1

	mu       sync.Mutex
	accepted int
	ready    chan struct{}
	subs     int
}

func startWSNode(t *testing.T, n *wsNode) string {
	t.Helper()
	n.ready = make(chan struct{})
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			n.serveHTTP(t, w, r)
			return
		}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		n.mu.Lock()
		conn := n.accepted
		n.accepted++
		n.mu.Unlock()
		subs := n.subscribe(t, c, conn)
		<-n.ready
		time.Sleep(100 * time.Millisecond)
		for i, h := range n.heads {
			if n.closeAfter != nil && n.closeAfter(conn) > 0 && i == n.closeAfter(conn) {
				return
			}
			if n.delay != nil {
				time.Sleep(n.delay(conn))
			}
			if n.skip != nil && n.skip(conn, h) {
				continue
			}
			times := 1
			if n.twice != nil && n.twice(conn, h) {
				times = 2
			}
			for range times {
				for _, sub := range subs {
					for _, result := range n.results(conn, sub.typ, h) {
						note := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_subscription","params":{"subscription":%q,"result":%s}}`, sub.id, result)
						if c.WriteMessage(websocket.TextMessage, []byte(note)) != nil {
							return
						}
					}
				}
			}
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

type fakeSub struct{ id, typ string }

// results returns the notification results of h for a subscription of typ on conn.
func (n *wsNode) results(conn int, typ string, h fakeHead) []string {
	if typ == "newHeads" {
		return []string{fmt.Sprintf(`{"number":"0x%x","hash":%q,"timestamp":"0x%x"}`, h.number, h.hash, h.timestamp.Unix())}
	}
	var out []string
	for i := range h.logs {
		if n.skipLog != nil && n.skipLog(conn, h, i) {
			continue
		}
		out = append(out, n.log(h, i))
	}
	return out
}

func (n *wsNode) log(h fakeHead, index int) string {
	stamp := fmt.Sprintf(`,"blockTimestamp":"0x%x"`, h.timestamp.Unix())
	if n.noLogTime {
		stamp = ""
	}
	return fmt.Sprintf(`{"address":"0x02","topics":[],"blockNumber":"0x%x","blockHash":%q,"logIndex":"0x%x","removed":false%s}`, h.number, h.hash, index, stamp)
}

// serveHTTP answers eth_getLogs on one block of the heads.
func (n *wsNode) serveHTTP(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Params) == 0 {
		t.Errorf("bad HTTP request: %v", err)
		return
	}
	if req.Method != "eth_getLogs" {
		t.Errorf("unexpected HTTP method %s", req.Method)
		return
	}
	var filter struct {
		FromBlock string `json:"fromBlock"`
		ToBlock   string `json:"toBlock"`
		Address   string `json:"address"`
	}
	json.Unmarshal(req.Params[0], &filter) //nolint:errcheck
	if filter.Address != "0x02" || filter.FromBlock != filter.ToBlock {
		t.Errorf("eth_getLogs %s: want the filter of the subscription on one block", req.Params[0])
	}
	var logs []string
	for _, h := range n.heads {
		if fmt.Sprintf("0x%x", h.number) == filter.FromBlock {
			for i := range h.logs {
				logs = append(logs, n.log(h, i))
			}
		}
	}
	result := "[" + strings.Join(logs, ",") + "]"
	fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":%s}`, result)
}

// subscribe answers the eth_subscribe requests of a connection until it holds one subscription per
// pattern line, and returns them.
func (n *wsNode) subscribe(t *testing.T, c *websocket.Conn, conn int) []fakeSub {
	var subs []fakeSub
	for answered := 1; ; answered++ {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return subs
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params []any           `json:"params"`
		}
		if err := json.Unmarshal(msg, &req); err != nil || req.Method != "eth_subscribe" {
			t.Errorf("unexpected request %s", msg)
			return subs
		}
		id := fmt.Sprintf("0x%d%d", conn, answered)
		resp := fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%s,"result":%q}`, req.ID, id)
		if n.reject != nil && n.reject(conn) {
			resp = fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"too many subscriptions"}}`, req.ID)
		} else {
			subs = append(subs, fakeSub{id: id, typ: fmt.Sprint(req.Params[0])})
		}
		if err := c.WriteMessage(websocket.TextMessage, resp); err != nil {
			return subs
		}
		n.mu.Lock()
		n.subs++
		if n.subs == n.conns*n.perConn() {
			close(n.ready)
		}
		n.mu.Unlock()
		if answered == n.perConn() {
			return subs
		}
	}
}

func (n *wsNode) perConn() int { return max(n.lines, 1) }

func heads(count int) []fakeHead {
	now := time.Now().Truncate(time.Second)
	out := make([]fakeHead, count)
	for i := range out {
		out[i] = fakeHead{number: uint64(100 + i), hash: fmt.Sprintf("0x%064x", 100+i), timestamp: now}
	}
	return out
}

func TestRunSubscriptionsMeasuresNotificationLatencyAndFanOut(t *testing.T) {
	node := &wsNode{conns: 2, heads: heads(2), delay: func(conn int) time.Duration { return time.Duration(conn) * 100 * time.Millisecond }}
	addr := startWSNode(t, node)

	metrics, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(newHeadsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	if metrics.Requests != 4 || metrics.Success != 1 {
		t.Errorf("requests %d success %v: want the 4 notifications, all received", metrics.Requests, metrics.Success)
	}
	if metrics.Latencies.Min <= 0 || metrics.Latencies.Max > 5*time.Second {
		t.Errorf("latencies %v..%v: want arrival - block timestamp", metrics.Latencies.Min, metrics.Latencies.Max)
	}
	latency, fanOut := results.apis["newHeads latency"], results.apis["newHeads fan-out"]
	if latency == nil || latency.requests != 4 {
		t.Fatalf("newHeads latency row %+v: want the 4 notifications", latency)
	}
	if fanOut == nil || fanOut.requests != 2 {
		t.Fatalf("newHeads fan-out row %+v: want 1 per head, the fastest left out", fanOut)
	}
	if got := fanOut.latencies.Max; got < 100*time.Millisecond || got > time.Second {
		t.Errorf("fan-out max %v: want the 100 ms delay of the second connection", got)
	}
}

func TestRunSubscriptionsCountsMissingAndDuplicateNotifications(t *testing.T) {
	hs := heads(3)
	node := &wsNode{
		conns: 2, heads: hs,
		skip:  func(conn int, h fakeHead) bool { return conn == 1 && h.number == hs[1].number },
		twice: func(conn int, h fakeHead) bool { return conn == 0 && h.number == hs[2].number },
	}
	addr := startWSNode(t, node)

	metrics, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(newHeadsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	row := results.apis["newHeads latency"]
	if row == nil || row.errors[errMissing] != 1 || row.errors[errDuplicate] != 1 {
		t.Errorf("errors %+v: want 1 missing and 1 duplicate", row)
	}
	if metrics.Requests != 7 || metrics.Success >= 1 {
		t.Errorf("requests %d success %v: want 5 received, 1 duplicate and 1 missing", metrics.Requests, metrics.Success)
	}
}

func TestRunSubscriptionsRejectsUnsupportedTypes(t *testing.T) {
	receipts := []byte(`{"jsonrpc":"2.0","method":"eth_subscribe","params":["transactionReceipts"],"id":1}`)
	if _, _, err := runSubscriptions(context.Background(), "ws://localhost:1", "http://localhost:1", [][]byte{receipts}, 1, time.Second, filepath.Join(t.TempDir(), "run.bin")); err == nil {
		t.Error("transactionReceipts subscription: want an error, it is not measured yet")
	}
}

func TestExecuteRunsASubscriptionPatternOnTheConnections(t *testing.T) {
	node := &wsNode{conns: 3, heads: heads(1)}
	addr := startWSNode(t, node)
	pt, cfg, _ := newJSONLPerf(t, "127.0.0.1:1", newHeadsRequest)
	cfg.WSAddress = addr

	format := ResultFormat{MaxRepetitionDigits: 1, MaxQpsDigits: 1, MaxDurationDigits: 1}
	if err := pt.Execute(context.Background(), 1, 0, "rpcdaemon", 3, 1, format); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.accepted != 3 {
		t.Errorf("%d connections: want the 3 of the qps column", node.accepted)
	}
}

func TestRunSubscriptionsCountsTheHeadsNoSubscriptionReceived(t *testing.T) {
	hs := heads(3)
	node := &wsNode{conns: 2, heads: []fakeHead{hs[0], hs[2]}} // no subscription receives hs[1]
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(newHeadsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	if row := results.apis["newHeads latency"]; row == nil || row.errors[errMissing] != 2 {
		t.Errorf("errors %+v: want the gap in the block numbers missing on both subscriptions", row)
	}
}

func TestRunSubscriptionsCountsRejectedSubscribesAndClosedConnections(t *testing.T) {
	node := &wsNode{
		conns: 3, heads: heads(3),
		reject:     func(conn int) bool { return conn == 1 },
		closeAfter: func(conn int) int { return map[int]int{2: 1}[conn] },
	}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(newHeadsRequest)}, 3, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: a rejected subscribe is a result, not a failure of the test: %v", err)
	}
	row := results.apis["newHeads latency"]
	if row == nil {
		t.Fatal("no newHeads latency row")
	}
	want := map[string]int{"rejected subscribe: too many subscriptions": 1, errClosed: 1, errMissing: 2}
	if fmt.Sprint(row.errors) != fmt.Sprint(want) {
		t.Errorf("errors %v, want %v: the closed connection misses the last 2 heads, the rejected one is not expected to receive any", row.errors, want)
	}
}

func headsWithLogs(count, logs int) []fakeHead {
	hs := heads(count)
	for i := range hs {
		hs[i].logs = logs
	}
	return hs
}

func TestRunSubscriptionsMeasuresLogsAgainstTheirHead(t *testing.T) {
	node := &wsNode{conns: 2, lines: 2, heads: headsWithLogs(2, 2), delay: func(conn int) time.Duration { return time.Duration(conn) * 100 * time.Millisecond }}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(newHeadsRequest), []byte(logsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	for row, want := range map[string]int{
		"newHeads latency": 4, // 2 connections x 2 heads
		"logs latency":     8, // 2 connections x 2 heads x 2 logs
		"logs fan-out":     4, // the fastest of each log left out
		"logs vs newHeads": 8,
	} {
		if got := results.apis[row]; got == nil || got.requests != want || len(got.errors) > 0 {
			t.Errorf("%s row %+v: want %d requests and no error", row, got, want)
		}
	}
	if got := results.apis["logs fan-out"].latencies.Max; got < 100*time.Millisecond {
		t.Errorf("logs fan-out max %v: want the 100 ms delay of the second connection", got)
	}
	if got := results.apis["logs vs newHeads"].latencies.Min; got < 0 {
		t.Errorf("logs vs newHeads min %v: the node sends each head before its logs", got)
	}
}

func TestRunSubscriptionsCountsTheLogsTheNodeHasButDidNotSend(t *testing.T) {
	hs := headsWithLogs(2, 3)
	node := &wsNode{conns: 2, heads: hs, skipLog: func(conn int, h fakeHead, index int) bool {
		return conn == 1 && h.number == hs[0].number && index == 1
	}}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(logsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	row := results.apis["logs latency"]
	if row == nil || row.requests != 12 || row.errors[errMissing] != 1 {
		t.Errorf("logs latency row %+v: want 11 logs received and 1 missing, by eth_getLogs of the block", row)
	}
	if results.apis["logs vs newHeads"] != nil {
		t.Error("logs vs newHeads row without a newHeads subscription")
	}
}

func TestRunSubscriptionsCountsLogsWithoutBlockTimestamp(t *testing.T) {
	node := &wsNode{conns: 1, heads: headsWithLogs(1, 2), noLogTime: true}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(logsRequest)}, 1, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	if row := results.apis["logs latency"]; row == nil || row.errors[errNoTimestamp] != 2 {
		t.Errorf("logs latency row %+v: want the 2 logs without blockTimestamp as errors", row)
	}
}

func TestWSURL(t *testing.T) {
	for _, tt := range []struct{ client, ws, want string }{
		{"localhost", "", "ws://localhost:8546"},
		{"127.0.0.1:8545", "", "ws://127.0.0.1:8546"},
		{"127.0.0.1:8545", "127.0.0.1:9000", "ws://127.0.0.1:9000"},
		{"127.0.0.1:8545", "node", "ws://node:8546"},
	} {
		if got := WSURL(tt.client, tt.ws); got != tt.want {
			t.Errorf("WSURL(%q, %q) = %q, want %q", tt.client, tt.ws, got, tt.want)
		}
	}
}

func TestExecuteSequencePrintsTheWebSocketAddressOfASubscriptionTest(t *testing.T) {
	pt, cfg, _ := newJSONLPerf(t, "127.0.0.1", newHeadsRequest)
	cfg.WSAddress, cfg.WaitingTime = "127.0.0.1:9000", 0
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	err = pt.ExecuteSequence(context.Background(), TestSequence{{QPS: 0, Duration: 0}}, "erigon")
	os.Stdout = stdout
	w.Close()
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ExecuteSequence: %v", err)
	}
	if !strings.Contains(string(out), "Test on port: ws://127.0.0.1:9000\n") {
		t.Errorf("output %q: want the WebSocket address the connections open on", out)
	}
}

func TestFormatSubscriptionResultsSharesTheNotificationsByType(t *testing.T) {
	row := func(requests int) *apiResult {
		a := &apiResult{requests: requests, errors: map[string]int{}}
		for range requests {
			a.latencies.Add(time.Second)
		}
		return a
	}
	lines := formatSubscriptionResults(map[string]*apiResult{
		"newHeads latency": row(1),
		"logs latency":     row(3),
		"logs vs newHeads": row(3),
		"logs fan-out":     row(2),
	}, false)
	if len(lines) != 5 {
		t.Fatalf("lines %q: want a header and 4 rows", lines)
	}
	share := map[string]string{}
	for _, line := range lines[1:] {
		m := regexp.MustCompile(`^\s+(.+?)\s+\d+\s+(\S+)`).FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("row %q: want name, reqs and share", line)
		}
		share[m[1]] = m[2]
	}
	want := map[string]string{"newHeads latency": "25.0%", "logs latency": "75.0%", "logs vs newHeads": "-", "logs fan-out": "-"}
	if fmt.Sprint(share) != fmt.Sprint(want) {
		t.Errorf("share %v, want %v: the latency rows share the notifications, the other rows count them again", share, want)
	}
}
