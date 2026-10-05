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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/erigontech/rpc-tests/internal/perf"
)

// fakeNode serves eth_blockNumber, eth_getBlockByNumber and eth_call, and answers any other method with a result.
// Every block has two txs, plus one to extraTo when set; eth_call fails for txs sent from failingFrom.
type fakeNode struct {
	head           uint64
	failingFrom    string
	extraTo        string
	detailedErrors bool // append the block number to each error, as real nodes add addresses and amounts
	calls          atomic.Int64
	mu             sync.Mutex
	requests       map[string]int // by method
	blocksRead     map[string]bool
}

func (n *fakeNode) count(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.requests[method]
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
	n.mu.Lock()
	if n.requests == nil {
		n.requests, n.blocksRead = map[string]int{}, map[string]bool{}
	}
	n.requests[req.Method]++
	if req.Method == "eth_getBlockByNumber" {
		n.blocksRead[string(req.Params[0])] = true
	}
	n.mu.Unlock()
	switch req.Method {
	case "eth_blockNumber":
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, n.head)
	case "eth_getBlockByNumber":
		var num string
		_ = json.Unmarshal(req.Params[0], &num)
		extra := ""
		if n.extraTo != "" {
			extra = fmt.Sprintf(`,{"hash":"0xc","from":"0x0000000000000000000000000000000000000001","to":%q,"gas":"0x5208","gasPrice":"0x1","value":"0x0","input":"0x"}`, n.extraTo)
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"number":%q,"hash":"0xb10c%s","transactions":[`+
			`{"hash":"0xa","from":"0x0000000000000000000000000000000000000001","to":"0x0000000000000000000000000000000000000002","gas":"0x5208","gasPrice":"0x1","value":"0x0","input":"0x"},`+
			`{"hash":"0xb","from":%q,"to":"0x0000000000000000000000000000000000000002","gas":"0x5208","gasPrice":"0x1","value":"0x0","input":"0x"}%s]}}`,
			num, strings.TrimPrefix(num, "0x"), n.failingFrom, extra)
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
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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

	if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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

	if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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

	if _, err := Run(context.Background(), cfg, single(EthCall{})); err == nil {
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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

		if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
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
		if _, err := Run(context.Background(), cfg, single(EthCall{})); err == nil {
			t.Errorf("blocks=%d count=%d: want an error", tc.blocks, tc.count)
		}
	}
}

func single(gen Generator) []Spec { return []Spec{{Gen: gen, Weight: 1}} }

func mustGen(t *testing.T, method string) Generator {
	t.Helper()
	gen, err := NewGenerator(method, nil)
	if err != nil {
		t.Fatalf("NewGenerator(%s): %v", method, err)
	}
	return gen
}

func methodOf(t *testing.T, body []byte) string {
	t.Helper()
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return req.Method
}

func TestRunMixWritesExactSharesInterleaved(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count = 0, 8

	sum, err := Run(context.Background(), cfg, []Spec{{Gen: EthCall{}, Weight: 3}, {Gen: mustGen(t, "eth_getBlockByNumber"), Weight: 1}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	counts := map[string]int{}
	lastCall, firstBlock := -1, -1
	for i, vt := range readTargets(t, cfg.Out, "mixed") {
		m := methodOf(t, vt.Body)
		counts[m]++
		if m == "eth_call" {
			lastCall = i
		} else if firstBlock < 0 {
			firstBlock = i
		}
	}
	if counts["eth_call"] != 6 || counts["eth_getBlockByNumber"] != 2 {
		t.Errorf("counts %v, want 6 eth_call and 2 eth_getBlockByNumber", counts)
	}
	if firstBlock > lastCall {
		t.Errorf("methods are grouped, not interleaved: first eth_getBlockByNumber at %d, last eth_call at %d", firstBlock, lastCall)
	}
	if sum.Methods["eth_call"].Written != 6 || sum.Methods["eth_getBlockByNumber"].Written != 2 {
		t.Errorf("per-method summary %+v", sum.Methods)
	}
}

func TestRunMixIsReproducible(t *testing.T) {
	_, url := startNode(t)
	var bodies [2]string
	for i := range bodies {
		cfg := baseConfig(t, url)
		cfg.Blocks, cfg.Count = 0, 8
		if _, err := Run(context.Background(), cfg, []Spec{{Gen: EthCall{}, Weight: 3}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, vt := range readTargets(t, cfg.Out, "mixed") {
			bodies[i] += string(vt.Body)
		}
	}
	if bodies[0] != bodies[1] {
		t.Error("two runs on the same chain wrote different patterns")
	}
}

func TestRunMixNeedsCounts(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	if _, err := Run(context.Background(), cfg, []Spec{{Gen: EthCall{}, Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}}); err == nil {
		t.Error("blocks with several methods: want an error, the shares need a total count")
	}
}

func TestRunContractsKeepOnlyTheirTransactions(t *testing.T) {
	const contract = "0x0000000000000000000000000000000000000003"
	node, url := startNode(t)
	node.extraTo = contract
	cfg := baseConfig(t, url)
	cfg.Contracts = []string{contract}

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 2 {
		t.Errorf("written %d, want the 2 txs to the contract", sum.Written)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_call") {
		if !strings.Contains(string(vt.Body), `"to":"`+contract+`"`) {
			t.Errorf("call not to the contract: %s", vt.Body)
		}
	}
}

func TestRunContractsFilterLogsByAddress(t *testing.T) {
	const contract = "0x0000000000000000000000000000000000000003"
	node, url := startNode(t)
	node.extraTo = contract
	cfg := baseConfig(t, url)
	cfg.Contracts = []string{contract}

	if _, err := Run(context.Background(), cfg, single(mustGen(t, "eth_getLogs"))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_getLogs") {
		if !strings.Contains(string(vt.Body), `"address":["`+contract+`"]`) {
			t.Errorf("logs not filtered by the contract: %s", vt.Body)
		}
	}
}

func TestRunNameOverridesTheFileName(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Name = "custom"
	if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(readTargets(t, cfg.Out, "custom")); got != 2 {
		t.Errorf("got %d targets in vegeta_erigon_custom.txt, want 2", got)
	}
}

func TestRunMixDrawsFromAWindowOfRecentBlocks(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks = 0, 40, 3

	sum, err := Run(context.Background(), cfg, []Spec{{Gen: mustGen(t, "eth_getBlockReceipts"), Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(node.blocksRead) != 3 {
		t.Errorf("read %d blocks, want the 3 of the window", len(node.blocksRead))
	}
	if sum.BlocksScanned != 3 || sum.FirstBlock != 100 || sum.LastBlock != 98 {
		t.Errorf("blocks scanned=%d range %d..%d, want 3 blocks 100..98", sum.BlocksScanned, sum.FirstBlock, sum.LastBlock)
	}
	window := map[string]bool{`["0x64"]`: true, `["0x63"]`: true, `["0x62"]`: true}
	receipts := 0
	for _, vt := range readTargets(t, cfg.Out, "mixed") {
		if methodOf(t, vt.Body) != "eth_getBlockReceipts" {
			continue
		}
		receipts++
		var req struct {
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(vt.Body, &req)
		if !window[string(req.Params)] {
			t.Errorf("block outside the window: %s", req.Params)
		}
	}
	if receipts != 20 {
		t.Errorf("got %d eth_getBlockReceipts, want 20 drawn from 3 blocks", receipts)
	}
}

func TestRunMixVerifiesBlockMethodsWithOneSample(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks = 0, 40, 3

	if _, err := Run(context.Background(), cfg, []Spec{{Gen: mustGen(t, "eth_getBlockReceipts"), Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := node.count("eth_getBlockReceipts"); got != 1 {
		t.Errorf("eth_getBlockReceipts verified %d times, want 1 sample", got)
	}
	if got := node.count("eth_chainId"); got != 1 {
		t.Errorf("eth_chainId verified %d times, want 1 sample", got)
	}
}

func TestRunMixDrawsOnlyVerifiedCalls(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks = 0, 40, 3

	sum, err := Run(context.Background(), cfg, []Spec{{Gen: EthCall{}, Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := 0
	for _, vt := range readTargets(t, cfg.Out, "mixed") {
		if methodOf(t, vt.Body) == "eth_call" {
			calls++
			if strings.Contains(string(vt.Body), failingFrom) {
				t.Errorf("failing call written: %s", vt.Body)
			}
		}
	}
	if calls != 20 {
		t.Errorf("got %d eth_call, want 20 drawn from the verified ones", calls)
	}
	if sum.Methods["eth_call"].Errors["insufficient funds for gas * price + value"] == 0 {
		t.Errorf("the failing calls of the window were not counted: %+v", sum.Methods["eth_call"])
	}
}
