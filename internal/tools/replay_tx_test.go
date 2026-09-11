package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/erigontech/rpc-tests/internal/rpc"
)

func TestMakeTraceTransaction(t *testing.T) {
	const hash = "0xabc"
	got := makeTraceTransaction(hash)
	for _, want := range []string{`"method":"trace_replayTransaction"`, `"` + hash + `"`, `"vmTrace"`} {
		if !strings.Contains(got, want) {
			t.Errorf("request %s is missing %s", got, want)
		}
	}
}

func TestMakeDebugTraceTransaction(t *testing.T) {
	const hash = "0xdef"
	got := makeDebugTraceTransaction(hash)
	for _, want := range []string{
		`"method":"debug_traceTransaction"`,
		`"` + hash + `"`,
		`"disableMemory":false`,
		`"disableStack":false`,
		`"disableStorage":false`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("request %s is missing %s", got, want)
		}
	}
}

// replayOutputDir moves the test into a scratch directory, because
// compareTxResponses writes its artefacts to the relative ./output/ path.
func replayOutputDir(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	return outputDir
}

func outputFileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestCompareTxResponsesIdentical(t *testing.T) {
	dir := replayOutputDir(t)
	handlers := map[string]func([]any) (any, *rpcErrorObject){
		"trace_replayTransaction": func([]any) (any, *rpcErrorObject) {
			return map[string]any{"output": "0x01"}, nil
		},
	}
	silk := newFakeNode(t, handlers)
	rpcdaemon := newFakeNode(t, handlers)

	client := rpc.NewClient("http", "", 0)
	got := compareTxResponses(context.Background(), client, makeTraceTransaction,
		silk.target(), rpcdaemon.target(), 1000, 0, "0xhash")

	if got != 0 {
		t.Errorf("got %d, want 0 (no diff)", got)
	}
	if names := outputFileNames(t, dir); len(names) != 0 {
		t.Errorf("matching responses should leave no artefacts, found %v", names)
	}
}

func TestCompareTxResponsesDiffering(t *testing.T) {
	dir := replayOutputDir(t)
	silk := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"trace_replayTransaction": func([]any) (any, *rpcErrorObject) {
			return map[string]any{"output": "0x01"}, nil
		},
	})
	rpcdaemon := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"trace_replayTransaction": func([]any) (any, *rpcErrorObject) {
			return map[string]any{"output": "0x02"}, nil
		},
	})

	client := rpc.NewClient("http", "", 0)
	got := compareTxResponses(context.Background(), client, makeTraceTransaction,
		silk.target(), rpcdaemon.target(), 1000, 3, "0xhash")

	if got != 1 {
		t.Fatalf("got %d, want 1 (diff)", got)
	}
	names := outputFileNames(t, dir)
	if len(names) != 3 {
		t.Fatalf("expected .silk/.rpcdaemon/.diffs artefacts, got %v", names)
	}
	for _, suffix := range []string{".silk", ".rpcdaemon", ".diffs"} {
		found := false
		for _, name := range names {
			if strings.HasSuffix(name, suffix) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing a %s artefact in %v", suffix, names)
		}
	}
	// The filename encodes block, tx index and hash.
	if !strings.Contains(strings.Join(names, " "), "bn_1000_txn_3_hash_0xhash") {
		t.Errorf("artefact names %v do not encode the block/tx/hash triple", names)
	}
}

func TestCompareTxResponsesRequestErrorIsNotADiff(t *testing.T) {
	dir := replayOutputDir(t)
	silk := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"trace_replayTransaction": func([]any) (any, *rpcErrorObject) { return "ok", nil },
	})

	client := rpc.NewClient("http", "", 0)
	got := compareTxResponses(context.Background(), client, makeTraceTransaction,
		silk.target(), unusedPort, 1, 0, "0xhash")

	if got != 0 {
		t.Errorf("an unreachable node should not be reported as a diff, got %d", got)
	}
	if names := outputFileNames(t, dir); len(names) != 0 {
		t.Errorf("no artefacts expected, found %v", names)
	}
}

func TestRunReplayTxRejectsBadStart(t *testing.T) {
	tests := []struct {
		name  string
		start string
		want  string
	}{
		{"missing separator", "1000", "block:tx"},
		{"non-numeric block", "abc:0", "invalid start block"},
		{"non-numeric tx", "1000:xyz", "invalid start tx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := runSubcommand("replay-tx", "--start", tt.start)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}
