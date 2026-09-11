package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/erigontech/rpc-tests/internal/eth"
	"github.com/erigontech/rpc-tests/internal/rpc"
)

func TestHexToInt64(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int64
	}{
		{"zero", "0x0", 0},
		{"lowercase", "0xff", 255},
		{"uppercase", "0xFF", 255},
		{"mixed case", "0xAbCd", 0xabcd},
		{"no prefix", "10", 16},
		{"empty string", "", 0},
		{"just the prefix", "0x", 0},
		{"not a string", 42, 0},
		{"nil", nil, 0},
		{"non-hex characters are skipped", "0xzz", 0},
		{"large value", "0x1234567890", 0x1234567890},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hexToInt64(tt.input); got != tt.want {
				t.Errorf("hexToInt64(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestSleepCtxReturnsEarlyOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	sleepCtx(ctx, 5*time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("sleepCtx blocked for %v after the context was cancelled", elapsed)
	}
}

func TestSleepCtxWaitsForTheDuration(t *testing.T) {
	start := time.Now()
	sleepCtx(context.Background(), 20*time.Millisecond)
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Errorf("sleepCtx returned after only %v", elapsed)
	}
}

// receiptBlock builds a block header plus the matching receipts, with a
// receiptsRoot that is consistent by construction.
func receiptBlock(t *testing.T, number int64, parentHash string, numReceipts int) (block map[string]any, receipts []any) {
	t.Helper()
	typed := make([]map[string]any, 0, numReceipts)
	for i := range numReceipts {
		typed = append(typed, map[string]any{
			"status":            "0x1",
			"cumulativeGasUsed": fmt.Sprintf("0x%x", 21000*(i+1)),
			"logsBloom":         "0x" + strings.Repeat("00", 256),
			"logs":              []any{},
			"type":              "0x0",
		})
	}
	root, err := eth.ComputeReceiptsRoot(typed)
	if err != nil {
		t.Fatalf("compute receipts root: %v", err)
	}

	receipts = make([]any, 0, len(typed))
	for _, r := range typed {
		receipts = append(receipts, r)
	}
	block = map[string]any{
		"number":       fmt.Sprintf("0x%x", number),
		"hash":         fmt.Sprintf("0x%064x", number),
		"parentHash":   parentHash,
		"receiptsRoot": root,
	}
	return block, receipts
}

// receiptsNode serves a fixed set of blocks by number and their receipts by hash.
func receiptsNode(t *testing.T, blocks map[string]map[string]any, receipts map[string][]any) *fakeNode {
	t.Helper()
	return newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockByNumber": func(params []any) (any, *rpcErrorObject) {
			tag, _ := params[0].(string)
			block, ok := blocks[tag]
			if !ok {
				return nil, nil // JSON-RPC "null result" for an unknown block
			}
			return block, nil
		},
		"eth_getBlockReceipts": func(params []any) (any, *rpcErrorObject) {
			hash, _ := params[0].(string)
			return receipts[hash], nil
		},
	})
}

func TestGetFullBlock(t *testing.T) {
	block, receipts := receiptBlock(t, 10, "0x00", 2)
	node := receiptsNode(t,
		map[string]map[string]any{"latest": block, "0xa": block},
		map[string][]any{block["hash"].(string): receipts})

	client := rpc.NewClient("http", "", 0)
	ctx := context.Background()

	got, err := getFullBlock(ctx, client, node.target(), "latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["number"] != "0xa" {
		t.Errorf("number: got %v, want 0xa", got["number"])
	}

	got, err = getFullBlockByNumber(ctx, client, node.target(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["number"] != "0xa" {
		t.Errorf("getFullBlockByNumber should request the hex block number, got %v", got["number"])
	}
}

func TestGetFullBlockErrors(t *testing.T) {
	client := rpc.NewClient("http", "", 0)
	ctx := context.Background()

	t.Run("transport failure", func(t *testing.T) {
		if _, err := getFullBlock(ctx, client, unusedPort, "latest"); err == nil {
			t.Error("expected an error for an unreachable node")
		}
	})

	t.Run("rpc error", func(t *testing.T) {
		node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) {
				return nil, &rpcErrorObject{Code: -32000, Message: "boom"}
			},
		})
		_, err := getFullBlock(ctx, client, node.target(), "latest")
		if err == nil || !strings.Contains(err.Error(), "RPC error") {
			t.Errorf("got %v, want an RPC error", err)
		}
	})

	t.Run("null result", func(t *testing.T) {
		node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) { return nil, nil },
		})
		_, err := getFullBlock(ctx, client, node.target(), "0x1")
		if err == nil || !strings.Contains(err.Error(), "no result") {
			t.Errorf("got %v, want a 'no result' error", err)
		}
	})
}

