package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/erigontech/rpc-tests/internal/rpc"
)

// txBlock builds an eth_getBlockByNumber result with full transaction objects.
func txBlock(number int64, inputs ...string) map[string]any {
	txs := make([]any, 0, len(inputs))
	for i, input := range inputs {
		txs = append(txs, map[string]any{
			"hash":  fmt.Sprintf("0x%02x%02x", number, i),
			"input": input,
		})
	}
	return map[string]any{
		"number":       fmt.Sprintf("0x%x", number),
		"transactions": txs,
	}
}

// replayNodes starts the two servers a replay scan compares. Both answer
// eth_getBlockByNumber from blocks; the trace method returns traces[target]
// so the two sides can be made to agree or disagree.
type replayNodes struct {
	silk      *fakeNode
	rpcdaemon *fakeNode
}

func startReplayNodes(t *testing.T, blocks map[string]map[string]any, silkTrace, rpcdaemonTrace any) replayNodes {
	t.Helper()
	blockHandler := func(params []any) (any, *rpcErrorObject) {
		tag, _ := params[0].(string)
		block, ok := blocks[tag]
		if !ok {
			return nil, nil
		}
		return block, nil
	}
	return replayNodes{
		silk: newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockByNumber":    blockHandler,
			"trace_replayTransaction": func([]any) (any, *rpcErrorObject) { return silkTrace, nil },
			"debug_traceTransaction":  func([]any) (any, *rpcErrorObject) { return silkTrace, nil },
		}),
		rpcdaemon: newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockByNumber":    blockHandler,
			"trace_replayTransaction": func([]any) (any, *rpcErrorObject) { return rpcdaemonTrace, nil },
			"debug_traceTransaction":  func([]any) (any, *rpcErrorObject) { return rpcdaemonTrace, nil },
		}),
	}
}

func (n replayNodes) options(start, end int64) replayOptions {
	return replayOptions{
		silkTarget:      n.silk.target(),
		rpcdaemonTarget: n.rpcdaemon.target(),
		makeRequest:     makeTraceTransaction,
		startBlock:      start,
		endBlock:        end,
		continueOnDiff:  false,
		maxFailed:       0,
	}
}

func TestScanTransactionsAllMatching(t *testing.T) {
	replayOutputDir(t)
	nodes := startReplayNodes(t, map[string]map[string]any{
		"0x1": txBlock(1, "0xdeadbeef", "0xcafe"),
		"0x2": txBlock(2, "0xfeed"),
	}, map[string]any{"output": "0x01"}, map[string]any{"output": "0x01"})

	err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), nodes.options(1, 3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Three transactions, each replayed against both nodes.
	if got := nodes.silk.callsTo("trace_replayTransaction"); got != 3 {
		t.Errorf("silk replays: got %d, want 3", got)
	}
	if got := nodes.rpcdaemon.callsTo("trace_replayTransaction"); got != 3 {
		t.Errorf("rpcdaemon replays: got %d, want 3", got)
	}
	if names := outputFileNames(t, outputDir); len(names) != 0 {
		t.Errorf("matching traces should leave no artefacts, found %v", names)
	}
}

func TestScanTransactionsStopsAtFirstDiff(t *testing.T) {
	replayOutputDir(t)
	nodes := startReplayNodes(t, map[string]map[string]any{
		"0x1": txBlock(1, "0xaa", "0xbb"),
		"0x2": txBlock(2, "0xcc"),
	}, map[string]any{"output": "0x01"}, map[string]any{"output": "0x02"})

	err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), nodes.options(1, 3))
	if err == nil {
		t.Fatal("expected the scan to stop at the first diff")
	}
	if !strings.Contains(err.Error(), "diff found") {
		t.Errorf("unexpected error: %v", err)
	}
	if got := nodes.silk.callsTo("trace_replayTransaction"); got != 1 {
		t.Errorf("the scan should stop after the first transaction, got %d replays", got)
	}
}

func TestScanTransactionsContinueOnDiff(t *testing.T) {
	replayOutputDir(t)
	nodes := startReplayNodes(t, map[string]map[string]any{
		"0x1": txBlock(1, "0xaa", "0xbb"),
		"0x2": txBlock(2, "0xcc"),
	}, map[string]any{"output": "0x01"}, map[string]any{"output": "0x02"})

	opts := nodes.options(1, 3)
	opts.continueOnDiff = true

	if err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := nodes.silk.callsTo("trace_replayTransaction"); got != 3 {
		t.Errorf("every transaction should be replayed, got %d", got)
	}
	// Three diffs, three artefact triples.
	if names := outputFileNames(t, outputDir); len(names) != 9 {
		t.Errorf("expected 9 artefacts, found %d: %v", len(names), names)
	}
}

