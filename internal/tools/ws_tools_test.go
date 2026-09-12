package tools

import (
	"strings"
	"sync"
	"testing"

	"github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/gorilla/websocket"
)

func TestGetBlockNumber(t *testing.T) {
	var gotTags []string
	var mu sync.Mutex
	url := newFakeWSNode(t, func(conn *websocket.Conn) {
		serveWSRequests(conn, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockByNumber": func(params []any) (any, *rpcErrorObject) {
				tag, _ := params[0].(string)
				mu.Lock()
				gotTags = append(gotTags, tag)
				mu.Unlock()
				return map[string]any{"number": "0x1234"}, nil
			},
		})
	})

	conn, err := rpc.Dial(url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for i, tag := range []string{"latest", "safe", "finalized"} {
		got, err := getBlockNumber(conn, tag, i+1)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tag, err)
		}
		if got != "0x1234" {
			t.Errorf("%s: got %q, want 0x1234", tag, got)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if strings.Join(gotTags, ",") != "latest,safe,finalized" {
		t.Errorf("tags: got %v", gotTags)
	}
}

func TestGetBlockNumberErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler func([]any) (any, *rpcErrorObject)
		want    string
	}{
		{
			name:    "rpc error",
			handler: func([]any) (any, *rpcErrorObject) { return nil, &rpcErrorObject{Code: -32000, Message: "no head"} },
			want:    "RPC error",
		},
		{
			name:    "result is not an object",
			handler: func([]any) (any, *rpcErrorObject) { return "0x1", nil },
			want:    "unexpected result type",
		},
		{
			name:    "block without a number field",
			handler: func([]any) (any, *rpcErrorObject) { return map[string]any{"hash": "0xaa"}, nil },
			want:    "missing number field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := newFakeWSNode(t, func(conn *websocket.Conn) {
				serveWSRequests(conn, map[string]func([]any) (any, *rpcErrorObject){
					"eth_getBlockByNumber": tt.handler,
				})
			})
			conn, err := rpc.Dial(url)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()

			_, err = getBlockNumber(conn, "latest", 1)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestGetBlockNumberOnClosedConnection(t *testing.T) {
	url := newFakeWSNode(t, func(conn *websocket.Conn) {
		conn.Close()
	})
	conn, err := rpc.Dial(url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := getBlockNumber(conn, "latest", 1); err == nil {
		t.Error("expected an error once the peer has gone away")
	}
}

func TestRunBlockByNumberDialFailure(t *testing.T) {
	err := runSubcommand("block-by-number", "--url", "ws://"+unusedPort)
	if err == nil {
		t.Fatal("expected an error when the node is unreachable")
	}
	if !strings.Contains(err.Error(), "connect to") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunFilterChangesDialFailure(t *testing.T) {
	err := runSubcommand("filter-changes", "--url", "ws://"+unusedPort)
	if err == nil {
		t.Fatal("expected an error when the node is unreachable")
	}
	if !strings.Contains(err.Error(), "connect to") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunFilterChangesFilterErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler func([]any) (any, *rpcErrorObject)
		want    string
	}{
		{
			name:    "node rejects the filter",
			handler: func([]any) (any, *rpcErrorObject) { return nil, &rpcErrorObject{Code: -32000, Message: "nope"} },
			want:    "create filter RPC error",
		},
		{
			name:    "filter id has the wrong type",
			handler: func([]any) (any, *rpcErrorObject) { return 12345, nil },
			want:    "unexpected filter ID type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := newFakeWSNode(t, func(conn *websocket.Conn) {
				serveWSRequests(conn, map[string]func([]any) (any, *rpcErrorObject){
					"eth_newFilter": tt.handler,
				})
			})
			err := runSubcommand("filter-changes", "--url", url)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// TestRunFilterChangesRegistersTransferTopic checks the ERC20 Transfer topic is
// what the filter is created with, before the command settles into its loop.
func TestRunFilterChangesRegistersTransferTopic(t *testing.T) {
	gotTopic := make(chan string, 1)
	url := newFakeWSNode(t, func(conn *websocket.Conn) {
		serveWSRequests(conn, map[string]func([]any) (any, *rpcErrorObject){
			"eth_newFilter": func(params []any) (any, *rpcErrorObject) {
				criteria, _ := params[0].(map[string]any)
				topics, _ := criteria["topics"].([]any)
				if len(topics) > 0 {
					topic, _ := topics[0].(string)
					select {
					case gotTopic <- topic:
					default:
					}
				}
				// Fail the next call so the command returns instead of looping.
				return nil, &rpcErrorObject{Code: -1, Message: "stop"}
			},
		})
	})

	_ = runSubcommand("filter-changes", "--url", url)

	select {
	case topic := <-gotTopic:
		if topic != transferTopic {
			t.Errorf("topic: got %s, want %s", topic, transferTopic)
		}
	default:
		t.Error("eth_newFilter was never called")
	}
}

func TestRunSubscriptionsDialFailure(t *testing.T) {
	err := runSubcommand("subscriptions", "--url", "ws://"+unusedPort)
	if err == nil {
		t.Fatal("expected an error when the node is unreachable")
	}
	if !strings.Contains(err.Error(), "connect to") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunSubscriptionsSubscribeErrors(t *testing.T) {
	tests := []struct {
		name  string
		fail  int // which eth_subscribe call fails (1 = newHeads, 2 = logs)
		want  string
		calls int
	}{
		{"newHeads rejected", 1, "subscribe newHeads RPC error", 1},
		{"logs rejected", 2, "subscribe logs RPC error", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			var mu sync.Mutex
			url := newFakeWSNode(t, func(conn *websocket.Conn) {
				serveWSRequests(conn, map[string]func([]any) (any, *rpcErrorObject){
					"eth_subscribe": func([]any) (any, *rpcErrorObject) {
						mu.Lock()
						calls++
						current := calls
						mu.Unlock()
						if current == tt.fail {
							return nil, &rpcErrorObject{Code: -32000, Message: "denied"}
						}
						return "0xsub", nil
					},
				})
			})

			err := runSubcommand("subscriptions", "--url", url)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if calls != tt.calls {
				t.Errorf("eth_subscribe calls: got %d, want %d", calls, tt.calls)
			}
		})
	}
}

// TestRunSubscriptionsConsumesNotifications drives the notification loop: the
// node answers both subscriptions, pushes one event for each, then hangs up,
// which is how the loop is expected to terminate with an error.
func TestRunSubscriptionsConsumesNotifications(t *testing.T) {
	url := newFakeWSNode(t, func(conn *websocket.Conn) {
		subIDs := []string{"0xnewheads", "0xlogs"}
		for i := range subIDs {
			var req rpcRequest
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			if err := conn.WriteJSON(map[string]any{
				"jsonrpc": "2.0", "id": req.ID, "result": subIDs[i],
			}); err != nil {
				return
			}
		}
		for _, id := range subIDs {
			_ = conn.WriteJSON(map[string]any{
				"jsonrpc": "2.0",
				"method":  "eth_subscription",
				"params":  map[string]any{"subscription": id, "result": map[string]any{"number": "0x1"}},
			})
		}
		conn.Close()
	})

	err := runSubcommand("subscriptions", "--url", url)
	if err == nil {
		t.Fatal("expected the loop to end with a receive error once the peer hangs up")
	}
	if !strings.Contains(err.Error(), "receive notification") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSubscriptionConstants(t *testing.T) {
	if !strings.HasPrefix(usdtAddress, "0x") || len(usdtAddress) != 42 {
		t.Errorf("usdtAddress %q is not a 20-byte hex address", usdtAddress)
	}
	for name, topic := range map[string]string{
		"transferTopic":     transferTopic,
		"transferTopicFull": transferTopicFull,
	} {
		if !strings.HasPrefix(topic, "0x") || len(topic) != 66 {
			t.Errorf("%s %q is not a 32-byte hex topic", name, topic)
		}
	}
}
