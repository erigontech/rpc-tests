package compare

import (
	"path/filepath"
	"testing"

	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

func BenchmarkCompareResponses_EqualMaps(b *testing.B) {
	a := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	c := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compareResponses(a, c)
	}
}

func BenchmarkCompareResponses_DifferentMaps(b *testing.B) {
	a := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	c := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x2"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compareResponses(a, c)
	}
}

func BenchmarkCompareResponses_LargeMap(b *testing.B) {
	makeMap := func(n int) map[string]interface{} {
		m := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1)}
		result := make(map[string]interface{}, n)
		for j := 0; j < n; j++ {
			result[string(rune('a'+j%26))+string(rune('0'+j/26))] = float64(j)
		}
		m["result"] = result
		return m
	}
	a := makeMap(100)
	c := makeMap(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compareResponses(a, c)
	}
}

func BenchmarkProcessResponse_ExactMatch(b *testing.B) {
	dir := b.TempDir()
	cfg := config.NewConfig()
	response := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	expected := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outcome := &testdata.TestOutcome{}
		cmd := &testdata.JsonRpcCommand{}
		ProcessResponse(response, nil, expected, cfg, cmd, dir, "", "", "", outcome)
	}
}

func BenchmarkProcessResponse_DiffMismatch_JsonDiffGo(b *testing.B) {
	dir := b.TempDir()
	cfg := config.NewConfig()
	cfg.DiffKind = config.JsonDiffGo

	daemonFile := filepath.Join(dir, "response.json")
	expRspFile := filepath.Join(dir, "expected.json")
	diffFile := filepath.Join(dir, "diff.json")

	response := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	expected := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x2"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outcome := &testdata.TestOutcome{}
		cmd := &testdata.JsonRpcCommand{}
		ProcessResponse(response, nil, expected, cfg, cmd, dir, daemonFile, expRspFile, diffFile, outcome)
	}
}

func BenchmarkDumpJSONs(b *testing.B) {
	dir := b.TempDir()
	daemonFile := filepath.Join(dir, "daemon.json")
	expRspFile := filepath.Join(dir, "expected.json")
	response := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"}
	expected := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "result": "0x2"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics := &testdata.TestMetrics{}
		dumpJSONs(true, daemonFile, expRspFile, dir, response, expected, metrics)
	}
}
