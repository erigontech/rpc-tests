package compare

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

func TestCompareResponses_EqualMaps(t *testing.T) {
	a := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	b := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	if !compareResponses(a, b) {
		t.Error("identical maps should be equal")
	}
}

func TestCompareResponses_DifferentMaps(t *testing.T) {
	a := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	b := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x2"}
	if compareResponses(a, b) {
		t.Error("different maps should not be equal")
	}
}

func TestCompareResponses_DifferentLengths(t *testing.T) {
	a := map[string]any{"jsonrpc": "2.0", "id": float64(1)}
	b := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	if compareResponses(a, b) {
		t.Error("maps with different lengths should not be equal")
	}
}

func TestCompareResponses_EqualArrays(t *testing.T) {
	a := []map[string]any{{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}}
	b := []map[string]any{{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}}
	if !compareResponses(a, b) {
		t.Error("identical arrays should be equal")
	}
}

func TestProcessResponse_WithoutCompare(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	cfg.WithoutCompareResults = true

	outcome := &testdata.TestOutcome{}
	response := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	expected := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x2"}

	ProcessResponse(response, nil, expected, cfg, dir, "", "", "", outcome)

	if !outcome.Success {
		t.Error("WithoutCompareResults should always succeed")
	}
}

func TestProcessResponse_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()

	outcome := &testdata.TestOutcome{}
	response := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	expected := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}

	ProcessResponse(response, nil, expected, cfg, dir, "", "", "", outcome)

	if !outcome.Success {
		t.Errorf("exact match should succeed, error: %v", outcome.Error)
	}
	if outcome.Metrics.EqualCount != 1 {
		t.Errorf("EqualCount: got %d, want 1", outcome.Metrics.EqualCount)
	}
}

func TestProcessResponse_NullExpectedResult(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()

	outcome := &testdata.TestOutcome{}
	response := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0xabc"}
	expected := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": nil}

	ProcessResponse(response, nil, expected, cfg, dir, "", "", "", outcome)

	if !outcome.Success {
		t.Errorf("null expected result should be accepted, error: %v", outcome.Error)
	}
}

func TestProcessResponse_NullExpectedError(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()

	outcome := &testdata.TestOutcome{}
	response := map[string]any{"jsonrpc": "2.0", "id": float64(1), "error": map[string]any{"code": float64(-32000), "message": "some error"}}
	expected := map[string]any{"jsonrpc": "2.0", "id": float64(1), "error": nil}

	ProcessResponse(response, nil, expected, cfg, dir, "", "", "", outcome)

	if !outcome.Success {
		t.Errorf("null expected error should be accepted, error: %v", outcome.Error)
	}
}

func TestProcessResponse_EmptyExpected(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()

	outcome := &testdata.TestOutcome{}
	response := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	expected := map[string]any{"jsonrpc": "2.0", "id": float64(1)}

	ProcessResponse(response, nil, expected, cfg, dir, "", "", "", outcome)

	if !outcome.Success {
		t.Errorf("empty expected (just jsonrpc+id) should be accepted, error: %v", outcome.Error)
	}
}

func TestProcessResponse_DoNotCompareError(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	cfg.DoNotCompareError = true

	outcome := &testdata.TestOutcome{}
	response := map[string]any{"jsonrpc": "2.0", "id": float64(1), "error": map[string]any{"code": float64(-32000), "message": "err1"}}
	expected := map[string]any{"jsonrpc": "2.0", "id": float64(1), "error": map[string]any{"code": float64(-32001), "message": "err2"}}

	ProcessResponse(response, nil, expected, cfg, dir, "", "", "", outcome)

	if !outcome.Success {
		t.Errorf("DoNotCompareError should accept different errors, error: %v", outcome.Error)
	}
}

func TestProcessResponse_DiffMismatch_JsonDiffGo(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	cfg.DiffKind = config.JsonDiffGo

	daemonFile := filepath.Join(dir, "response.json")
	expRspFile := filepath.Join(dir, "expected.json")
	diffFile := filepath.Join(dir, "diff.json")

	outcome := &testdata.TestOutcome{}
	response := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	expected := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x2"}

	ProcessResponse(response, nil, expected, cfg, dir, daemonFile, expRspFile, diffFile, outcome)

	if outcome.Success {
		t.Error("mismatched responses should fail")
	}
	if outcome.Error == nil {
		t.Error("expected ErrDiffMismatch")
	}
}

func TestProcessResponse_DiffMismatch_SingleTest_HasColoredDiff(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	cfg.DiffKind = config.JsonDiffGo
	cfg.ReqTestNum = 1 // single test mode

	daemonFile := filepath.Join(dir, "response.json")
	expRspFile := filepath.Join(dir, "expected.json")
	diffFile := filepath.Join(dir, "diff.json")

	outcome := &testdata.TestOutcome{}
	response := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	expected := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x2"}

	ProcessResponse(response, nil, expected, cfg, dir, daemonFile, expRspFile, diffFile, outcome)

	if outcome.ColoredDiff == "" {
		t.Error("single test mode should produce colored diff on mismatch")
	}
}

func TestDumpJSONs_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	daemonFile := filepath.Join(dir, "daemon.json")
	expRspFile := filepath.Join(dir, "expected.json")
	metrics := &testdata.TestMetrics{}

	response := map[string]any{"result": "0x1"}
	expected := map[string]any{"result": "0x2"}

	err := dumpJSONs(true, daemonFile, expRspFile, dir, response, expected, metrics)
	if err != nil {
		t.Fatalf("dumpJSONs: %v", err)
	}

	if _, err := os.Stat(daemonFile); os.IsNotExist(err) {
		t.Error("daemon file should be written")
	}
	if _, err := os.Stat(expRspFile); os.IsNotExist(err) {
		t.Error("expected file should be written")
	}
	if metrics.MarshallingTime == 0 {
		t.Error("MarshallingTime should be > 0")
	}
}

func TestDumpJSONs_SkipsWhenFalse(t *testing.T) {
	dir := t.TempDir()
	daemonFile := filepath.Join(dir, "daemon.json")
	metrics := &testdata.TestMetrics{}

	err := dumpJSONs(false, daemonFile, "", dir, nil, nil, metrics)
	if err != nil {
		t.Fatalf("dumpJSONs: %v", err)
	}

	if _, err := os.Stat(daemonFile); !os.IsNotExist(err) {
		t.Error("daemon file should NOT be written when dump=false")
	}
}

func TestOutputFilePaths(t *testing.T) {
	apiFile, dirName, diff, daemon, exp := OutputFilePaths("/output", "eth_call/test_01.json")

	if !filepath.IsAbs(apiFile) || !contains(apiFile, "eth_call") {
		t.Errorf("apiFile: got %q", apiFile)
	}
	if !contains(dirName, "eth_call") {
		t.Errorf("dirName: got %q", dirName)
	}
	if !contains(diff, "-diff.json") {
		t.Errorf("diffFile: got %q", diff)
	}
	if !contains(daemon, "-response.json") {
		t.Errorf("daemonFile: got %q", daemon)
	}
	if !contains(exp, "-expResponse.json") {
		t.Errorf("expRspFile: got %q", exp)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && filepath.ToSlash(s) != "" && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