func TestFetchBlockReceiptsRaw(t *testing.T) {
	block, receipts := receiptBlock(t, 7, "0x00", 3)
	hash := block["hash"].(string)
	node := receiptsNode(t, map[string]map[string]any{"0x7": block}, map[string][]any{hash: receipts})

	got, err := fetchBlockReceiptsRaw(context.Background(), rpc.NewClient("http", "", 0), node.target(), hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("receipts: got %d, want 3", len(got))
	}
}

// TestFetchBlockReceiptsRawSkipsNonObjects covers the defensive filter applied
// to entries that are not JSON objects.
func TestFetchBlockReceiptsRawSkipsNonObjects(t *testing.T) {
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockReceipts": func([]any) (any, *rpcErrorObject) {
			return []any{map[string]any{"status": "0x1"}, "garbage", 42}, nil
		},
	})
	got, err := fetchBlockReceiptsRaw(context.Background(), rpc.NewClient("http", "", 0), node.target(), "0xhash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("receipts: got %d, want 1", len(got))
	}
}

func TestFetchBlockReceiptsRawErrors(t *testing.T) {
	client := rpc.NewClient("http", "", 0)
	ctx := context.Background()

	t.Run("transport failure", func(t *testing.T) {
		if _, err := fetchBlockReceiptsRaw(ctx, client, unusedPort, "0xhash"); err == nil {
			t.Error("expected an error for an unreachable node")
		}
	})

	t.Run("rpc error", func(t *testing.T) {
		node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockReceipts": func([]any) (any, *rpcErrorObject) {
				return nil, &rpcErrorObject{Code: -32000, Message: "boom"}
			},
		})
		if _, err := fetchBlockReceiptsRaw(ctx, client, node.target(), "0xhash"); err == nil {
			t.Error("expected an RPC error")
		}
	})

	t.Run("unexpected result type", func(t *testing.T) {
		node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
			"eth_getBlockReceipts": func([]any) (any, *rpcErrorObject) { return "not-an-array", nil },
		})
		if _, err := fetchBlockReceiptsRaw(ctx, client, node.target(), "0xhash"); err == nil {
			t.Error("expected an error for a non-array result")
		}
	})
}