func TestScanTransactionsMaxFailed(t *testing.T) {
	replayOutputDir(t)
	nodes := startReplayNodes(t, map[string]map[string]any{
		"0x1": txBlock(1, "0xaa", "0xbb", "0xcc", "0xdd"),
	}, map[string]any{"output": "0x01"}, map[string]any{"output": "0x02"})

	opts := nodes.options(1, 2)
	opts.continueOnDiff = true // as runReplayTx does when -n is given
	opts.maxFailed = 2

	err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), opts)
	if err == nil {
		t.Fatal("expected the scan to stop once maxFailed is reached")
	}
	if !strings.Contains(err.Error(), "max failed requests reached: 2") {
		t.Errorf("unexpected error: %v", err)
	}
	if got := nodes.silk.callsTo("trace_replayTransaction"); got != 2 {
		t.Errorf("the scan should stop after 2 failures, got %d replays", got)
	}
}

// TestScanTransactionsStartTxAppliesToFirstBlockOnly pins the tx-index offset
// behaviour: --start 1:2 skips the first two transactions of block 1 and none
// of the transactions of the blocks after it.
func TestScanTransactionsStartTxAppliesToFirstBlockOnly(t *testing.T) {
	replayOutputDir(t)
	nodes := startReplayNodes(t, map[string]map[string]any{
		"0x1": txBlock(1, "0xaa", "0xbb", "0xcc"),
		"0x2": txBlock(2, "0xdd", "0xee"),
	}, map[string]any{"output": "0x01"}, map[string]any{"output": "0x01"})

	opts := nodes.options(1, 3)
	opts.startTx = 2

	if err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 transaction from block 1 (index 2) + 2 from block 2.
	if got := nodes.silk.callsTo("trace_replayTransaction"); got != 3 {
		t.Errorf("replays: got %d, want 3", got)
	}
}

func TestScanTransactionsSkipsBlocksWithoutWork(t *testing.T) {
	replayOutputDir(t)
	tests := []struct {
		name  string
		block map[string]any
	}{
		{"no transactions", txBlock(1)},
		{"transactions field of the wrong type", map[string]any{"number": "0x1", "transactions": "none"}},
		{"result is not an object", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := startReplayNodes(t, map[string]map[string]any{"0x1": tt.block},
				map[string]any{"output": "0x01"}, map[string]any{"output": "0x02"})

			err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), nodes.options(1, 2))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := nodes.silk.callsTo("trace_replayTransaction"); got != 0 {
				t.Errorf("nothing should be replayed, got %d", got)
			}
		})
	}
}

// TestScanTransactionsInputFilter pins the guard on the input field. The check
// is len(input) < 2, so it only drops transactions whose input is missing or a
// single character: "0x", a plain value transfer with no calldata, still passes
// and is replayed.
func TestScanTransactionsInputFilter(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		replays int
	}{
		{"missing input field", "", 0},
		{"one character", "0", 0},
		{"empty calldata is still replayed", "0x", 1},
		{"with calldata", "0xdeadbeef", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replayOutputDir(t)
			nodes := startReplayNodes(t, map[string]map[string]any{
				"0x1": txBlock(1, tt.input),
			}, map[string]any{"output": "0x01"}, map[string]any{"output": "0x01"})

			if err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), nodes.options(1, 2)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := nodes.silk.callsTo("trace_replayTransaction"); got != tt.replays {
				t.Errorf("replays: got %d, want %d", got, tt.replays)
			}
		})
	}
}

func TestScanTransactionsSkipsRPCErrors(t *testing.T) {
	replayOutputDir(t)
	silk := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) {
			return nil, &rpcErrorObject{Code: -32000, Message: "block not found"}
		},
	})
	rpcdaemon := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){})

	opts := replayOptions{
		silkTarget:      silk.target(),
		rpcdaemonTarget: rpcdaemon.target(),
		makeRequest:     makeTraceTransaction,
		startBlock:      1,
		endBlock:        4,
	}
	if err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), opts); err != nil {
		t.Fatalf("an RPC error should be skipped, not returned: %v", err)
	}
	if got := silk.callsTo("eth_getBlockByNumber"); got != 3 {
		t.Errorf("all three blocks should have been attempted, got %d", got)
	}
}

