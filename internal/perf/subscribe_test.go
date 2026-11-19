package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
	txs       int // transactions of the block, each with a receipt holding one log
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
	// receipts returns the receipts of h sent to conn, as the notifications that carry them; nil = all
	// the receipts of h in one notification.
	receipts func(conn int, h fakeHead) [][]int
	// noReceiptLogs sends the receipts without logs, so without a block timestamp.
	noReceiptLogs bool
	// receiptObjects sends each receipt as its own notification, an object instead of a list.
	receiptObjects bool

	// linesOf, when set, is the subscriptions of a connection whose first one is of typ, for connections
	// that hold different numbers of them; lines is then unused and total is the sum over the connections.
	linesOf func(typ string) int
	total   int

	mu       sync.Mutex
	accepted int
	ready    chan struct{}
	subs     int
	types    map[int][]string // by connection, the types subscribed on it
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
	switch typ {
	case "newHeads":
		return []string{fmt.Sprintf(`{"number":"0x%x","hash":%q,"timestamp":"0x%x"}`, h.number, h.hash, h.timestamp.Unix())}
	case "transactionReceipts":
		var groups [][]int
		if n.receipts != nil {
			groups = n.receipts(conn, h)
		} else if h.txs > 0 {
			groups = [][]int{make([]int, h.txs)}
			for i := range groups[0] {
				groups[0][i] = i
			}
		}
		var out []string
		for _, group := range groups {
			receipts := make([]string, len(group))
			for i, tx := range group {
				logs := n.log(h, tx)
				if n.noReceiptLogs {
					logs = ""
				}
				receipts[i] = fmt.Sprintf(`{"blockNumber":"0x%x","blockHash":%q,"transactionHash":"0x%064x","transactionIndex":"0x%x","logs":[%s]}`,
					h.number, h.hash, tx, tx, logs)
			}
			if n.receiptObjects {
				out = append(out, receipts...)
				continue
			}
			out = append(out, "["+strings.Join(receipts, ",")+"]")
		}
		return out
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
	if req.Method == "eth_getBlockTransactionCountByNumber" {
		var number string
		json.Unmarshal(req.Params[0], &number) //nolint:errcheck
		result := "null"
		for _, h := range n.heads {
			if fmt.Sprintf("0x%x", h.number) == number {
				result = fmt.Sprintf(`"0x%x"`, h.txs)
			}
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":%s}`, result)
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
			n.mu.Lock()
			if n.types == nil {
				n.types = map[int][]string{}
			}
			n.types[conn] = append(n.types[conn], fmt.Sprint(req.Params[0]))
			n.mu.Unlock()
		}
		if err := c.WriteMessage(websocket.TextMessage, resp); err != nil {
			return subs
		}
		n.mu.Lock()
		n.subs++
		want := n.perConn()
		if n.linesOf != nil && len(subs) > 0 {
			want = n.linesOf(subs[0].typ)
		}
		if n.subs == n.conns*n.perConn() && n.linesOf == nil || n.subs == n.total && n.linesOf != nil {
			close(n.ready)
		}
		n.mu.Unlock()
		if answered == want {
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
	for _, req := range []string{
		`{"jsonrpc":"2.0","method":"eth_subscribe","params":["newPendingTransactions"],"id":1}`,
		`{"jsonrpc":"2.0","method":"eth_subscribe","params":["transactionReceipts",{"transactionHashes":["0x01"]}],"id":1}`,
	} {
		if _, err := subscriptionLines([][]byte{[]byte(req)}); err == nil {
			t.Errorf("%s: want an error, it is not measured", req)
		}
	}
	for _, req := range []string{newHeadsRequest, logsRequest, receiptsRequest} {
		if _, err := subscriptionLines([][]byte{[]byte(req)}); err != nil {
			t.Errorf("%s: %v", req, err)
		}
	}
}

func TestExecuteRunsASubscriptionPatternOnTheConnections(t *testing.T) {
	node := &wsNode{conns: 3, heads: heads(1)}
	addr := startWSNode(t, node)
	pt, cfg, _ := newJSONLPerf(t, "127.0.0.1:1", newHeadsRequest)
	cfg.WSAddress = addr

	format := ResultFormat{MaxRepetitionDigits: 1, MaxQpsDigits: 1, MaxDurationDigits: 1}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	err = pt.Execute(context.Background(), 1, 0, "rpcdaemon", 3, 1, format)
	os.Stdout = stdout
	w.Close()
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "[1.1] rpcdaemon: executes test conn: 3 time: 1 -> ") {
		t.Errorf("output %q: want the number of connections labelled conn", out)
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.accepted != 3 {
		t.Errorf("%d connections: want the 3 of the qps column", node.accepted)
	}
}

func TestExecuteReportsTheRowsOfASubscriptionPattern(t *testing.T) {
	node := &wsNode{conns: 3, heads: heads(1)}
	addr := startWSNode(t, node)
	pt, cfg, _ := newJSONLPerf(t, "127.0.0.1:1", newHeadsRequest)
	cfg.WSAddress = addr
	cfg.CreateTestReport = true
	cfg.JSONReportFile = filepath.Join(t.TempDir(), "report.json")
	quietStdout(t)
	if err := pt.Report.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	format := ResultFormat{MaxRepetitionDigits: 1, MaxQpsDigits: 1, MaxDurationDigits: 1}
	if err := pt.Execute(context.Background(), 1, 0, "rpcdaemon", 3, 1, format); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := pt.Report.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(cfg.JSONReportFile)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Results []struct {
			TestRepetitions []struct {
				APIs map[string]APIInfo `json:"apis"`
			} `json:"testRepetitions"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	row, ok := parsed.Results[0].TestRepetitions[0].APIs["newHeads latency"]
	if !ok || row.Requests != 3 || row.Errors != 0 {
		t.Errorf("apis = %+v, want a newHeads latency row with the head of each of the 3 connections", parsed.Results[0].TestRepetitions[0].APIs)
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

func TestRunSubscriptionsComparesIdenticalSubscriptionsOfOneConnection(t *testing.T) {
	node := &wsNode{conns: 1, lines: 3, heads: heads(2)}
	addr := startWSNode(t, node)
	reqs := [][]byte{[]byte(newHeadsRequest), []byte(newHeadsRequest), []byte(`{"jsonrpc":"2.0","method":"eth_subscribe","params":["newHeads"],"id":7}`)}

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, reqs, 1, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	if row := results.apis["newHeads fan-out"]; row == nil || row.requests != 4 {
		t.Errorf("fan-out row %+v: want the 2 slower of the 3 identical subscriptions, for each of the 2 heads", row)
	}
}

func TestRunSubscriptionsKeepsLogsFiltersApartInTheFanOut(t *testing.T) {
	node := &wsNode{conns: 1, lines: 2, heads: headsWithLogs(1, 2)}
	addr := startWSNode(t, node)
	other := `{"jsonrpc":"2.0","method":"eth_subscribe","params":["logs",{"address":"0x02","topics":["0x7e"]}],"id":1}`

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(logsRequest), []byte(other)}, 1, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	if row := results.apis["logs fan-out"]; row != nil {
		t.Errorf("fan-out row %+v: two different filters receive different logs, they are not compared", row)
	}
}

const receiptsRequest = `{"jsonrpc":"2.0","method":"eth_subscribe","params":["transactionReceipts"],"id":1}`

func headsWithTxs(count, txs int) []fakeHead {
	hs := heads(count)
	for i := range hs {
		hs[i].txs = txs
	}
	return hs
}

func TestRunSubscriptionsMeasuresReceiptsAgainstTheirHead(t *testing.T) {
	node := &wsNode{conns: 2, lines: 2, heads: headsWithTxs(2, 3), delay: func(conn int) time.Duration { return time.Duration(conn) * 100 * time.Millisecond }}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(newHeadsRequest), []byte(receiptsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	for row, want := range map[string]int{
		"transactionReceipts latency":     4, // 2 connections x 2 blocks, one notification per block
		"transactionReceipts fan-out":     2,
		"transactionReceipts vs newHeads": 4,
	} {
		if got := results.apis[row]; got == nil || got.requests != want || len(got.errors) > 0 {
			t.Errorf("%s row %+v: want %d requests and no error", row, got, want)
		}
	}
	if got := results.apis["transactionReceipts fan-out"].latencies.Max; got < 100*time.Millisecond {
		t.Errorf("fan-out max %v: want the 100 ms delay of the second connection", got)
	}
}

func TestRunSubscriptionsChecksOneReceiptsNotificationPerBlock(t *testing.T) {
	node := &wsNode{conns: 1, heads: headsWithTxs(2, 3), receipts: func(int, fakeHead) [][]int { return [][]int{{0}, {1}, {2}} }}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(receiptsRequest)}, 1, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	row := results.apis["transactionReceipts latency"]
	if row == nil || row.requests != 6 || fmt.Sprint(row.errors) != fmt.Sprint(map[string]int{errGranularity: 4}) {
		t.Errorf("row %+v: want one notification per receipt as 2 extra notifications per block", row)
	}
}

func TestRunSubscriptionsMeasuresReceiptsSentOneObjectPerReceipt(t *testing.T) {
	node := &wsNode{conns: 2, heads: headsWithTxs(2, 3), receiptObjects: true}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(receiptsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	for row, want := range map[string]int{
		"transactionReceipts latency":     4, // 2 connections x 2 blocks, the receipts of a block counted once
		"transactionReceipts per receipt": 4,
	} {
		if got := results.apis[row]; got == nil || got.requests != want || len(got.errors) > 0 {
			t.Errorf("%s row %+v: want %d requests and no error", row, got, want)
		}
	}
}

func TestRunSubscriptionsCountsAReceiptObjectThatDidNotArrive(t *testing.T) {
	hs := headsWithTxs(2, 3)
	node := &wsNode{conns: 1, heads: hs, receiptObjects: true, receipts: func(_ int, h fakeHead) [][]int {
		if h.number == hs[1].number {
			return [][]int{{0, 2}}
		}
		return [][]int{{0, 1, 2}}
	}}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(receiptsRequest)}, 1, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	row := results.apis["transactionReceipts latency"]
	if row == nil || fmt.Sprint(row.errors) != fmt.Sprint(map[string]int{errReceiptCount: 1}) {
		t.Errorf("row %+v: want the block with 2 of its 3 receipts counted as a wrong receipt count", row)
	}
}

func TestRunSubscriptionsCountsMissingAndIncompleteReceipts(t *testing.T) {
	hs := headsWithTxs(3, 3)
	hs[2].txs = 0 // an empty block has no receipts notification
	node := &wsNode{conns: 2, heads: hs, receipts: func(conn int, h fakeHead) [][]int {
		switch {
		case h.txs == 0, conn == 1 && h.number == hs[0].number:
			return nil
		case conn == 0 && h.number == hs[1].number:
			return [][]int{{0, 1}}
		}
		return [][]int{{0, 1, 2}}
	}}
	addr := startWSNode(t, node)

	_, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(receiptsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	row := results.apis["transactionReceipts latency"]
	if row == nil || fmt.Sprint(row.errors) != fmt.Sprint(map[string]int{errMissing: 1, errReceiptCount: 1}) {
		t.Errorf("row %+v: want block 0 missing on connection 1 and block 1 incomplete on connection 0", row)
	}
}

func TestRunSubscriptionsGivesNoLatencyToANotificationWithoutTimestamp(t *testing.T) {
	node := &wsNode{conns: 1, heads: headsWithTxs(1, 2), noReceiptLogs: true, receipts: func(int, fakeHead) [][]int { return [][]int{{0}, {1}} }}
	addr := startWSNode(t, node)

	metrics, results, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(receiptsRequest)}, 1, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	row := results.apis["transactionReceipts latency"]
	if row == nil || row.requests != 2 {
		t.Fatalf("row %+v: want the 2 notifications of the block", row)
	}
	if metrics.Latencies.Max > time.Minute || metrics.Latencies.Min < 0 || row.latencies.Max > time.Minute {
		t.Errorf("latencies %v..%v: a notification without block timestamp has no latency", metrics.Latencies.Min, metrics.Latencies.Max)
	}
}

func TestListenTakesTheArrivalOfANotificationWhileTheOneBeforeIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for _, note := range []string{
			`{"jsonrpc":"2.0","method":"eth_subscription","params":{"subscription":"0x1","result":[{"blockNumber":"0x64","blockHash":"0x01","logs":[]}]}}`,
			`{"jsonrpc":"2.0","method":"eth_subscription","params":{"subscription":"0x2","result":{"number":"0x64","hash":"0x01","timestamp":"0x0"}}}`,
		} {
			if c.WriteMessage(websocket.TextMessage, []byte(note)) != nil {
				return
			}
		}
		_, _, _ = c.ReadMessage()
	}))
	defer srv.Close()
	c, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	resp.Body.Close()
	defer c.Close()

	const parseTime = 300 * time.Millisecond
	parse := parseResult
	parseResult = func(typ string, result json.RawMessage) (notification, string, string, bool) {
		if typ == "transactionReceipts" {
			time.Sleep(parseTime)
		}
		return parse(typ, result)
	}
	defer func() { parseResult = parse }()

	lines := []subscriptionLine{{typ: "transactionReceipts"}, {typ: "newHeads", group: 1}}
	notes, _ := listen(c, map[string]int{"0x1": 0, "0x2": 1}, lines, time.Now().Add(parseTime+time.Second), make(chan struct{}, 1))
	if len(notes) != 2 {
		t.Fatalf("notifications %d: want the receipts and the head", len(notes))
	}
	if gap := notes[1].arrival.Sub(notes[0].arrival); gap >= parseTime/2 {
		t.Errorf("the head arrived %v after the receipts: want its read time, not the end of the parsing of the receipts", gap)
	}
}

func TestRunSubscriptionsParsesOnFewerGoroutinesThanTheReaders(t *testing.T) {
	node := &wsNode{conns: 4, heads: heads(3)}
	addr := startWSNode(t, node)

	workers, parse := parseWorkers, parseResult
	defer func() { parseWorkers, parseResult = workers, parse }()
	parseWorkers = 2
	var mu sync.Mutex
	running, most := 0, 0
	parseResult = func(typ string, result json.RawMessage) (notification, string, string, bool) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return parse(typ, result)
	}

	_, _, err := runSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(newHeadsRequest)}, 4, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runSubscriptions: %v", err)
	}
	if most != 2 {
		t.Errorf("notifications parsed at once %d: want at most the 2 parse workers, and all of them used", most)
	}
}

func TestRunSubscriptionsSendsTheLinesOfItsClassOnEachConnection(t *testing.T) {
	node := &wsNode{conns: 4, total: 4, linesOf: func(string) int { return 1 }, heads: headsWithTxs(2, 3)}
	addr := startWSNode(t, node)
	clients := []ClientClass{{Name: "wallet", Share: 3, First: 0, Count: 1}, {Name: "indexer", Share: 1, First: 1, Count: 1}}

	_, results, err := runClientSubscriptions(context.Background(), "ws://"+addr, "http://"+addr, [][]byte{[]byte(newHeadsRequest), []byte(receiptsRequest)}, clients, 4, time.Second, filepath.Join(t.TempDir(), "run.bin"))
	if err != nil {
		t.Fatalf("runClientSubscriptions: %v", err)
	}
	byTypes := map[string]int{}
	for _, types := range node.types {
		byTypes[strings.Join(types, ",")]++
	}
	if want := map[string]int{"newHeads": 3, "transactionReceipts": 1}; !maps.Equal(byTypes, want) {
		t.Errorf("connections by subscribed types %v, want %v", byTypes, want)
	}
	for row, want := range map[string]int{
		"newHeads latency":                    6, // 3 connections x 2 blocks
		"newHeads fan-out":                    4,
		"transactionReceipts latency":         2,
		"newHeads latency wallet":             6,
		"newHeads fan-out wallet":             4,
		"transactionReceipts latency indexer": 2,
	} {
		if got := results.apis[row]; got == nil || got.requests != want || len(got.errors) > 0 {
			t.Errorf("%s row %+v: want %d requests and no error", row, got, want)
		}
	}
}

func TestExecuteSplitsTheConnectionsByTheClientClassesOfThePattern(t *testing.T) {
	node := &wsNode{conns: 4, total: 4, linesOf: func(string) int { return 1 }, heads: headsWithTxs(1, 2)}
	addr := startWSNode(t, node)
	header := `{"pattern":{"description":"classes","clients":[{"name":"wallet","share":3,"first":0,"count":1},{"name":"indexer","share":1,"first":1,"count":1}]}}`
	pt, cfg, _ := newJSONLPerf(t, addr, header, newHeadsRequest, receiptsRequest)
	cfg.WSAddress = addr

	format := ResultFormat{MaxRepetitionDigits: 1, MaxQpsDigits: 1, MaxDurationDigits: 1}
	if err := pt.Execute(context.Background(), 1, 0, "rpcdaemon", 4, 1, format); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	byTypes := map[string]int{}
	for _, types := range node.types {
		byTypes[strings.Join(types, ",")]++
	}
	if want := map[string]int{"newHeads": 3, "transactionReceipts": 1}; !maps.Equal(byTypes, want) {
		t.Errorf("connections by subscribed types %v, want %v", byTypes, want)
	}
}