func TestVerifyBlockReceiptsMatchingRoot(t *testing.T) {
	block, receipts := receiptBlock(t, 42, "0x00", 4)
	node := receiptsNode(t, nil, map[string][]any{block["hash"].(string): receipts})

	err := verifyBlockReceipts(context.Background(), rpc.NewClient("http", "", 0), node.target(), block, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyBlockReceiptsMismatchedRoot(t *testing.T) {
	block, receipts := receiptBlock(t, 42, "0x00", 4)
	block["receiptsRoot"] = "0x" + strings.Repeat("11", 32)
	node := receiptsNode(t, nil, map[string][]any{block["hash"].(string): receipts})

	err := verifyBlockReceipts(context.Background(), rpc.NewClient("http", "", 0), node.target(), block, false)
	if err == nil {
		t.Fatal("expected an error for a receipts-root mismatch")
	}
	if !strings.Contains(err.Error(), "receipt root mismatch at block 42") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestVerifyBlockReceiptsFetchFailureIsTolerated documents that a failure to
// fetch receipts only logs and lets the scan continue.
func TestVerifyBlockReceiptsFetchFailureIsTolerated(t *testing.T) {
	block, _ := receiptBlock(t, 42, "0x00", 1)
	err := verifyBlockReceipts(context.Background(), rpc.NewClient("http", "", 0), unusedPort, block, false)
	if err != nil {
		t.Errorf("a fetch failure should not abort the scan, got %v", err)
	}
}

func TestVerifyReceiptsRootSkipsMissingBlock(t *testing.T) {
	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"eth_getBlockByNumber": func([]any) (any, *rpcErrorObject) { return nil, nil },
	})
	// A null block yields a "no result" error from getFullBlock, which
	// verifyReceiptsRoot reports as a failed lookup.
	err := verifyReceiptsRoot(context.Background(), rpc.NewClient("http", "", 0), node.target(), 5)
	if err == nil || !strings.Contains(err.Error(), "get block 5") {
		t.Errorf("got %v, want a 'get block 5' error", err)
	}
}

func TestScanReceiptsRange(t *testing.T) {
	blocks := map[string]map[string]any{}
	receipts := map[string][]any{}
	for n := int64(1); n <= 3; n++ {
		block, r := receiptBlock(t, n, "0x00", int(n))
		blocks[fmt.Sprintf("0x%x", n)] = block
		receipts[block["hash"].(string)] = r
	}
	node := receiptsNode(t, blocks, receipts)

	err := scanReceiptsRange(context.Background(), rpc.NewClient("http", "", 0), node.target(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := node.callsTo("eth_getBlockReceipts"); got != 3 {
		t.Errorf("expected 3 receipt fetches, got %d", got)
	}
}

func TestScanReceiptsRangeStopsAtMismatch(t *testing.T) {
	blocks := map[string]map[string]any{}
	receipts := map[string][]any{}
	for n := int64(1); n <= 3; n++ {
		block, r := receiptBlock(t, n, "0x00", 1)
		if n == 2 {
			block["receiptsRoot"] = "0x" + strings.Repeat("22", 32)
		}
		blocks[fmt.Sprintf("0x%x", n)] = block
		receipts[block["hash"].(string)] = r
	}
	node := receiptsNode(t, blocks, receipts)

	err := scanReceiptsRange(context.Background(), rpc.NewClient("http", "", 0), node.target(), 1, 3)
	if err == nil {
		t.Fatal("expected the scan to abort on a mismatch")
	}
	if got := node.callsTo("eth_getBlockByNumber"); got != 2 {
		t.Errorf("scan should stop at block 2, but made %d block lookups", got)
	}
}

func TestScanReceiptsRangeHonoursCancellation(t *testing.T) {
	node := receiptsNode(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := scanReceiptsRange(ctx, rpc.NewClient("http", "", 0), node.target(), 1, 100)
	if err != nil {
		t.Errorf("cancellation should be a graceful stop, got %v", err)
	}
	if got := node.callsTo("eth_getBlockByNumber"); got != 0 {
		t.Errorf("no request should be made after cancellation, got %d", got)
	}
}

func TestRunScanBlockReceiptsFlagValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "only start block",
			args: []string{"scan-block-receipts", "--start-block", "10"},
			want: "must specify --start-block AND --end-block",
		},
		{
			name: "only end block",
			args: []string{"scan-block-receipts", "--end-block", "10"},
			want: "must specify --start-block AND --end-block",
		},
		{
			name: "inverted range",
			args: []string{"scan-block-receipts", "--start-block", "10", "--end-block", "5"},
			want: "must be >= start block",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runSubcommand(tt.args...)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestRunScanBlockReceiptsRangeMode(t *testing.T) {
	block, receipts := receiptBlock(t, 1, "0x00", 2)
	node := receiptsNode(t,
		map[string]map[string]any{"0x1": block},
		map[string][]any{block["hash"].(string): receipts})

	err := runSubcommand("scan-block-receipts", "--url", node.url(), "--start-block", "1", "--end-block", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.callsTo("eth_getBlockReceipts") != 1 {
		t.Error("expected the range scan to verify block 1")
	}
}
