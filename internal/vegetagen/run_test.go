package vegetagen

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	detailedErrors bool   // append the block number to each error, as real nodes add addresses and amounts
	noLogsAt       string // eth_getLogs filtering on this address returns no logs
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
	case "eth_getLogs":
		var f struct {
			FromBlock string `json:"fromBlock"`
			Address   string `json:"address"`
		}
		_ = json.Unmarshal(req.Params[0], &f)
		if n.noLogsAt != "" && f.Address == n.noLogsAt {
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":[]}`)
			return
		}
		extra := ""
		if n.extraTo != "" {
			extra = fmt.Sprintf(`,{"address":%q,"topics":["0x7e"],"transactionHash":"0xc","logIndex":"0x2"}`, n.extraTo)
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":[`+
			`{"address":"0x0000000000000000000000000000000000000002","topics":["0x7e","0x01","0x02"],"transactionHash":"0xa","logIndex":"0x0"},`+
			`{"address":"0x0000000000000000000000000000000000000002","topics":["0x7e","0x01","0x02"],"transactionHash":"0xb","logIndex":"0x1"}%s]}`, extra)
	case "eth_getTransactionByHash":
		var hash string
		_ = json.Unmarshal(req.Params[0], &hash)
		if hash == missingTx {
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":null}`)
			return
		}
		from := "0x0000000000000000000000000000000000000001"
		if hash == failingTx {
			from = n.failingFrom
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"hash":%q,"blockNumber":"0x50","from":%q,`+
			`"to":"0x0000000000000000000000000000000000000002","gas":"0x5208","gasPrice":"0x1","value":"0x0","input":"0x12"}}`, hash, from)
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
	var r io.Reader = f
	if strings.HasSuffix(tarPath, ".tar.gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		r = gz
	}
	tr := tar.NewReader(r)
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

func TestRunWritesGzippedTar(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Out = filepath.Join(t.TempDir(), "out.tar.gz")

	if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(readTargets(t, cfg.Out, "eth_call")); got != 2 {
		t.Fatalf("got %d targets, want 2", got)
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

	sum, err := Run(context.Background(), cfg, single(mustGen(t, "eth_getLogs")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 2 {
		t.Errorf("written %d, want the log of the contract in each of the 2 blocks", sum.Written)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_getLogs") {
		if !strings.Contains(string(vt.Body), `"address":"`+contract+`"`) {
			t.Errorf("logs not of the contract: %s", vt.Body)
		}
	}
}

func TestRunLogsMatchALogOfTheBlockWithinTheHead(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks = 10

	sum, err := Run(context.Background(), cfg, single(mustGen(t, "eth_getLogs")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 10 {
		t.Errorf("written %d, want 1 per block: the 2 logs of a block are identical", sum.Written)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_getLogs") {
		var req struct {
			Params []struct {
				FromBlock string `json:"fromBlock"`
				ToBlock   string `json:"toBlock"`
				Address   string `json:"address"`
				Topics    []any  `json:"topics"`
			} `json:"params"`
		}
		if err := json.Unmarshal(vt.Body, &req); err != nil {
			t.Fatal(err)
		}
		f := req.Params[0]
		var from, to uint64
		fmt.Sscanf(f.FromBlock, "0x%x", &from)
		fmt.Sscanf(f.ToBlock, "0x%x", &to)
		if from > to || to > 100 || to-from >= DefaultLogsRange {
			t.Errorf("range %s..%s: want within %d blocks and not past the head", f.FromBlock, f.ToBlock, DefaultLogsRange)
		}
		if f.Address == "" && len(f.Topics) < 2 {
			t.Errorf("filter on topic0 alone or on nothing: %s", vt.Body)
		}
	}
}

func TestRunLogsDropEmptyResults(t *testing.T) {
	node, url := startNode(t)
	node.noLogsAt = "0x0000000000000000000000000000000000000002"
	cfg := baseConfig(t, url)
	cfg.Blocks = 20

	sum, err := Run(context.Background(), cfg, single(mustGen(t, "eth_getLogs")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Errors["empty result"] == 0 || sum.Written != sum.OK {
		t.Errorf("errors %v, written %d, ok %d: want the empty results counted and not written", sum.Errors, sum.Written, sum.OK)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_getLogs") {
		if strings.Contains(string(vt.Body), `"address"`) {
			t.Errorf("request with an empty result written: %s", vt.Body)
		}
	}
}

func TestRunMixVerifiesEveryLogsRequest(t *testing.T) {
	node, url := startNode(t)
	node.noLogsAt = "0x0000000000000000000000000000000000000002"
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks = 0, 40, 20

	if _, err := Run(context.Background(), cfg, []Spec{{Gen: mustGen(t, "eth_getLogs"), Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, vt := range readTargets(t, cfg.Out, "mixed") {
		if methodOf(t, vt.Body) == "eth_getLogs" && strings.Contains(string(vt.Body), `"address"`) {
			t.Errorf("request with an empty result written: %s", vt.Body)
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

func TestRunMixBlocksDrawsAMethodFromTheMostRecentBlocks(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks = 0, 40, 3

	specs := []Spec{{Gen: mustGen(t, "eth_getBlockReceipts"), Weight: 1, Blocks: 1}, {Gen: mustGen(t, "eth_getBlockByNumber"), Weight: 1}}
	if _, err := Run(context.Background(), cfg, specs); err != nil {
		t.Fatalf("Run: %v", err)
	}
	byNumber := map[string]bool{}
	for _, vt := range readTargets(t, cfg.Out, "mixed") {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(vt.Body, &req)
		switch req.Method {
		case "eth_getBlockReceipts":
			if string(req.Params[0]) != `"0x64"` {
				t.Errorf("eth_getBlockReceipts outside its 1 recent block: %s", req.Params[0])
			}
		case "eth_getBlockByNumber":
			byNumber[string(req.Params[0])] = true
		}
	}
	if len(byNumber) != 3 {
		t.Errorf("eth_getBlockByNumber drew from %d blocks, want the whole window of 3", len(byNumber))
	}
}

func TestRunWindowDrawsOneMethodFromTheWindow(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks, cfg.Window = 0, 40, 5, true

	sum, err := Run(context.Background(), cfg, single(mustGen(t, "eth_getLogs")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(node.blocksRead) != 5 || sum.BlocksScanned != 5 {
		t.Errorf("read %d blocks, scanned %d, want the 5 of the window", len(node.blocksRead), sum.BlocksScanned)
	}
	froms := map[string]bool{}
	for _, vt := range readTargets(t, cfg.Out, "eth_getLogs") {
		var req struct {
			Params []struct {
				ToBlock string `json:"toBlock"`
			} `json:"params"`
		}
		_ = json.Unmarshal(vt.Body, &req)
		froms[req.Params[0].ToBlock] = true
	}
	if sum.Written != 40 || len(froms) < 2 {
		t.Errorf("written %d with %d distinct toBlock, want 40 spread over the window", sum.Written, len(froms))
	}
	cfg.Blocks, cfg.Count = 2, 0
	if _, err := Run(context.Background(), cfg, single(mustGen(t, "eth_getLogs"))); err == nil {
		t.Error("blocks with a window: want an error, the window needs count")
	}
}

func TestRunLogsFilterSkipsLogsWithTooFewTopics(t *testing.T) {
	node, url := startNode(t)
	node.extraTo = "0x0000000000000000000000000000000000000003" // its log has topic0 only
	cfg := baseConfig(t, url)

	gen, err := NewGenerator("eth_getLogs", map[string]string{"filter": FilterTopics})
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	sum, err := Run(context.Background(), cfg, single(gen))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 2 {
		t.Errorf("written %d, want 1 per block: the log with topic0 only is skipped", sum.Written)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_getLogs") {
		if strings.Contains(string(vt.Body), `"address"`) || strings.Contains(string(vt.Body), node.extraTo) {
			t.Errorf("filter=topics wrote %s", vt.Body)
		}
	}
}

func TestRunMixLogsFilterSkipsLogsWithTooFewTopics(t *testing.T) {
	node, url := startNode(t)
	node.extraTo = "0x0000000000000000000000000000000000000003" // its log has topic0 only
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks, cfg.Window = 0, 20, 3, true

	gen, err := NewGenerator("eth_getLogs", map[string]string{"filter": FilterTopics})
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	sum, err := Run(context.Background(), cfg, single(gen))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 20 {
		t.Errorf("written %d, want 20", sum.Written)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_getLogs") {
		if strings.Contains(string(vt.Body), `"address"`) || strings.Contains(string(vt.Body), node.extraTo) {
			t.Errorf("filter=topics wrote %s", vt.Body)
		}
	}
}

func TestRunLogsFilterWithNoUsableLogFails(t *testing.T) {
	node, url := startNode(t)
	node.extraTo = "0x0000000000000000000000000000000000000003" // its log has topic0 only
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks, cfg.Window = 0, 5, 3, true
	cfg.Contracts = []string{node.extraTo}

	gen, err := NewGenerator("eth_getLogs", map[string]string{"filter": FilterTopics})
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	if _, err := Run(context.Background(), cfg, single(gen)); err == nil {
		t.Error("filter=topics on contracts whose logs have topic0 only: want an error, no request can be built")
	}
}

// The or lists of the logs of a contract hold only the values of the logs of the contracts, in the
// walk from the head as in the window.
func TestRunOrListsKeepToTheContracts(t *testing.T) {
	const contract = "0x0000000000000000000000000000000000000002"
	other := "0x0000000000000000000000000000000000000003"
	for _, window := range []bool{false, true} {
		node, url := startNode(t)
		node.extraTo = other
		cfg := baseConfig(t, url)
		cfg.Blocks, cfg.Count, cfg.MaxBlocks, cfg.Window = 0, 30, 30, window
		cfg.Contracts = []string{contract}

		gen, err := NewGenerator("eth_getLogs", map[string]string{"or": "16"})
		if err != nil {
			t.Fatalf("NewGenerator: %v", err)
		}
		if _, err := Run(context.Background(), cfg, single(gen)); err != nil {
			t.Fatalf("window=%v: Run: %v", window, err)
		}
		for _, vt := range readTargets(t, cfg.Out, "eth_getLogs") {
			if strings.Contains(string(vt.Body), other) {
				t.Errorf("window=%v: %s holds %s, outside the contracts", window, vt.Body, other)
			}
		}
	}
}

func blocksRead(node *fakeNode) map[string]bool {
	node.mu.Lock()
	defer node.mu.Unlock()
	return node.blocksRead
}

func TestRunRangeReadsEveryBlockOfTheRange(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Tag, cfg.Verify, cfg.Keep = 0, TagParent, false, KeepAll
	cfg.Range = &BlockRange{From: 10, To: 12}

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := map[string]bool{`"0xc"`: true, `"0xb"`: true, `"0xa"`: true}
	if got := blocksRead(node); len(got) != len(want) || !got[`"0xc"`] || !got[`"0xb"`] || !got[`"0xa"`] {
		t.Errorf("blocks read %v, want %v", got, want)
	}
	if sum.Written != 6 || sum.FirstBlock != 12 || sum.LastBlock != 10 {
		t.Errorf("written=%d range %d..%d, want 6 requests from 12..10", sum.Written, sum.FirstBlock, sum.LastBlock)
	}
}

func TestRunRangeEndsAtTheFirstOfToAndLen(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rng      BlockRange
		wantLast uint64
	}{
		{"to", BlockRange{From: 10, To: 12}, 12},
		{"len", BlockRange{From: 10, Len: 2}, 11},
		{"len before to", BlockRange{From: 10, To: 12, Len: 2}, 11},
		{"to before len", BlockRange{From: 10, To: 12, Len: 5}, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, url := startNode(t)
			cfg := baseConfig(t, url)
			cfg.Blocks, cfg.Tag = 0, TagParent
			cfg.Range = &tc.rng

			sum, err := Run(context.Background(), cfg, single(EthCall{}))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if sum.FirstBlock != tc.wantLast || sum.LastBlock != 10 {
				t.Errorf("range %d..%d, want %d..10", sum.FirstBlock, sum.LastBlock, tc.wantLast)
			}
		})
	}
}

func TestRunRangeStopsAtItsStartBeforeCount(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.Tag, cfg.Verify, cfg.Keep = 0, 100, TagParent, false, KeepAll
	cfg.Range = &BlockRange{From: 10, To: 11}

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(blocksRead(node)); got != 2 || sum.Written != 4 {
		t.Errorf("read %d blocks, written %d, want the 2 blocks of the range and their 4 calls", got, sum.Written)
	}
}

func TestRunRangeStopsAtCountBeforeItsStart(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.Tag, cfg.Verify = 0, 3, TagParent, false
	cfg.Range = &BlockRange{From: 10, To: 50}

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 3 || sum.FirstBlock != 50 || sum.LastBlock != 49 {
		t.Errorf("written=%d range %d..%d, want 3 requests from 50..49", sum.Written, sum.FirstBlock, sum.LastBlock)
	}
}

func TestRunHeadWithRangePinsTheEndOfTheRange(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Tag = 0, TagHead
	cfg.Range = &BlockRange{From: 10, To: 12}

	if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, vt := range readTargets(t, cfg.Out, "eth_call") {
		if !strings.HasSuffix(string(vt.Body), `,"0xc"],"id":1}`) {
			t.Errorf("body not pinned to the end of the range 0xc: %s", vt.Body)
		}
	}
}

func TestRunRangeRejectsInvalidSettings(t *testing.T) {
	_, url := startNode(t)
	for _, tc := range []struct {
		name   string
		rng    BlockRange
		tag    BlockTag
		blocks uint64
	}{
		{"no end", BlockRange{From: 10}, TagParent, 0},
		{"start after end", BlockRange{From: 12, To: 10}, TagParent, 0},
		{"end past the head", BlockRange{From: 10, To: 101, Len: 2}, TagParent, 0},
		{"latest", BlockRange{From: 10, To: 12}, TagLatest, 0},
		{"blocks", BlockRange{From: 10, To: 12}, TagParent, 2},
		{"sample larger than the range", BlockRange{From: 10, To: 12, Sample: 4}, TagParent, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(t, url)
			cfg.Blocks, cfg.Tag = tc.blocks, tc.tag
			cfg.Range = &tc.rng
			if _, err := Run(context.Background(), cfg, single(EthCall{})); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestRunMixDrawsFromTheRange(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.Tag = 0, 40, TagParent
	cfg.Range = &BlockRange{From: 10, To: 12}

	sum, err := Run(context.Background(), cfg, []Spec{{Gen: mustGen(t, "eth_getBlockReceipts"), Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := blocksRead(node); len(got) != 3 || !got[`"0xc"`] || !got[`"0xb"`] || !got[`"0xa"`] {
		t.Errorf("blocks read %v, want the 3 blocks of the range", got)
	}
	if sum.BlocksScanned != 3 || sum.FirstBlock != 12 || sum.LastBlock != 10 {
		t.Errorf("blocks scanned=%d range %d..%d, want 3 blocks 12..10", sum.BlocksScanned, sum.FirstBlock, sum.LastBlock)
	}
}

// blockNums returns the numbers of the blocks the node served.
func blockNums(t *testing.T, node *fakeNode) []uint64 {
	t.Helper()
	read := blocksRead(node)
	nums := make([]uint64, 0, len(read))
	for k := range read {
		n, err := strconv.ParseUint(strings.TrimPrefix(strings.Trim(k, `"`), "0x"), 16, 64)
		if err != nil {
			t.Fatalf("block number %s: %v", k, err)
		}
		nums = append(nums, n)
	}
	return nums
}

