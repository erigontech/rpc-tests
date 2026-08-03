package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erigontech/rpc-tests/internal/config"
	internalrpc "github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

// fakeNode is a JSON-RPC server whose answers are sequenced per method: results[method][i]
// answers the i-th call to that method, the last entry answering every call after it.
type fakeNode struct {
	server  *httptest.Server
	mu      sync.Mutex
	results map[string][]any
	calls   map[string]int
	before  func(method string) // hook invoked before answering, to synchronize with other nodes
}

func newFakeNode(t *testing.T, results map[string][]any) *fakeNode {
	t.Helper()
	node := &fakeNode{results: results, calls: map[string]int{}}
	node.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			ID     any    `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("fake node: cannot decode request: %v", err)
			return
		}

		node.mu.Lock()
		idx := node.calls[req.Method]
		node.calls[req.Method]++
		seq := node.results[req.Method]
		before := node.before
		node.mu.Unlock()

		if before != nil {
			before(req.Method)
		}

		var result any
		if len(seq) > 0 {
			result = seq[min(idx, len(seq)-1)]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(node.server.Close)
	return node
}

func (n *fakeNode) callCount(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[method]
}

func (n *fakeNode) target() string {
	return strings.TrimPrefix(n.server.URL, "http://")
}

// blockHead is one answer to eth_getBlockByNumber("latest", false).
func blockHead(number, hash string) any {
	return map[string]any{"number": number, "hash": hash}
}

// movingHeads returns a head sequence where every read sees a new block, i.e. a head that
// never stands still between two reads.
func movingHeads(count int) []any {
	heads := make([]any, 0, count)
	for i := range count {
		heads = append(heads, blockHead(fmt.Sprintf("0x%x", 0x64+i), fmt.Sprintf("0xhash%d", i)))
	}
	return heads
}

// writeVerifyFixture builds a fixture and the config for verify-with-daemon mode, comparing
// the node under test against a reference node (as with -e).
func writeVerifyFixture(t *testing.T, underTest, reference *fakeNode, latest bool, commands []map[string]any) (*config.Config, *testdata.TestDescriptor) {
	t.Helper()
	cfg, descriptor := writeFixture(t, underTest.server, commands)
	cfg.VerifyWithDaemon = true
	cfg.DaemonAsReference = config.ExternalProvider
	cfg.ExternalProviderURL = reference.target()
	cfg.TestsOnLatestBlock = latest
	descriptor.Latest = latest
	return cfg, descriptor
}

// resultFiles lists the response/diff files left behind under the output directory.
func resultFiles(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(cfg.OutputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walking %s: %v", cfg.OutputDir, err)
	}
	return files
}

// The two nodes resolve the "latest" tag when the request arrives, so both requests must be
// in flight at the same time: the reference node must be called before the node under test
// has answered.
func TestRunTest_DispatchesBothNodesConcurrently(t *testing.T) {
	referenceCalled := make(chan struct{})
	var notConcurrent atomic.Bool

	underTest := newFakeNode(t, map[string][]any{"m1": {"r1"}})
	reference := newFakeNode(t, map[string][]any{"m1": {"r1"}})

	reference.before = func(method string) {
		if method == "m1" {
			close(referenceCalled)
		}
	}
	underTest.before = func(method string) {
		if method != "m1" {
			return
		}
		select {
		case <-referenceCalled:
		case <-time.After(10 * time.Second):
			notConcurrent.Store(true)
		}
	}

	cfg, descriptor := writeVerifyFixture(t, underTest, reference, false, []map[string]any{command("m1", "r1")})
	outcome := RunTest(context.Background(), descriptor, cfg, internalrpc.NewClient("http", "", 0))

	if notConcurrent.Load() {
		t.Fatal("the reference node was called only after the node under test answered: the two requests are not dispatched concurrently")
	}
	if outcome.Error != nil {
		t.Fatalf("RunTest error: %v", outcome.Error)
	}
	if !outcome.Success {
		t.Error("expected success when both nodes answer the same result")
	}
}

// Both nodes report an error: the one from the node under test is the one reported.
func TestRunTest_ReferenceErrorReported(t *testing.T) {
	underTest := newFakeNode(t, map[string][]any{"m1": {"r1"}})
	reference := newFakeNode(t, map[string][]any{"m1": {"r1"}})
	reference.server.Close() // the reference node is unreachable

	cfg, descriptor := writeVerifyFixture(t, underTest, reference, false, []map[string]any{command("m1", "r1")})
	outcome := RunTest(context.Background(), descriptor, cfg, internalrpc.NewClient("http", "", 0))

	if outcome.Error == nil {
		t.Fatal("expected an error when the reference node is unreachable")
	}
	if outcome.ErrorDetails == nil || outcome.ErrorDetails.Target != reference.target() {
		t.Errorf("error details should point at the reference node %s, got %+v", reference.target(), outcome.ErrorDetails)
	}
}

// Heads stable on both nodes: the mismatch is real, reported at the first attempt.
func TestRunTest_LatestStableHeadFailsWithoutRetry(t *testing.T) {
	stable := []any{blockHead("0x64", "0xaa")}
	underTest := newFakeNode(t, map[string][]any{"m1": {"r1"}, "eth_getBlockByNumber": stable})
	reference := newFakeNode(t, map[string][]any{"m1": {"r2"}, "eth_getBlockByNumber": stable})

	cfg, descriptor := writeVerifyFixture(t, underTest, reference, true, []map[string]any{command("m1", "r1")})
	outcome := RunTest(context.Background(), descriptor, cfg, internalrpc.NewClient("http", "", 0))

	if outcome.Success {
		t.Fatal("expected failure: the two nodes answered different results on the same head")
	}
	if got := underTest.callCount("m1"); got != 1 {
		t.Errorf("attempts: got %d, want 1 (a stable head means the mismatch is real)", got)
	}
	if len(outcome.Notes) != 0 {
		t.Errorf("no inconclusive note expected, got %v", outcome.Notes)
	}
	if outcome.ErrorDetails == nil || outcome.ErrorDetails.Heads == nil {
		t.Fatalf("the heads should be attached as evidence, got %+v", outcome.ErrorDetails)
	}
	heads := outcome.ErrorDetails.Heads
	if len(heads.Before) != 2 || len(heads.After) != 2 {
		t.Fatalf("expected the head of both nodes before and after, got %+v", heads)
	}
	if heads.Before[0].Number != 0x64 || heads.Before[0].Hash != "0xaa" {
		t.Errorf("head of the node under test: got %+v", heads.Before[0])
	}
	// The diff of a real failure is kept as evidence.
	if len(resultFiles(t, cfg)) == 0 {
		t.Error("expected the response/diff files of a real failure to be kept")
	}
}

// The head moves under every attempt: the result stays inconclusive and is reported as a
// failure only after the retries are exhausted, with the heads as evidence.
func TestRunTest_LatestHeadMovedRetriesThenFails(t *testing.T) {
	underTest := newFakeNode(t, map[string][]any{"m1": {"r1"}, "eth_getBlockByNumber": movingHeads(8)})
	reference := newFakeNode(t, map[string][]any{"m1": {"r2"}, "eth_getBlockByNumber": {blockHead("0x64", "0xaa")}})

	cfg, descriptor := writeVerifyFixture(t, underTest, reference, true, []map[string]any{command("m1", "r1")})
	cfg.LatestRetries = 3
	outcome := RunTest(context.Background(), descriptor, cfg, internalrpc.NewClient("http", "", 0))

	if outcome.Success {
		t.Fatal("expected failure after the retries are exhausted")
	}
	if got := underTest.callCount("m1"); got != 3 {
		t.Errorf("attempts: got %d, want 3 (--latest-retries)", got)
	}
	if len(outcome.Notes) != 3 {
		t.Fatalf("expected 2 inconclusive notes plus the final one, got %v", outcome.Notes)
	}
	for _, note := range outcome.Notes[:2] {
		if !strings.Contains(note, "inconclusive") {
			t.Errorf("note %q should report an inconclusive attempt", note)
		}
	}
	if !strings.Contains(outcome.Notes[2], "head moved on 3 consecutive attempts") {
		t.Errorf("last note %q should report the exhausted retries", outcome.Notes[2])
	}
	if outcome.ErrorDetails == nil || outcome.ErrorDetails.Heads == nil {
		t.Errorf("the heads should be attached as evidence, got %+v", outcome.ErrorDetails)
	}
}

// The head moved during the first attempt but the retry runs within one block: the test
// passes, and the artifacts of the discarded attempt are not left behind.
func TestRunTest_LatestInconclusiveThenPasses(t *testing.T) {
	underTest := newFakeNode(t, map[string][]any{
		"m1":                   {"stale", "r1"},
		"eth_getBlockByNumber": {blockHead("0x64", "0xaa"), blockHead("0x65", "0xbb"), blockHead("0x65", "0xbb")},
	})
	reference := newFakeNode(t, map[string][]any{
		"m1":                   {"r1"},
		"eth_getBlockByNumber": {blockHead("0x65", "0xbb")},
	})

	cfg, descriptor := writeVerifyFixture(t, underTest, reference, true, []map[string]any{command("m1", "r1")})
	outcome := RunTest(context.Background(), descriptor, cfg, internalrpc.NewClient("http", "", 0))

	if outcome.Error != nil || !outcome.Success {
		t.Fatalf("expected success on the retry, got success=%v error=%v", outcome.Success, outcome.Error)
	}
	if got := underTest.callCount("m1"); got != 2 {
		t.Errorf("attempts: got %d, want 2 (one inconclusive, one good)", got)
	}
	if len(outcome.Notes) != 1 || !strings.Contains(outcome.Notes[0], "inconclusive") {
		t.Errorf("expected one inconclusive note, got %v", outcome.Notes)
	}
	if files := resultFiles(t, cfg); len(files) != 0 {
		t.Errorf("the artifacts of the discarded attempt should have been removed, found %v", files)
	}
}

// With --latest-retries 0 the head is not read at all and the failure is reported as is.
func TestRunTest_LatestClassificationDisabled(t *testing.T) {
	underTest := newFakeNode(t, map[string][]any{"m1": {"r1"}, "eth_getBlockByNumber": movingHeads(4)})
	reference := newFakeNode(t, map[string][]any{"m1": {"r2"}, "eth_getBlockByNumber": movingHeads(4)})

	cfg, descriptor := writeVerifyFixture(t, underTest, reference, true, []map[string]any{command("m1", "r1")})
	cfg.LatestRetries = 0
	outcome := RunTest(context.Background(), descriptor, cfg, internalrpc.NewClient("http", "", 0))

	if outcome.Success {
		t.Fatal("expected failure")
	}
	if got := underTest.callCount("eth_getBlockByNumber"); got != 0 {
		t.Errorf("no head read expected with --latest-retries 0, got %d", got)
	}
	if got := underTest.callCount("m1"); got != 1 {
		t.Errorf("attempts: got %d, want 1", got)
	}
	if outcome.ErrorDetails != nil && outcome.ErrorDetails.Heads != nil {
		t.Error("no head evidence expected when the check is disabled")
	}
}

// A non-latest test is never re-run, even if the nodes are moving.
func TestRunTest_NonLatestNotRetried(t *testing.T) {
	underTest := newFakeNode(t, map[string][]any{"m1": {"r1"}})
	reference := newFakeNode(t, map[string][]any{"m1": {"r2"}})

	cfg, descriptor := writeVerifyFixture(t, underTest, reference, false, []map[string]any{command("m1", "r1")})
	outcome := RunTest(context.Background(), descriptor, cfg, internalrpc.NewClient("http", "", 0))

	if outcome.Success {
		t.Fatal("expected failure")
	}
	if got := underTest.callCount("eth_getBlockByNumber"); got != 0 {
		t.Errorf("no head read expected for a test that does not use the latest tag, got %d", got)
	}
	if got := underTest.callCount("m1"); got != 1 {
		t.Errorf("attempts: got %d, want 1", got)
	}
}

// When a head cannot be read the failure is reported as is, with the reason logged.
func TestRunTest_LatestHeadUnreadable(t *testing.T) {
	underTest := newFakeNode(t, map[string][]any{"m1": {"r1"}}) // no head answer: null result
	reference := newFakeNode(t, map[string][]any{"m1": {"r2"}, "eth_getBlockByNumber": {blockHead("0x64", "0xaa")}})

	cfg, descriptor := writeVerifyFixture(t, underTest, reference, true, []map[string]any{command("m1", "r1")})
	outcome := RunTest(context.Background(), descriptor, cfg, internalrpc.NewClient("http", "", 0))

	if outcome.Success {
		t.Fatal("expected failure")
	}
	if got := underTest.callCount("m1"); got != 1 {
		t.Errorf("attempts: got %d, want 1 (an unclassifiable failure is not retried)", got)
	}
	if len(outcome.Notes) != 1 || !strings.Contains(outcome.Notes[0], "head unreadable") {
		t.Errorf("expected a note about the unreadable head, got %v", outcome.Notes)
	}
}
