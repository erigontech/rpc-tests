package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const newHeadsRequest = `{"jsonrpc":"2.0","method":"eth_subscribe","params":["newHeads"],"id":1}`

type fakeHead struct {
	number    uint64
	hash      string
	timestamp time.Time
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
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		n.mu.Lock()
		conn := n.accepted
		n.accepted++
		n.mu.Unlock()
		subIDs := n.subscribe(t, c, conn)
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
				for _, id := range subIDs {
					note := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_subscription","params":{"subscription":%q,"result":{"number":"0x%x","hash":%q,"timestamp":"0x%x"}}}`,
						id, h.number, h.hash, h.timestamp.Unix())
					if c.WriteMessage(websocket.TextMessage, []byte(note)) != nil {
						return
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

// subscribe answers the eth_subscribe requests of a connection until it holds one subscription per
// pattern line, and returns their ids.
func (n *wsNode) subscribe(t *testing.T, c *websocket.Conn, conn int) []string {
	var ids []string
	for answered := 1; ; answered++ {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return ids
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(msg, &req); err != nil || req.Method != "eth_subscribe" {
			t.Errorf("unexpected request %s", msg)
			return ids
		}
		id := fmt.Sprintf("0x%d%d", conn, answered)
		resp := fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%s,"result":%q}`, req.ID, id)
		if n.reject != nil && n.reject(conn) {
			resp = fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"too many subscriptions"}}`, req.ID)
		} else {
			ids = append(ids, id)
		}
		if err := c.WriteMessage(websocket.TextMessage, resp); err != nil {
			return ids
		}
		n.mu.Lock()
		n.subs++
		if n.subs == n.conns*n.perConn() {
			close(n.ready)
		}
		n.mu.Unlock()
		if answered == n.perConn() {
			return ids
		}
	}
}

func (n *wsNode) perConn() int { return 1 }

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

	metrics, results, err := runSubscriptions(context.Background(), addr, [][]byte{[]byte(newHeadsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
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

	metrics, results, err := runSubscriptions(context.Background(), addr, [][]byte{[]byte(newHeadsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
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
	logs := []byte(`{"jsonrpc":"2.0","method":"eth_subscribe","params":["logs",{}],"id":1}`)
	if _, _, err := runSubscriptions(context.Background(), "localhost:1", [][]byte{logs}, 1, time.Second, filepath.Join(t.TempDir(), "run.bin")); err == nil {
		t.Error("logs subscription: want an error, only newHeads is measured")
	}
}

func TestExecuteRunsASubscriptionPatternOnTheConnections(t *testing.T) {
	node := &wsNode{conns: 3, heads: heads(1)}
	addr := startWSNode(t, node)
	pt, _, _ := newJSONLPerf(t, addr, newHeadsRequest)

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

	_, results, err := runSubscriptions(context.Background(), addr, [][]byte{[]byte(newHeadsRequest)}, 2, time.Second, filepath.Join(t.TempDir(), "run.bin"))
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

	_, results, err := runSubscriptions(context.Background(), addr, [][]byte{[]byte(newHeadsRequest)}, 3, time.Second, filepath.Join(t.TempDir(), "run.bin"))
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
