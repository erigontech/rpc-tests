package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/erigontech/rpc-tests/internal/eth"
	"github.com/erigontech/rpc-tests/internal/rpc"
)

// chain is a scriptable fake chain head used to drive the polling loops.
// Each call to "latest" advances to the next scripted block.
type chain struct {
	mu       sync.Mutex
	blocks   []map[string]any
	receipts map[string][]any
	cursor   int
	cancel   context.CancelFunc
	// stopAfter cancels the driving context once this many "latest" polls
	// have been served; 0 disables the safety stop.
	stopAfter int
	polls     int
}

func (c *chain) latest() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.polls++
	if c.stopAfter > 0 && c.polls >= c.stopAfter && c.cancel != nil {
		c.cancel()
	}
	block := c.blocks[c.cursor]
	if c.cursor < len(c.blocks)-1 {
		c.cursor++
	}
	return block
}

func (c *chain) byNumber(tag string) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range c.blocks {
		if b["number"] == tag {
			return b
		}
	}
	return nil
}

// linkedBlocks builds `count` consistently linked blocks starting at 1. When
// reorgAt is > 0 the block at that height gets a bogus parentHash.
func linkedBlocks(t *testing.T, count int, reorgAt int64) *chain {
	t.Helper()
	c := &chain{receipts: map[string][]any{}}
	for n := int64(1); n <= int64(count); n++ {
		parent := fmt.Sprintf("0x%064x", n-1)
		if n == reorgAt {
			parent = "0x" + strings.Repeat("ee", 32)
		}
		block, receipts := receiptBlock(t, n, parent, 1)
		c.blocks = append(c.blocks, block)
		c.receipts[block["hash"].(string)] = receipts
	}
	return c
}

func (c *chain) node(t *testing.T) *fakeNode {
	t.Helper()
	return newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockByNumber": func(params []any) (any, *rpcErrorObject) {
			tag, _ := params[0].(string)
			if tag == "latest" {
				return c.latest(), nil
			}
			return c.byNumber(tag), nil
		},
		"eth_getBlockReceipts": func(params []any) (any, *rpcErrorObject) {
			hash, _ := params[0].(string)
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.receipts[hash], nil
		},
	})
}

