package vegetagen

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/erigontech/rpc-tests/internal/perf"
)

// fakeNode serves eth_blockNumber, eth_getBlockByNumber and eth_call.
// Every block has two txs; eth_call fails for txs sent from failingFrom.
type fakeNode struct {
	head           uint64
	failingFrom    string
	detailedErrors bool // append the block number to each error, as real nodes add addresses and amounts
	calls          atomic.Int64
}

const failingFrom = "0xbad0000000000000000000000000000000000bad"

func (n *fakeNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch req.Method {
	case "eth_blockNumber":
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, n.head)
	case "eth_getBlockByNumber":
		var num string
		_ = json.Unmarshal(req.Params[0], &num)
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"number":%q,"transactions":[`+
			`{"hash":"0xa","from":"0x0000000000000000000000000000000000000001","to":"0x0000000000000000000000000000000000000002","gas":"0x5208","gasPrice":"0x1","value":"0x0","input":"0x"},`+
			`{"hash":"0xb","from":%q,"to":"0x0000000000000000000000000000000000000002","gas":"0x5208","gasPrice":"0x1","value":"0x0","input":"0x"}]}}`,
			num, n.failingFrom)
	case "eth_call":
		n.calls.Add(1)
		var args struct {
			From string `json:"from"`
		}
		_ = json.Unmarshal(req.Params[0], &args)
		if args.From == n.failingFrom {
			msg := "insufficient funds for gas * price + value"
			if n.detailedErrors {
				var block string
				_ = json.Unmarshal(req.Params[1], &block)
				msg += ": address " + n.failingFrom + " at " + block
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":%q}}`, msg)
			return
		}
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x"}`)
	default:
		http.Error(w, "unknown method "+req.Method, http.StatusBadRequest)
	}
}

func startNode(t *testing.T) (*fakeNode, string) {
	t.Helper()
	node := &fakeNode{head: 100, failingFrom: failingFrom}
	srv := httptest.NewServer(node)
	t.Cleanup(srv.Close)
	return node, srv.URL
}

func baseConfig(t *testing.T, url string) Config {
	return Config{
		URL:       url,
		TargetURL: "http://localhost:8545",
		Blocks:    2,
		Tag:       TagLatest,
		Verify:    true,
		Keep:      KeepOK,
		Workers:   2,
		Out:       filepath.Join(t.TempDir(), "out.tar"),
	}
}

// readTargets returns the targets of the single vegeta file in the tar.
func readTargets(t *testing.T, tarPath, method string) []perf.VegetaTarget {
	t.Helper()
	f, err := os.Open(tarPath)
	if err != nil {
		t.Fatalf("open tar: %v", err)
	}
	defer f.Close()
	tr := tar.NewReader(f)
	// rpc_perf extracts without creating parent directories, so the directory entry must come first.
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar next: %v", err)
	}
	if hdr.Typeflag != tar.TypeDir || hdr.Name != "erigon_stress_test/" {
		t.Fatalf("first tar entry %q (type %c), want directory erigon_stress_test/", hdr.Name, hdr.Typeflag)
	}
	if hdr, err = tr.Next(); err != nil {
		t.Fatalf("tar next: %v", err)
	}
	if want := "erigon_stress_test/vegeta_erigon_" + method + ".txt"; hdr.Name != want {
		t.Fatalf("tar entry %q, want %q", hdr.Name, want)
	}
	var targets []perf.VegetaTarget
	sc := bufio.NewScanner(tr)
	sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for sc.Scan() {
		var vt perf.VegetaTarget
		if err := json.Unmarshal(sc.Bytes(), &vt); err != nil {
			t.Fatalf("decode target: %v", err)
		}
		targets = append(targets, vt)
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("tar has more than one entry")
	}
	return targets
}

func TestRunLatestKeepsOnlySuccessfulCalls(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Generated != 4 || sum.OK != 2 || sum.Written != 2 {
		t.Errorf("summary generated=%d ok=%d written=%d, want 4/2/2", sum.Generated, sum.OK, sum.Written)
	}
	if got := sum.Errors["insufficient funds for gas * price + value"]; got != 2 {
		t.Errorf("insufficient funds errors = %d, want 2", got)
	}

	targets := readTargets(t, cfg.Out, "eth_call")
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}
	for _, vt := range targets {
		if vt.Method != "POST" || vt.URL != "http://localhost:8545" {
			t.Errorf("target method=%s url=%s", vt.Method, vt.URL)
		}
		if vt.Header["Content-Type"][0] != "application/json" {
			t.Errorf("content type %v", vt.Header["Content-Type"])
		}
		body := string(vt.Body)
		if strings.Contains(body, failingFrom) {
			t.Errorf("failing call written: %s", body)
		}
		if !strings.HasSuffix(body, `,"latest"],"id":1}`) {
			t.Errorf("body not at latest: %s", body)
		}
	}
}