func TestRunSampleReadsDistinctBlocksOfTheRange(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Tag, cfg.Verify, cfg.Keep = 0, TagParent, false, KeepAll
	cfg.Range = &BlockRange{From: 10, To: 50, Sample: 5}

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	nums := blockNums(t, node)
	if len(nums) != 5 || sum.Written != 10 {
		t.Fatalf("read %d blocks, written %d, want 5 distinct blocks and their 10 calls", len(nums), sum.Written)
	}
	for _, n := range nums {
		if n < 10 || n > 50 {
			t.Errorf("block %d outside the range 10..50", n)
		}
	}
	if sum.BlocksScanned != 5 {
		t.Errorf("blocks scanned=%d, want 5", sum.BlocksScanned)
	}
}

func TestRunSampleIsReproducible(t *testing.T) {
	_, url := startNode(t)
	var bodies [2]string
	for i := range bodies {
		cfg := baseConfig(t, url)
		cfg.Blocks, cfg.Tag, cfg.Verify, cfg.Keep = 0, TagParent, false, KeepAll
		cfg.Range = &BlockRange{From: 10, To: 90, Sample: 5}
		if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, vt := range readTargets(t, cfg.Out, "eth_call") {
			bodies[i] += string(vt.Body)
		}
	}
	if bodies[0] != bodies[1] {
		t.Error("two runs on the same range wrote different patterns")
	}
}

