package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/erigontech/rpc-tests/internal/rpc"
)

func TestCountReceiptLogs(t *testing.T) {
	tests := []struct {
		name     string
		receipts []any
		want     int
	}{
		{"nil", nil, 0},
		{"empty", []any{}, 0},
		{
			name:     "single receipt with logs",
			receipts: []any{map[string]any{"logs": []any{1, 2, 3}}},
			want:     3,
		},
		{
			name: "sums across receipts",
			receipts: []any{
				map[string]any{"logs": []any{1}},
				map[string]any{"logs": []any{1, 2}},
			},
			want: 3,
		},
		{
			name:     "receipt without a logs field",
			receipts: []any{map[string]any{"status": "0x1"}},
			want:     0,
		},
		{
			name:     "logs field of the wrong type",
			receipts: []any{map[string]any{"logs": "not-an-array"}},
			want:     0,
		},
		{
			name:     "non-object entries are skipped",
			receipts: []any{"garbage", 42, map[string]any{"logs": []any{1}}},
			want:     1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countReceiptLogs(tt.receipts); got != tt.want {
				t.Errorf("countReceiptLogs = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetBlock(t *testing.T) {
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockByNumber": func(params []any) (any, *rpcErrorObject) {
			if params[0] != "latest" {
				t.Errorf("tag: got %v, want latest", params[0])
			}
			if params[1] != false {
				t.Errorf("full-transactions flag: got %v, want false", params[1])
			}
			return map[string]any{"number": "0x64", "hash": "0xdead"}, nil
		},
	})

	got, err := getBlock(context.Background(), rpc.NewClient("http", "", 0), node.target(), "latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["number"] != "0x64" {
		t.Errorf("number: got %v, want 0x64", got["number"])
	}
}

func TestGetBlockErrors(t *testing.T) {
	client := rpc.NewClient("http", "", 0)
	ctx := context.Background()

	t.Run("transport failure", func(t *testing.T) {
		if _, err := getBlock(ctx, client, unusedPort, "latest"); err == nil {
			t.Error("expected an error for an unreachable node")
		}
	})

	t.Run("null result", func(t *testing.T) {
		node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) { return nil, nil },
		})
		_, err := getBlock(ctx, client, node.target(), "latest")
		if err == nil || !strings.Contains(err.Error(), "unexpected result type") {
			t.Errorf("got %v, want an 'unexpected result type' error", err)
		}
	})
}

func TestGetLogs(t *testing.T) {
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getLogs": func(params []any) (any, *rpcErrorObject) {
			filter, ok := params[0].(map[string]any)
			if !ok {
				t.Fatalf("params[0] is %T, want an object", params[0])
			}
			if filter["blockHash"] != "0xdead" {
				t.Errorf("blockHash: got %v, want 0xdead", filter["blockHash"])
			}
			return []any{map[string]any{"address": "0x01"}}, nil
		},
	})

	logs, err := getLogs(context.Background(), rpc.NewClient("http", "", 0), node.target(), "0xdead")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("logs: got %d, want 1", len(logs))
	}
}

func TestGetLogsRPCError(t *testing.T) {
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getLogs": func([]any) (any, *rpcErrorObject) {
			return nil, &rpcErrorObject{Code: -32000, Message: "unknown block"}
		},
	})
	_, err := getLogs(context.Background(), rpc.NewClient("http", "", 0), node.target(), "0xdead")
	if err == nil || !strings.Contains(err.Error(), "RPC error") {
		t.Errorf("got %v, want an RPC error", err)
	}
}

// TestGetLogsNullResult documents that a null result is reported as an empty
// log list rather than an error.
func TestGetLogsNullResult(t *testing.T) {
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getLogs": func([]any) (any, *rpcErrorObject) { return nil, nil },
	})
	logs, err := getLogs(context.Background(), rpc.NewClient("http", "", 0), node.target(), "0xdead")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logs != nil {
		t.Errorf("logs: got %v, want nil", logs)
	}
}

func TestGetBlockReceipts(t *testing.T) {
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockReceipts": func(params []any) (any, *rpcErrorObject) {
			if params[0] != "0x64" {
				t.Errorf("block number: got %v, want 0x64", params[0])
			}
			return []any{map[string]any{"logs": []any{1, 2}}}, nil
		},
	})

	receipts, err := getBlockReceipts(context.Background(), rpc.NewClient("http", "", 0), node.target(), "0x64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := countReceiptLogs(receipts); got != 2 {
		t.Errorf("log count: got %d, want 2", got)
	}
}

func TestGetBlockReceiptsErrors(t *testing.T) {
	client := rpc.NewClient("http", "", 0)
	ctx := context.Background()

	t.Run("transport failure", func(t *testing.T) {
		if _, err := getBlockReceipts(ctx, client, unusedPort, "0x1"); err == nil {
			t.Error("expected an error for an unreachable node")
		}
	})

	t.Run("rpc error", func(t *testing.T) {
		node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockReceipts": func([]any) (any, *rpcErrorObject) {
				return nil, &rpcErrorObject{Code: -32000, Message: "boom"}
			},
		})
		if _, err := getBlockReceipts(ctx, client, node.target(), "0x1"); err == nil {
			t.Error("expected an RPC error")
		}
	})
}

// TestEmptyTrieRootConstant guards the constant used to decide whether a block
// with zero logs is legitimately empty.
func TestEmptyTrieRootConstant(t *testing.T) {
	const want = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	if emptyTrieRoot != want {
		t.Errorf("emptyTrieRoot = %s, want %s", emptyTrieRoot, want)
	}
}