func TestRunWritesBlocksFromHeadDownwards(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Tag = TagParent
	cfg.Verify = false
	cfg.Keep = KeepAll

	if _, err := Run(context.Background(), cfg, EthCall{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	targets := readTargets(t, cfg.Out, "eth_call")
	wantParams := []string{"0x63", "0x63", "0x62", "0x62"}
	if len(targets) != len(wantParams) {
		t.Fatalf("got %d targets, want %d", len(targets), len(wantParams))
	}
	for i, vt := range targets {
		if want := fmt.Sprintf(`,%q],"id":1}`, wantParams[i]); !strings.HasSuffix(string(vt.Body), want) {
			t.Errorf("target %d body %s, want suffix %s", i, vt.Body, want)
		}
	}
}

func TestRunParentWithoutVerifyDoesNotCallNode(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Tag = TagParent
	cfg.Verify = false
	cfg.Keep = KeepAll

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if node.calls.Load() != 0 {
		t.Errorf("node received %d eth_call, want 0", node.calls.Load())
	}
	if sum.Written != 4 {
		t.Errorf("written %d, want 4", sum.Written)
	}
}

func TestRunParentWithVerifyKeepsOK(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Tag = TagParent
	cfg.Verify = true

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.OK != 2 || sum.Written != 2 {
		t.Errorf("ok=%d written=%d, want 2/2", sum.OK, sum.Written)
	}
}

func TestRunHeadPinsTheHeadNumber(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Tag = TagHead

	if _, err := Run(context.Background(), cfg, EthCall{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_call") {
		if !strings.HasSuffix(string(vt.Body), `,"0x64"],"id":1}`) {
			t.Errorf("body not pinned to head 0x64: %s", vt.Body)
		}
	}
}

func TestRunStopsAtCount(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks = 0
	cfg.Count = 3

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 3 {
		t.Errorf("written %d, want 3", sum.Written)
	}
	if got := len(readTargets(t, cfg.Out, "eth_call")); got != 3 {
		t.Errorf("tar has %d targets, want 3", got)
	}
}

func TestRunFailsBelowMinCount(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.MinCount = 3

	if _, err := Run(context.Background(), cfg, EthCall{}); err == nil {
		t.Fatal("Run: want error when fewer than min-count calls are written")
	}
	if _, err := os.Stat(cfg.Out); !os.IsNotExist(err) {
		t.Errorf("output written despite min-count failure")
	}
}

func TestRunGroupsErrorsByKind(t *testing.T) {
	node, url := startNode(t)
	node.detailedErrors = true
	cfg := baseConfig(t, url)
	cfg.Blocks = 3
	cfg.Tag = TagParent // a different block per request, so each error message differs
	cfg.Verify = true

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sum.Errors) != 1 || sum.Errors["insufficient funds for gas * price + value"] != 3 {
		t.Errorf("errors %v, want 3 grouped under one kind", sum.Errors)
	}
}

func TestRunDropsFeesUnlessKept(t *testing.T) {
	_, url := startNode(t)
	for _, keepFees := range []bool{false, true} {
		cfg := baseConfig(t, url)
		cfg.Tag = TagParent
		cfg.Verify = false
		cfg.Keep = KeepAll
		cfg.KeepFees = keepFees

		if _, err := Run(context.Background(), cfg, EthCall{}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, vt := range readTargets(t, cfg.Out, "eth_call") {
			if got := strings.Contains(string(vt.Body), `"gasPrice"`); got != keepFees {
				t.Errorf("keepFees=%v: body has gasPrice=%v: %s", keepFees, got, vt.Body)
			}
		}
	}
}

func TestRunLatestWithoutVerifyWritesAllCalls(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Verify = false

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if node.calls.Load() != 0 {
		t.Errorf("node received %d eth_call, want 0", node.calls.Load())
	}
	if sum.Written != 4 {
		t.Errorf("written %d, want 4", sum.Written)
	}
}

func TestRunLatestVerifyKeepAllWritesFailuresToo(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Keep = KeepAll

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.OK != 2 || sum.Written != 4 {
		t.Errorf("ok=%d written=%d, want 2/4", sum.OK, sum.Written)
	}
}

func TestRunCountScansBlocksUntilEnoughCallsAreWritten(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks = 0
	cfg.Count = 5 // one successful call per block

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 5 || sum.BlocksScanned != 5 {
		t.Errorf("written=%d blocks scanned=%d, want 5/5", sum.Written, sum.BlocksScanned)
	}
	if sum.FirstBlock != 100 || sum.LastBlock != 96 {
		t.Errorf("block range %d..%d, want 100..96", sum.FirstBlock, sum.LastBlock)
	}
}

func TestRunReportsBlocksScanned(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)

	sum, err := Run(context.Background(), cfg, EthCall{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.BlocksScanned != 2 || sum.FirstBlock != 100 || sum.LastBlock != 99 {
		t.Errorf("blocks scanned=%d range %d..%d, want 2 blocks 100..99", sum.BlocksScanned, sum.FirstBlock, sum.LastBlock)
	}
}

func TestRunNeedsEitherBlocksOrCount(t *testing.T) {
	_, url := startNode(t)
	for _, tc := range []struct {
		blocks uint64
		count  int
	}{{0, 0}, {2, 3}} {
		cfg := baseConfig(t, url)
		cfg.Blocks, cfg.Count = tc.blocks, tc.count
		if _, err := Run(context.Background(), cfg, EthCall{}); err == nil {
			t.Errorf("blocks=%d count=%d: want an error", tc.blocks, tc.count)
		}
	}
}