// TestScanTransactionsSurvivesUnreachableNode is the shape a run against a node
// that is down takes: every block is skipped and the scan completes.
func TestScanTransactionsSurvivesUnreachableNode(t *testing.T) {
	replayOutputDir(t)
	opts := replayOptions{
		silkTarget:      unusedPort,
		rpcdaemonTarget: unusedPort,
		makeRequest:     makeTraceTransaction,
		startBlock:      1,
		endBlock:        5,
	}
	if err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), opts); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestScanTransactionsStopsOnCancelledContext(t *testing.T) {
	replayOutputDir(t)
	nodes := startReplayNodes(t, map[string]map[string]any{
		"0x1": txBlock(1, "0xaa"),
	}, map[string]any{"output": "0x01"}, map[string]any{"output": "0x01"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := scanTransactions(ctx, rpc.NewClient("http", "", 0), nodes.options(1, 1000000)); err != nil {
		t.Fatalf("cancellation should be a graceful stop, got %v", err)
	}
	if got := nodes.silk.callsTo("eth_getBlockByNumber"); got != 0 {
		t.Errorf("no request should be made after cancellation, got %d", got)
	}
}

// TestScanTransactionsCancelMidScan checks that a signal arriving during a long
// scan ends it instead of running to endBlock.
func TestScanTransactionsCancelMidScan(t *testing.T) {
	replayOutputDir(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var seen int
	blockHandler := func(params []any) (any, *rpcErrorObject) {
		mu.Lock()
		seen++
		if seen == 3 {
			cancel()
		}
		mu.Unlock()
		return txBlock(1), nil
	}
	silk := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){"eth_getBlockByNumber": blockHandler})
	rpcdaemon := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){})

	opts := replayOptions{
		silkTarget:      silk.target(),
		rpcdaemonTarget: rpcdaemon.target(),
		makeRequest:     makeTraceTransaction,
		startBlock:      1,
		endBlock:        defaultEndBlock,
	}
	if err := scanTransactions(ctx, rpc.NewClient("http", "", 0), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen > 10 {
		t.Errorf("the scan should stop shortly after cancellation, made %d block requests", seen)
	}
}

func TestScanTransactionsDebugTraceMethod(t *testing.T) {
	replayOutputDir(t)
	nodes := startReplayNodes(t, map[string]map[string]any{
		"0x1": txBlock(1, "0xaa"),
	}, map[string]any{"gas": 21000.0}, map[string]any{"gas": 21000.0})

	opts := nodes.options(1, 2)
	opts.makeRequest = makeDebugTraceTransaction

	if err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := nodes.silk.callsTo("debug_traceTransaction"); got != 1 {
		t.Errorf("debug_traceTransaction calls: got %d, want 1", got)
	}
	if got := nodes.silk.callsTo("trace_replayTransaction"); got != 0 {
		t.Errorf("trace_replayTransaction should not be used, got %d calls", got)
	}
}

func TestScanTransactionsEmptyRange(t *testing.T) {
	replayOutputDir(t)
	nodes := startReplayNodes(t, nil, nil, nil)

	if err := scanTransactions(context.Background(), rpc.NewClient("http", "", 0), nodes.options(10, 10)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := nodes.silk.callsTo("eth_getBlockByNumber"); got != 0 {
		t.Errorf("an empty range should make no request, got %d", got)
	}
}

func TestDefaultEndBlock(t *testing.T) {
	if defaultEndBlock != 18000000 {
		t.Errorf("defaultEndBlock = %d, want 18000000 (the historical upper bound)", defaultEndBlock)
	}
}

// TestRunReplayTxCleansOutputDir checks the housekeeping runReplayTx does
// before scanning: ./output/ is recreated empty.
func TestRunReplayTxCleansOutputDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := outputDir + "stale.diffs"
	if err := os.WriteFile(stale, []byte("old"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// An empty range makes the scan a no-op, so the command returns promptly.
	done := make(chan error, 1)
	go func() { done <- runSubcommand("replay-tx", "--start", "17999999:0") }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runReplayTx did not finish")
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale artefact should have been removed, stat err = %v", err)
	}
}
