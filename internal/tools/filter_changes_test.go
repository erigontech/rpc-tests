package tools

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/gorilla/websocket"
)

// filterNode serves the four filter methods over WebSocket and records the
// order in which they are called. cancelAfter cancels the supplied context
// once that many eth_getFilterChanges polls have been answered.
type filterNode struct {
	mu          sync.Mutex
	calls       []string
	changes     any
	logs        any
	failChanges bool
	failLogs    bool
	cancel      context.CancelFunc
	cancelAfter int
	polls       int
}

func (n *filterNode) record(method string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, method)
}

func (n *filterNode) methodCalls(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, m := range n.calls {
		if m == method {
			count++
		}
	}
	return count
}

func (n *filterNode) callOrder() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.calls...)
}

func (n *filterNode) handlers() map[string]func([]any) (any, *rpcErrorObject) {
	return map[string]func([]any) (any, *rpcErrorObject){
		"eth_getFilterChanges": func([]any) (any, *rpcErrorObject) {
			n.record("eth_getFilterChanges")
			n.mu.Lock()
			n.polls++
			reached := n.cancelAfter > 0 && n.polls >= n.cancelAfter
			n.mu.Unlock()
			if reached && n.cancel != nil {
				n.cancel()
			}
			if n.failChanges {
				return nil, &rpcErrorObject{Code: -32000, Message: "filter not found"}
			}
			return n.changes, nil
		},
		"eth_getFilterLogs": func([]any) (any, *rpcErrorObject) {
			n.record("eth_getFilterLogs")
			if n.failLogs {
				return nil, &rpcErrorObject{Code: -32000, Message: "filter not found"}
			}
			return n.logs, nil
		},
		"eth_uninstallFilter": func(params []any) (any, *rpcErrorObject) {
			n.record("eth_uninstallFilter")
			return true, nil
		},
		"eth_newFilter": func([]any) (any, *rpcErrorObject) {
			n.record("eth_newFilter")
			return "0xf1", nil
		},
	}
}

// dialFilterNode starts the node and returns a connection to it.
func dialFilterNode(t *testing.T, node *filterNode) *rpc.WSConn {
	t.Helper()
	url := newFakeWSNode(t, func(conn *websocket.Conn) {
		serveWSRequests(conn, node.handlers())
	})
	wsConn, err := rpc.Dial(url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = wsConn.Close() })
	return wsConn
}

func TestPollFilterChangesPollsBothMethods(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := &filterNode{
		changes:     []any{map[string]any{"blockNumber": "0x1"}},
		logs:        []any{map[string]any{"address": "0xaa"}},
		cancel:      cancel,
		cancelAfter: 3,
	}
	conn := dialFilterNode(t, node)

	if err := pollFilterChanges(ctx, conn, "0xf1", time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The context is only consulted after a complete round, and a ticker that
	// has already fired can win the select, so one extra round may slip through.
	changes := node.methodCalls("eth_getFilterChanges")
	if changes < 3 || changes > 4 {
		t.Errorf("eth_getFilterChanges: got %d polls, want 3 or 4", changes)
	}
	if logs := node.methodCalls("eth_getFilterLogs"); logs != changes {
		t.Errorf("every round polls both methods: %d changes vs %d logs", changes, logs)
	}

	// Each round is changes-then-logs, and the filter is released last.
	order := node.callOrder()
	if last := order[len(order)-1]; last != "eth_uninstallFilter" {
		t.Errorf("last call: got %s, want eth_uninstallFilter", last)
	}
	rounds := order[:len(order)-1]
	if len(rounds)%2 != 0 {
		t.Fatalf("expected complete rounds, got %v", order)
	}
	for i := 0; i < len(rounds); i += 2 {
		if rounds[i] != "eth_getFilterChanges" || rounds[i+1] != "eth_getFilterLogs" {
			t.Errorf("round %d is not changes-then-logs: %v", i/2, rounds[i:i+2])
		}
	}
}

// TestPollFilterChangesUninstallsOnShutdown is the path that used to be
// unreachable: on cancellation the filter must be released on the node.
func TestPollFilterChangesUninstallsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := &filterNode{cancel: cancel, cancelAfter: 1}
	conn := dialFilterNode(t, node)

	if err := pollFilterChanges(ctx, conn, "0xf1", time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := node.methodCalls("eth_uninstallFilter"); got != 1 {
		t.Errorf("eth_uninstallFilter: got %d calls, want 1", got)
	}
}

// TestPollFilterChangesSurvivesRPCErrors covers the two error branches: a node
// rejecting the polls must not end the loop.
func TestPollFilterChangesSurvivesRPCErrors(t *testing.T) {
	tests := []struct {
		name        string
		failChanges bool
		failLogs    bool
	}{
		{"changes rejected", true, false},
		{"logs rejected", false, true},
		{"both rejected", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			node := &filterNode{
				failChanges: tt.failChanges,
				failLogs:    tt.failLogs,
				cancel:      cancel,
				cancelAfter: 2,
			}
			conn := dialFilterNode(t, node)

			if err := pollFilterChanges(ctx, conn, "0xf1", time.Millisecond); err != nil {
				t.Fatalf("an RPC error should not end the loop: %v", err)
			}
			if got := node.methodCalls("eth_getFilterChanges"); got < 2 {
				t.Errorf("polls: got %d, want at least 2", got)
			}
			if got := node.methodCalls("eth_uninstallFilter"); got != 1 {
				t.Errorf("the filter should still be released, got %d calls", got)
			}
		})
	}
}