func TestRunMixDrawsFromTheSample(t *testing.T) {
	node, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.Tag = 0, 40, TagParent
	cfg.Range = &BlockRange{From: 10, To: 50, Sample: 3}

	sum, err := Run(context.Background(), cfg, []Spec{{Gen: mustGen(t, "eth_getBlockReceipts"), Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	nums := blockNums(t, node)
	if len(nums) != 3 || sum.BlocksScanned != 3 {
		t.Fatalf("read %d blocks, scanned %d, want the 3 sampled blocks", len(nums), sum.BlocksScanned)
	}
	for _, n := range nums {
		if n < 10 || n > 50 {
			t.Errorf("block %d outside the range 10..50", n)
		}
	}
}

// requestsPerBlock counts the written requests by their block parameter, the last param of each body.
func requestsPerBlock(t *testing.T, targets []perf.VegetaTarget) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, vt := range targets {
		var req struct {
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(vt.Body, &req); err != nil || len(req.Params) == 0 {
			t.Fatalf("decode request %s: %v", vt.Body, err)
		}
		counts[string(req.Params[len(req.Params)-1])]++
	}
	return counts
}

func TestRunPerBlockTakesNTxsOfEachBlock(t *testing.T) {
	node, url := startNode(t)
	node.extraTo = "0x0000000000000000000000000000000000000003"
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Tag, cfg.Verify, cfg.Keep, cfg.PerBlock = 4, TagParent, false, KeepAll, 2

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	perBlock := requestsPerBlock(t, readTargets(t, cfg.Out, "eth_call"))
	if sum.Written != 8 || len(perBlock) != 4 {
		t.Fatalf("written %d over %d blocks, want 2 of the 3 txs of each of the 4 blocks", sum.Written, len(perBlock))
	}
	for block, n := range perBlock {
		if n != 2 {
			t.Errorf("block %s: %d requests, want 2", block, n)
		}
	}
}

func TestRunPerBlockAboveTheTxsOfABlockTakesThemAll(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Tag, cfg.Verify, cfg.Keep, cfg.PerBlock = TagParent, false, KeepAll, 5

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 4 {
		t.Errorf("written %d, want the 2 txs of each of the 2 blocks", sum.Written)
	}
}

func TestRunPerBlockIsReproducible(t *testing.T) {
	node, url := startNode(t)
	node.extraTo = "0x0000000000000000000000000000000000000003"
	var bodies [2]string
	for i := range bodies {
		cfg := baseConfig(t, url)
		cfg.Blocks, cfg.Tag, cfg.Verify, cfg.Keep, cfg.PerBlock = 10, TagParent, false, KeepAll, 1
		if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, vt := range readTargets(t, cfg.Out, "eth_call") {
			bodies[i] += string(vt.Body)
		}
	}
	if bodies[0] != bodies[1] {
		t.Error("two runs on the same blocks picked different txs")
	}
}

func TestRunPerBlockTakesNLogsOfEachBlock(t *testing.T) {
	node, url := startNode(t)
	node.extraTo = "0x0000000000000000000000000000000000000003"
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Verify, cfg.Keep, cfg.PerBlock = 3, false, KeepAll, 1

	sum, err := Run(context.Background(), cfg, single(mustGen(t, "eth_getLogs")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 3 {
		t.Errorf("written %d, want 1 of the 3 logs of each of the 3 blocks", sum.Written)
	}
}

func TestRunMixRejectsPerBlock(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.PerBlock = 0, 10, 1

	if _, err := Run(context.Background(), cfg, []Spec{{Gen: EthCall{}, Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}}); err == nil {
		t.Error("want an error: a mix already draws its requests at random from the window")
	}
}

// readJSONL returns the lines of a JSONL pattern, checking that each one is a JSON-RPC request.
func readJSONL(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, line := range lines {
		var req struct {
			Jsonrpc string `json:"jsonrpc"`
			Method  string `json:"method"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil || req.Jsonrpc != "2.0" || req.Method == "" {
			t.Fatalf("line %d is not a JSON-RPC request: %s", i, line)
		}
	}
	return lines
}

// assertJSONLMatchesTar runs specs twice, to a tar and to a .jsonl, and checks that the .jsonl holds the
// bodies of the tar targets, in the same order.
func assertJSONLMatchesTar(t *testing.T, cfg Config, specs []Spec, name string) {
	t.Helper()
	if _, err := Run(context.Background(), cfg, specs); err != nil {
		t.Fatalf("Run tar: %v", err)
	}
	targets := readTargets(t, cfg.Out, name)
	cfg.Out = filepath.Join(t.TempDir(), "out.jsonl")
	if _, err := Run(context.Background(), cfg, specs); err != nil {
		t.Fatalf("Run jsonl: %v", err)
	}
	lines := readJSONL(t, cfg.Out)
	if len(lines) != len(targets) {
		t.Fatalf("got %d lines, want the %d requests of the tar", len(lines), len(targets))
	}
	for i, vt := range targets {
		if lines[i] != string(vt.Body) {
			t.Errorf("line %d = %s, want %s", i, lines[i], vt.Body)
		}
	}
}

func TestRunWritesJSONLWithTheRequestsOfTheTar(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Tag = TagParent

	assertJSONLMatchesTar(t, cfg, single(EthCall{}), "eth_call")
}

func TestRunMixWritesJSONLWithTheRequestsOfTheTar(t *testing.T) {
	_, url := startNode(t)
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Count, cfg.MaxBlocks = 0, 20, 3

	assertJSONLMatchesTar(t, cfg, []Spec{{Gen: mustGen(t, "eth_getBlockReceipts"), Weight: 1}, {Gen: mustGen(t, "eth_chainId"), Weight: 1}}, "mixed")
}