func TestScanReceiptsLatestStopsOnCancel(t *testing.T) {
	c := linkedBlocks(t, 4, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.cancel, c.stopAfter = cancel, 4
	node := c.node(t)

	err := scanReceiptsLatest(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.callsTo("eth_getBlockReceipts") == 0 {
		t.Error("expected the scan to verify at least one block")
	}
}

func TestScanReceiptsLatestStopsAtReorg(t *testing.T) {
	c := linkedBlocks(t, 5, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Safety net: if the reorg is missed the context still stops the loop.
	c.cancel, c.stopAfter = cancel, 20
	node := c.node(t)

	err := scanReceiptsLatest(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.Err() != nil {
		t.Error("the loop should have stopped at the reorg, before the safety cancel")
	}
}

func TestScanReceiptsLatestReportsRootMismatch(t *testing.T) {
	c := linkedBlocks(t, 3, 0)
	c.blocks[0]["receiptsRoot"] = "0x" + strings.Repeat("33", 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.cancel, c.stopAfter = cancel, 20
	node := c.node(t)

	err := scanReceiptsLatest(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, false)
	if err == nil || !strings.Contains(err.Error(), "receipt root mismatch") {
		t.Errorf("got %v, want a receipts-root mismatch error", err)
	}
}

// TestScanReceiptsLatestRetriesAfterError covers the branch where the node is
// briefly unreachable: the loop logs, backs off and keeps going.
func TestScanReceiptsLatestRetriesAfterError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := scanReceiptsLatest(ctx, rpc.NewClient("http", "", 0), unusedPort, time.Millisecond, false)
	if err != nil {
		t.Errorf("a persistent error should not abort the watcher, got %v", err)
	}
}

func TestScanReceiptsBeyondLatestStopsAtReorg(t *testing.T) {
	c := linkedBlocks(t, 6, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.cancel, c.stopAfter = cancel, 20
	node := c.node(t)

	err := scanReceiptsBeyondLatest(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.callsTo("eth_getBlockReceipts") == 0 {
		t.Error("expected the scan to verify blocks")
	}
}

func TestScanReceiptsBeyondLatestStopsOnCancel(t *testing.T) {
	c := linkedBlocks(t, 5, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.cancel, c.stopAfter = cancel, 3
	node := c.node(t)

	err := scanReceiptsBeyondLatest(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, false)
	if err != nil {
		t.Errorf("cancellation should be a graceful stop, got %v", err)
	}
}

func TestScanReceiptsBeyondLatestRetriesAfterError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := scanReceiptsBeyondLatest(ctx, rpc.NewClient("http", "", 0), unusedPort, time.Millisecond, false)
	if err != nil {
		t.Errorf("a persistent error should not abort the watcher, got %v", err)
	}
}

// logsChain serves a single latest block plus scripted eth_getLogs and
// eth_getBlockReceipts answers for the latest-block-logs watcher.
func logsChain(t *testing.T, receiptsRoot string, logs []any, receipts []any) *fakeNode {
	t.Helper()
	return newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) {
			return map[string]any{
				"number":       "0x64",
				"hash":         "0xdead",
				"receiptsRoot": receiptsRoot,
			}, nil
		},
		"eth_getLogs":          func([]any) (any, *rpcErrorObject) { return logs, nil },
		"eth_getBlockReceipts": func([]any) (any, *rpcErrorObject) { return receipts, nil },
	})
}

func TestWatchLatestBlockLogsWithLogs(t *testing.T) {
	node := logsChain(t, "0x"+strings.Repeat("aa", 32), []any{map[string]any{"address": "0x1"}}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := watchLatestBlockLogs(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.callsTo("eth_getBlockReceipts") != 0 {
		t.Error("receipts should not be fetched when eth_getLogs already returned logs")
	}
}

// TestWatchLatestBlockLogsEmptyReceiptsRoot covers the benign case: no logs and
// an empty receipts root means the block genuinely has nothing to report.
func TestWatchLatestBlockLogsEmptyReceiptsRoot(t *testing.T) {
	node := logsChain(t, emptyTrieRoot, []any{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := watchLatestBlockLogs(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.callsTo("eth_getBlockReceipts") != 0 {
		t.Error("an empty receipts root needs no receipts cross-check")
	}
}

// TestWatchLatestBlockLogsDetectsMissingLogs is the condition the tool exists
// to catch: eth_getLogs says zero, but the receipts contain logs.
func TestWatchLatestBlockLogsDetectsMissingLogs(t *testing.T) {
	receipts := []any{map[string]any{"logs": []any{map[string]any{"address": "0x1"}}}}
	node := logsChain(t, "0x"+strings.Repeat("bb", 32), []any{}, receipts)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- watchLatestBlockLogs(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, 0)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher should have stopped once it found unreported logs")
	}
	if node.callsTo("eth_getBlockReceipts") == 0 {
		t.Error("receipts should have been fetched to cross-check the missing logs")
	}
}

// TestWatchLatestBlockLogsSurvivesErrors covers the retry branches for a node
// that never answers.
func TestWatchLatestBlockLogsSurvivesErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := watchLatestBlockLogs(ctx, rpc.NewClient("http", "", 0), unusedPort, time.Millisecond, 0); err != nil {
		t.Errorf("a persistent error should not abort the watcher, got %v", err)
	}
}

// TestWatchLatestBlockLogsGetLogsError covers the branch where the block is
// readable but eth_getLogs fails.
func TestWatchLatestBlockLogsGetLogsError(t *testing.T) {
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) {
			return map[string]any{"number": "0x64", "hash": "0xdead", "receiptsRoot": emptyTrieRoot}, nil
		},
		"eth_getLogs": func([]any) (any, *rpcErrorObject) {
			return nil, &rpcErrorObject{Code: -32000, Message: "boom"}
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := watchLatestBlockLogs(ctx, rpc.NewClient("http", "", 0), node.target(), time.Millisecond, 0); err != nil {
		t.Errorf("a getLogs failure should not abort the watcher, got %v", err)
	}
}

// TestReceiptBlockFixtureIsSelfConsistent guards the shared test fixture: if
// the generated header root ever stopped matching its own receipts, every
// receipts-verification test above would pass vacuously.
func TestReceiptBlockFixtureIsSelfConsistent(t *testing.T) {
	block, receipts := receiptBlock(t, 1, "0x00", 3)
	typed := make([]map[string]any, 0, len(receipts))
	for _, r := range receipts {
		typed = append(typed, r.(map[string]any))
	}
	root, err := eth.ComputeReceiptsRoot(typed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != block["receiptsRoot"] {
		t.Errorf("fixture root %v does not match its receipts %s", block["receiptsRoot"], root)
	}
}