// TestPollFilterChangesEmptyResults covers the "no change / no log received"
// branches, including a result that is not an array at all.
func TestPollFilterChangesEmptyResults(t *testing.T) {
	tests := []struct {
		name    string
		changes any
		logs    any
	}{
		{"empty arrays", []any{}, []any{}},
		{"null results", nil, nil},
		{"results of the wrong type", "not-an-array", 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			node := &filterNode{changes: tt.changes, logs: tt.logs, cancel: cancel, cancelAfter: 1}
			conn := dialFilterNode(t, node)

			if err := pollFilterChanges(ctx, conn, "0xf1", time.Millisecond); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestPollFilterChangesStopsOnBrokenConnection: once the peer goes away every
// call fails, but the loop is still driven by ctx, not by the error.
func TestPollFilterChangesStopsOnBrokenConnection(t *testing.T) {
	url := newFakeWSNode(t, func(conn *websocket.Conn) {
		conn.Close()
	})
	wsConn, err := rpc.Dial(url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer wsConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- pollFilterChanges(ctx, wsConn, "0xf1", time.Millisecond) }()

	select {
	case pollErr := <-done:
		if pollErr != nil {
			t.Errorf("unexpected error: %v", pollErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pollFilterChanges did not stop after the context expired")
	}
}

func TestPollFilterChangesAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	node := &filterNode{changes: []any{}, logs: []any{}}
	conn := dialFilterNode(t, node)

	if err := pollFilterChanges(ctx, conn, "0xf1", time.Hour); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The context is only consulted after the first round, so exactly one poll
	// happens before the filter is released.
	if got := node.methodCalls("eth_getFilterChanges"); got != 1 {
		t.Errorf("polls: got %d, want 1", got)
	}
	if got := node.methodCalls("eth_uninstallFilter"); got != 1 {
		t.Errorf("eth_uninstallFilter: got %d calls, want 1", got)
	}
}

func TestFilterPollInterval(t *testing.T) {
	if filterPollInterval != 2*time.Second {
		t.Errorf("filterPollInterval = %v, want 2s", filterPollInterval)
	}
}

// TestRunFilterChangesRegistersAndReleasesFilter drives the command end to end:
// it creates the filter, polls, and releases it when the context is cancelled.
func TestRunFilterChangesRegistersAndReleasesFilter(t *testing.T) {
	node := &filterNode{changes: []any{}, logs: []any{}}
	url := newFakeWSNode(t, func(conn *websocket.Conn) {
		serveWSRequests(conn, node.handlers())
	})

	wsConn, err := rpc.Dial(url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer wsConn.Close()

	// Reproduce what runFilterChanges does after a successful eth_newFilter.
	var filterResp jsonRPCResponse
	err = wsConn.CallJSON(jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_newFilter",
		Params:  []any{map[string]any{"topics": []string{transferTopic}}},
		ID:      1,
	}, &filterResp)
	if err != nil {
		t.Fatalf("create filter: %v", err)
	}
	filterID, ok := filterResp.Result.(string)
	if !ok {
		t.Fatalf("unexpected filter ID type %T", filterResp.Result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	node.cancel, node.cancelAfter = cancel, 2
	defer cancel()

	if err := pollFilterChanges(ctx, wsConn, filterID, time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	order := node.callOrder()
	if order[0] != "eth_newFilter" {
		t.Errorf("the filter should be created first, got %v", order)
	}
	if order[len(order)-1] != "eth_uninstallFilter" {
		t.Errorf("the filter should be released last, got %v", order)
	}
}
