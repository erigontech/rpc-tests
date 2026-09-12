package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/erigontech/rpc-tests/internal/rpc"
)

// emptyBlocksNode serves eth_blockNumber plus eth_getBlockByNumber for a chain
// where blocks divisible by `emptyEvery` carry no transactions.
func emptyBlocksNode(t *testing.T, latest int64, emptyEvery int64, withWithdrawals bool) *fakeNode {
	t.Helper()
	return newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_blockNumber": func([]any) (any, *rpcErrorObject) {
			return fmt.Sprintf("0x%x", latest), nil
		},
		"eth_getBlockByNumber": func(params []any) (any, *rpcErrorObject) {
			tag, _ := params[0].(string)
			number := hexToInt64(tag)
			block := map[string]any{
				"number":     tag,
				"hash":       fmt.Sprintf("0x%064x", number),
				"parentHash": fmt.Sprintf("0x%064x", number-1),
				"stateRoot":  fmt.Sprintf("0x%064x", number/2),
			}
			if number%emptyEvery == 0 {
				block["transactions"] = []any{}
			} else {
				block["transactions"] = []any{"0xtx"}
			}
			if withWithdrawals {
				if number%(emptyEvery*2) == 0 {
					block["withdrawals"] = []any{map[string]any{"index": "0x1"}}
				} else {
					block["withdrawals"] = []any{}
				}
			}
			return block, nil
		},
	})
}

func TestFetchBlockInfo(t *testing.T) {
	node := emptyBlocksNode(t, 100, 2, true)

	got, err := fetchBlockInfo(context.Background(), rpc.NewClient("http", "", 0), node.target(), 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Number != 4 {
		t.Errorf("Number: got %d, want 4", got.Number)
	}
	if len(got.Transactions) != 0 {
		t.Errorf("Transactions: got %d, want 0", len(got.Transactions))
	}
	if !got.HasWithdrawals {
		t.Error("HasWithdrawals should be true when the field is present")
	}
	if got.StateRoot == "" {
		t.Error("StateRoot should be populated")
	}
	if got.ParentHash == "" {
		t.Error("ParentHash should be populated")
	}
}

// TestFetchBlockInfoWithoutWithdrawals covers a pre-Shanghai block, where the
// withdrawals field is absent altogether.
func TestFetchBlockInfoWithoutWithdrawals(t *testing.T) {
	node := emptyBlocksNode(t, 100, 2, false)

	got, err := fetchBlockInfo(context.Background(), rpc.NewClient("http", "", 0), node.target(), 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HasWithdrawals {
		t.Error("HasWithdrawals should be false when the field is absent")
	}
	if len(got.Transactions) != 1 {
		t.Errorf("Transactions: got %d, want 1", len(got.Transactions))
	}
}

func TestFetchBlockInfoErrors(t *testing.T) {
	client := rpc.NewClient("http", "", 0)
	ctx := context.Background()

	t.Run("transport failure", func(t *testing.T) {
		if _, err := fetchBlockInfo(ctx, client, unusedPort, 1); err == nil {
			t.Error("expected an error for an unreachable node")
		}
	})

	t.Run("rpc error", func(t *testing.T) {
		node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) {
				return nil, &rpcErrorObject{Code: -32000, Message: "boom"}
			},
		})
		_, err := fetchBlockInfo(ctx, client, node.target(), 1)
		if err == nil || !strings.Contains(err.Error(), "RPC error") {
			t.Errorf("got %v, want an RPC error", err)
		}
	})

	t.Run("null result", func(t *testing.T) {
		node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) { return nil, nil },
		})
		_, err := fetchBlockInfo(ctx, client, node.target(), 1)
		if err == nil || !strings.Contains(err.Error(), "unexpected result type") {
			t.Errorf("got %v, want an 'unexpected result type' error", err)
		}
	})
}

// TestFetchBlockInfoUsesHexBlockNumber pins the RPC parameter encoding, since a
// decimal block number would silently address the wrong block.
func TestFetchBlockInfoUsesHexBlockNumber(t *testing.T) {
	var gotTag string
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockByNumber": func(params []any) (any, *rpcErrorObject) {
			gotTag, _ = params[0].(string)
			return map[string]any{"transactions": []any{}}, nil
		},
	})

	if _, err := fetchBlockInfo(context.Background(), rpc.NewClient("http", "", 0), node.target(), 255); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTag != "0xff" {
		t.Errorf("block tag: got %q, want 0xff", gotTag)
	}
}

func TestRunEmptyBlocksFindsRequestedCount(t *testing.T) {
	node := emptyBlocksNode(t, 40, 2, false)

	if err := runSubcommand("empty-blocks", "--url", node.url(), "--count", "5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.callsTo("eth_blockNumber") == 0 {
		t.Error("the latest block number should be queried first")
	}
	if node.callsTo("eth_getBlockByNumber") == 0 {
		t.Error("blocks should have been scanned")
	}
}

// TestRunEmptyBlocksHonoursIgnoreWithdrawals checks the flag that decides
// whether a block carrying withdrawals still counts as empty.
func TestRunEmptyBlocksHonoursIgnoreWithdrawals(t *testing.T) {
	// Every even block is transaction-free; every 4th also carries withdrawals.
	node := emptyBlocksNode(t, 40, 2, true)
	if err := runSubcommand("empty-blocks", "--url", node.url(), "--count", "3", "--ignore-withdrawals"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunEmptyBlocksComparesStateRoot(t *testing.T) {
	node := emptyBlocksNode(t, 20, 2, false)
	if err := runSubcommand("empty-blocks", "--url", node.url(), "--count", "2", "--compare-state-root"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunEmptyBlocksUnreachableNode(t *testing.T) {
	err := runSubcommand("empty-blocks", "--url", "http://"+unusedPort, "--count", "1")
	if err == nil {
		t.Fatal("expected an error when the node is unreachable")
	}
	if !strings.Contains(err.Error(), "get latest block") {
		t.Errorf("unexpected error: %v", err)
	}
}
