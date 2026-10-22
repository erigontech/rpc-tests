package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func TestConfigProcessName(t *testing.T) {
	cfg := NewConfig()
	cfg.TestingClient = "rpcdaemon"

	if got := cfg.ProcessName(); got != "rpcdaemon" {
		t.Errorf("ProcessName should fall back to TestingClient, got %q", got)
	}

	cfg.ServerProcessName = "silkworm"
	if got := cfg.ProcessName(); got != "silkworm" {
		t.Errorf("ProcessName should prefer ServerProcessName, got %q", got)
	}
}

// quietStdout silences the progress lines the perf runner prints for the
// duration of one test. Nothing in this package asserts on stdout, and leaving
// it redirected for the whole binary would swallow go test's own coverage line.
func quietStdout(t *testing.T) {
	t.Helper()
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return
	}
	original := os.Stdout
	os.Stdout = devNull
	t.Cleanup(func() {
		os.Stdout = original
		devNull.Close()
	})
}

// patternArchive builds a tar.gz laid out like the real Vegeta pattern
// archives: erigon_stress_test/vegeta_erigon_<testType>.txt.
func patternArchive(t *testing.T, dir, testType string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, "pattern.tar.gz")
	writeTar(t, path, GzipCompression, map[string]string{
		"erigon_stress_test/vegeta_erigon_" + testType + ".txt": strings.Join(lines, "\n") + "\n",
	})
	return path
}

func targetLine(url string) string {
	target := VegetaTarget{
		Method: "POST",
		URL:    url,
		Body:   []byte(`{"jsonrpc":"2.0","method":"eth_getLogs","params":[],"id":1}`),
		Header: map[string][]string{"Content-Type": {"application/json"}},
	}
	data, _ := json.Marshal(target)
	return string(data)
}

// newTestPerf wires a PerfTest against a scratch run directory and a pattern
// archive pointing at serverURL.
func newTestPerf(t *testing.T, serverURL string) (*PerfTest, *Config, *RunDirs) {
	t.Helper()
	quietStdout(t)
	dir := t.TempDir()
	cfg := NewConfig()
	cfg.TestType = "eth_getLogs"
	cfg.VegetaPatternTarFile = patternArchive(t, dir, cfg.TestType, targetLine(serverURL))
	cfg.CheckServerAlive = false
	cfg.Repetitions = 1

	dirs := NewRunDirs()
	dirs.RunTestDir = filepath.Join(dir, "run")
	dirs.PatternDir = dirs.RunTestDir + "/erigon_stress_test"
	dirs.ReportFile = dirs.RunTestDir + "/vegeta_report.hrd"
	dirs.TarFileName = dirs.RunTestDir + "/vegeta_TAR_File"
	dirs.PatternBase = dirs.RunTestDir + "/erigon_stress_test/vegeta_erigon_"

	pt, err := NewPerfTest(cfg, NewTestReport(cfg, dirs), dirs)
	if err != nil {
		t.Fatalf("NewPerfTest: %v", err)
	}
	t.Cleanup(func() { _ = pt.Cleanup(true) })
	return pt, cfg, dirs
}

func TestNewPerfTestExtractsPattern(t *testing.T) {
	pt, cfg, dirs := newTestPerf(t, "http://localhost:8545")

	pattern := dirs.PatternBase + cfg.TestType + ".txt"
	if _, err := os.Stat(pattern); err != nil {
		t.Fatalf("pattern file was not extracted: %v", err)
	}
	if pt.Config != cfg {
		t.Error("PerfTest should keep the supplied config")
	}
}

func TestNewPerfTestRewritesClientAddress(t *testing.T) {
	quietStdout(t)
	dir := t.TempDir()
	cfg := NewConfig()
	cfg.TestType = "eth_getLogs"
	cfg.VegetaPatternTarFile = patternArchive(t, dir, cfg.TestType, targetLine("http://localhost:8545"))
	cfg.ClientAddress = "10.0.0.5"
	cfg.Tracing = true

	dirs := NewRunDirs()
	dirs.RunTestDir = filepath.Join(dir, "run")
	dirs.PatternDir = dirs.RunTestDir + "/erigon_stress_test"
	dirs.TarFileName = dirs.RunTestDir + "/vegeta_TAR_File"
	dirs.PatternBase = dirs.RunTestDir + "/erigon_stress_test/vegeta_erigon_"

	pt, err := NewPerfTest(cfg, NewTestReport(cfg, dirs), dirs)
	if err != nil {
		t.Fatalf("NewPerfTest: %v", err)
	}
	defer pt.Cleanup(true)

	data, err := os.ReadFile(dirs.PatternBase + cfg.TestType + ".txt")
	if err != nil {
		t.Fatalf("read pattern: %v", err)
	}
	if strings.Contains(string(data), "localhost") {
		t.Errorf("localhost should have been rewritten:\n%s", data)
	}
	if !strings.Contains(string(data), "10.0.0.5") {
		t.Errorf("client address is missing:\n%s", data)
	}
}

func TestNewPerfTestMissingPatternFile(t *testing.T) {
	cfg := NewConfig()
	cfg.VegetaPatternTarFile = filepath.Join(t.TempDir(), "absent.tar.gz")
	dirs := NewRunDirs()
	dirs.RunTestDir = filepath.Join(t.TempDir(), "run")
	dirs.TarFileName = dirs.RunTestDir + "/tar"
	dirs.PatternDir = dirs.RunTestDir + "/pattern"

	_, err := NewPerfTest(cfg, NewTestReport(cfg, dirs), dirs)
	if err == nil {
		t.Fatal("expected an error for a missing pattern file")
	}
	if !strings.Contains(err.Error(), "invalid pattern file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPerfTestCleanupRemovesArtefacts(t *testing.T) {
	pt, _, dirs := newTestPerf(t, "http://localhost:8545")

	if _, err := os.Stat(dirs.TarFileName); err != nil {
		t.Fatalf("the copied tar should exist before cleanup: %v", err)
	}
	if err := pt.Cleanup(false); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(dirs.TarFileName); !os.IsNotExist(err) {
		t.Errorf("tar file should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(dirs.PatternDir); !os.IsNotExist(err) {
		t.Errorf("pattern dir should be gone, stat err = %v", err)
	}
}

func TestLoadTargets(t *testing.T) {
	pt, _, _ := newTestPerf(t, "http://localhost:8545")

	path := filepath.Join(t.TempDir(), "targets.txt")
	content := strings.Join([]string{
		targetLine("http://localhost:8545"),
		"", // blank lines are skipped
		targetLine("http://localhost:8546"),
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	targets, err := pt.loadTargets(path)
	if err != nil {
		t.Fatalf("loadTargets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets: got %d, want 2", len(targets))
	}
	if targets[0].Method != "POST" {
		t.Errorf("method: got %q, want POST", targets[0].Method)
	}
	if targets[0].URL != "http://localhost:8545" {
		t.Errorf("url: got %q", targets[0].URL)
	}
	if got := targets[0].Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type header: got %q", got)
	}
	if len(targets[0].Body) == 0 {
		t.Error("body should be carried over")
	}
}

func TestLoadTargetsErrors(t *testing.T) {
	pt, _, _ := newTestPerf(t, "http://localhost:8545")
	dir := t.TempDir()

	t.Run("missing file", func(t *testing.T) {
		if _, err := pt.loadTargets(filepath.Join(dir, "absent")); err == nil {
			t.Error("expected an error for a missing pattern file")
		}
	})

	t.Run("malformed line", func(t *testing.T) {
		path := filepath.Join(dir, "bad.txt")
		if err := os.WriteFile(path, []byte("this is not json\n"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := pt.loadTargets(path)
		if err == nil || !strings.Contains(err.Error(), "failed to parse target") {
			t.Errorf("got %v, want a parse error", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(dir, "empty.txt")
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := pt.loadTargets(path)
		if err == nil || !strings.Contains(err.Error(), "no targets found") {
			t.Errorf("got %v, want a 'no targets' error", err)
		}
	})
}

// syntheticMetrics builds a closed vegeta.Metrics from the given results.
func syntheticMetrics(results ...*vegeta.Result) *vegeta.Metrics {
	var m vegeta.Metrics
	for _, r := range results {
		m.Add(r)
	}
	m.Close()
	return &m
}

func okResult(latency time.Duration) *vegeta.Result {
	return &vegeta.Result{Code: 200, Latency: latency, Timestamp: time.Now(), BytesIn: 10, BytesOut: 5}
}

func errResult(message string) *vegeta.Result {
	return &vegeta.Result{Code: 0, Error: message, Latency: time.Millisecond, Timestamp: time.Now()}
}

func TestProcessResultsSuccess(t *testing.T) {
	pt, cfg, _ := newTestPerf(t, "http://localhost:8545")
	cfg.MorePercentiles = true
	cfg.InstantReport = true

	metrics := syntheticMetrics(okResult(10*time.Millisecond), okResult(20*time.Millisecond))
	if err := pt.processResults(1, 0, "rpcdaemon", 100, 5, metrics, attackResults{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProcessResultsAllFailedIsAnError(t *testing.T) {
	pt, _, _ := newTestPerf(t, "http://localhost:8545")

	metrics := syntheticMetrics(errResult("connection refused"), errResult("connection refused"))
	err := pt.processResults(1, 0, "rpcdaemon", 100, 5, metrics, attackResults{})
	if err == nil {
		t.Fatal("expected an error when the success ratio is 0")
	}
	if !strings.Contains(err.Error(), "ratio is 0.00%") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProcessResultsHaltOnVegetaError(t *testing.T) {
	pt, cfg, _ := newTestPerf(t, "http://localhost:8545")
	cfg.HaltOnVegetaError = true

	metrics := syntheticMetrics(okResult(time.Millisecond), errResult("timeout"))
	err := pt.processResults(1, 0, "rpcdaemon", 100, 5, metrics, attackResults{})
	if err == nil {
		t.Fatal("expected an error when halt-on-error is set and errors occurred")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("the error message should name the vegeta error, got %v", err)
	}
}

// TestProcessResultsSummarisesManyErrors covers the "(+N more)" branch used
// when several distinct vegeta errors occur.
func TestProcessResultsSummarisesManyErrors(t *testing.T) {
	pt, _, _ := newTestPerf(t, "http://localhost:8545")

	metrics := syntheticMetrics(
		okResult(time.Millisecond),
		errResult("timeout"),
		errResult("connection refused"),
		errResult("EOF"),
	)
	if err := pt.processResults(1, 0, "rpcdaemon", 100, 5, metrics, attackResults{}); err != nil {
		t.Errorf("errors alone should not fail the test: %v", err)
	}
}

func TestPrintInstantReport(t *testing.T) {
	quietStdout(t)
	metrics := syntheticMetrics(
		okResult(10*time.Millisecond),
		okResult(2*time.Second),
		errResult("timeout"),
	)
	// Exercised for panics and format-verb mistakes rather than exact output.
	printInstantReport(metrics)
}

func TestExecuteRunsAnAttack(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	defer server.Close()

	pt, cfg, dirs := newTestPerf(t, server.URL)
	cfg.CreateTestReport = false

	format := ResultFormat{MaxRepetitionDigits: 1, MaxQpsDigits: 2, MaxDurationDigits: 1}
	err := pt.Execute(context.Background(), 1, 0, "rpcdaemon", 10, 1, format)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if requests == 0 {
		t.Error("the attack should have reached the server")
	}

	binDir := filepath.Join(dirs.RunTestDir, BinaryDir)
	entries, err := os.ReadDir(binDir)
	if err != nil {
		t.Fatalf("read binary dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one vegeta binary result file, got %v", entries)
	}
	if !strings.HasSuffix(entries[0].Name(), ".bin") {
		t.Errorf("unexpected result file name %q", entries[0].Name())
	}
}

func TestExecuteHonoursCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	defer server.Close()

	pt, _, _ := newTestPerf(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	format := ResultFormat{MaxRepetitionDigits: 1, MaxQpsDigits: 2, MaxDurationDigits: 1}
	err := pt.Execute(ctx, 1, 0, "rpcdaemon", 10, 30, format)
	if err == nil {
		t.Error("expected the attack to be cut short by the context")
	}
}

func TestExecuteReportsDeadServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	defer server.Close()

	pt, cfg, _ := newTestPerf(t, server.URL)
	cfg.CheckServerAlive = true
	cfg.TestingClient = "no_such_process_12345"

	format := ResultFormat{MaxRepetitionDigits: 1, MaxQpsDigits: 2, MaxDurationDigits: 1}
	err := pt.Execute(context.Background(), 1, 0, "rpcdaemon", 10, 1, format)
	if err == nil || !strings.Contains(err.Error(), "server died") {
		t.Errorf("got %v, want a dead-server error", err)
	}
}

func TestExecuteSequence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	defer server.Close()

	pt, cfg, dirs := newTestPerf(t, server.URL)
	cfg.Repetitions = 2
	cfg.WaitingTime = 0

	sequence := TestSequence{{QPS: 10, Duration: 1}}
	if err := pt.ExecuteSequence(context.Background(), sequence, "rpcdaemon"); err != nil {
		t.Fatalf("ExecuteSequence: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dirs.RunTestDir, BinaryDir))
	if err != nil {
		t.Fatalf("read binary dir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected one result file per repetition, got %d", len(entries))
	}
}

// TestExecuteSequenceZeroQPSJustWaits covers the pause entries a sequence can
// carry, written as 0:<seconds>.
func TestExecuteSequenceZeroQPSJustWaits(t *testing.T) {
	pt, cfg, dirs := newTestPerf(t, "http://localhost:8545")
	cfg.Repetitions = 1
	cfg.WaitingTime = 0

	start := time.Now()
	if err := pt.ExecuteSequence(context.Background(), TestSequence{{QPS: 0, Duration: 0}}, "rpcdaemon"); err != nil {
		t.Fatalf("ExecuteSequence: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("a zero-duration pause took %v", elapsed)
	}
	if _, err := os.Stat(filepath.Join(dirs.RunTestDir, BinaryDir)); !os.IsNotExist(err) {
		t.Error("a pause entry should not produce result files")
	}
}

func TestExecuteSequencePropagatesFailure(t *testing.T) {
	pt, cfg, _ := newTestPerf(t, "http://127.0.0.1:1")
	cfg.Repetitions = 1
	cfg.VegetaResponseTimeout = "1s"

	err := pt.ExecuteSequence(context.Background(), TestSequence{{QPS: 5, Duration: 1}}, "rpcdaemon")
	if err == nil {
		t.Fatal("expected an error when every request fails")
	}
	if !strings.Contains(err.Error(), "ratio is 0.00%") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRPCErrorMessage(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantMsg string
		wantErr bool
	}{
		{"result", `{"jsonrpc":"2.0","id":1,"result":"0x"}`, "", false},
		{"error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"insufficient funds for gas * price + value"}}`, "insufficient funds for gas * price + value", true},
		{"error before id", `{"jsonrpc":"2.0","error":{"code":-32005,"message":"server overloaded, retry later"},"id":null}`, "server overloaded, retry later", true},
		{"nested error inside a result", `{"jsonrpc":"2.0","id":1,"result":{"type":"CALL","error":"execution reverted"}}`, "", false},
		{"empty body", ``, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, isErr := rpcErrorMessage([]byte(tt.body))
			if isErr != tt.wantErr || msg != tt.wantMsg {
				t.Errorf("got (%q, %v), want (%q, %v)", msg, isErr, tt.wantMsg, tt.wantErr)
			}
		})
	}
}

func TestRunVegetaAttackTruncatesBodiesInTheBinaryFile(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":["` + strings.Repeat("a", 10000) + `"]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	pt, cfg, dirs := newTestPerf(t, server.URL)
	cfg.MaxBodyRsp = "100"
	targets := []vegeta.Target{{Method: "POST", URL: server.URL, Body: []byte(`{}`)}}
	out := filepath.Join(dirs.RunTestDir, "attack.bin")
	metrics, _, err := pt.runVegetaAttack(context.Background(), targets, 50, 200*time.Millisecond, out)
	if err != nil {
		t.Fatalf("runVegetaAttack: %v", err)
	}
	if metrics.Requests == 0 || metrics.Success != 1 {
		t.Fatalf("requests %d success %v: want every full body read as a success", metrics.Requests, metrics.Success)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := vegeta.NewDecoder(f)
	decoded := 0
	for {
		var r vegeta.Result
		if err := dec.Decode(&r); err != nil {
			break
		}
		decoded++
		if len(r.Body) != 100 {
			t.Fatalf("body of %d bytes in the binary file, want the first 100", len(r.Body))
		}
	}
	if uint64(decoded) != metrics.Requests {
		t.Errorf("decoded %d results, want %d", decoded, metrics.Requests)
	}
}

func TestPerfTestCleanupKeepsTheRunDirOnlyForAReport(t *testing.T) {
	for _, report := range []bool{false, true} {
		pt, cfg, dirs := newTestPerf(t, "http://localhost:8545")
		cfg.CreateTestReport = report
		bin := filepath.Join(dirs.RunTestDir, BinaryDir, "test.bin")
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := pt.Cleanup(false); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
		_, err := os.Stat(bin)
		if report && err != nil {
			t.Errorf("with a report the binary file should be kept: %v", err)
		}
		if !report && !os.IsNotExist(err) {
			t.Errorf("without a report the run dir should be gone, stat err = %v", err)
		}
	}
}

func TestRunVegetaAttackCountsRPCErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted"}}`))
	}))
	defer server.Close()

	pt, _, dirs := newTestPerf(t, server.URL)
	targets := []vegeta.Target{{Method: "POST", URL: server.URL, Body: []byte(`{}`)}}
	out := filepath.Join(dirs.RunTestDir, "attack.bin")
	metrics, results, err := pt.runVegetaAttack(context.Background(), targets, 50, 200*time.Millisecond, out)
	if err != nil {
		t.Fatalf("runVegetaAttack: %v", err)
	}
	if metrics.Requests == 0 {
		t.Fatal("the attack should have reached the server")
	}
	if metrics.Success != 1 {
		t.Errorf("vegeta success = %v: HTTP 200 counts as success", metrics.Success)
	}
	if got := results.rpcErrors["execution reverted"]; uint64(got) != metrics.Requests {
		t.Errorf("rpc errors = %d, want one per request (%d)", got, metrics.Requests)
	}
}

func TestFormatRPCErrors(t *testing.T) {
	tests := []struct {
		name      string
		rpcErrors map[string]int
		requests  uint64
		want      string
	}{
		{"none", nil, 10, ""},
		{"one kind", map[string]int{"execution reverted": 5}, 10, " rpc_errors=5 (50.00%) top=execution reverted (x5)"},
		{"several kinds", map[string]int{"execution reverted": 1, "insufficient funds": 3}, 8, " rpc_errors=4 (50.00%) top=insufficient funds (x3) (+1 more)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatRPCErrors(tt.rpcErrors, tt.requests); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRPCErrorKind(t *testing.T) {
	tests := []struct{ msg, want string }{
		{"insufficient funds for gas * price + value: address 0xD251 have 2122 want 5343", "insufficient funds for gas * price + value"},
		{"fee cap less than block base fee: address 0xbD23, feeCap: 128399523 baseFee: 214962990", "fee cap less than block base fee"},
		{"execution reverted: Ownable: caller is not the owner", "execution reverted"},
		{"execution reverted", "execution reverted"},
		{"out of gas", "out of gas"},
	}
	for _, tt := range tests {
		if got := RPCErrorKind(tt.msg); got != tt.want {
			t.Errorf("RPCErrorKind(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

func TestEmptyResult(t *testing.T) {
	tests := []struct {
		body string
		want bool
	}{
		{`{"jsonrpc":"2.0","id":1,"result":null}`, true},
		{`{"jsonrpc":"2.0","id":1,"result":[]}`, true},
		{`{"jsonrpc":"2.0","id":1,"result":[ ]}`, true},
		{`{"result":[],"jsonrpc":"2.0","id":1}`, true},
		{`{"jsonrpc":"2.0","id":1,"result":"0x"}`, false},
		{`{"jsonrpc":"2.0","id":1,"result":{}}`, false},
		{`{"jsonrpc":"2.0","id":1,"result":[{"address":"0x1"}]}`, false},
		{`{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted"}}`, false},
		{`not json`, false},
		{`{"jsonrpc":"2.0","id":1,"result":[` + strings.Repeat(`"0x00",`, 40) + `"0x00"]}`, false},
	}
	for _, tt := range tests {
		if got := emptyResult([]byte(tt.body)); got != tt.want {
			t.Errorf("emptyResult(%s) = %v, want %v", tt.body, got, tt.want)
		}
	}
}

func TestFormatEmpty(t *testing.T) {
	if got := formatEmpty(0, 10); got != "" {
		t.Errorf("got %q, want none", got)
	}
	if got, want := formatEmpty(3, 12), " empty=3 (25.00%)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunVegetaAttackCountsEmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	defer server.Close()

	pt, _, dirs := newTestPerf(t, server.URL)
	targets := []vegeta.Target{{Method: "POST", URL: server.URL + "#eth_getLogs", Body: []byte(`{}`)}}
	metrics, results, err := pt.runVegetaAttack(context.Background(), targets, 50, 200*time.Millisecond, filepath.Join(dirs.RunTestDir, "attack.bin"))
	if err != nil {
		t.Fatalf("runVegetaAttack: %v", err)
	}
	if metrics.Requests == 0 {
		t.Fatal("the attack should have reached the server")
	}
	if uint64(results.empty) != metrics.Requests {
		t.Errorf("empty results = %d, want one per request (%d)", results.empty, metrics.Requests)
	}
	if a := results.apis["eth_getLogs"]; a == nil || a.empty != a.requests {
		t.Errorf("eth_getLogs = %+v, want all its results empty", a)
	}
}

func TestRunVegetaAttackGroupsRPCErrorsByKind(t *testing.T) {
	var n int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		i := n
		mu.Unlock()
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"fee cap less than block base fee: address 0x%x, feeCap: %d"}}`, i, i)
	}))
	defer server.Close()

	pt, _, dirs := newTestPerf(t, server.URL)
	targets := []vegeta.Target{{Method: "POST", URL: server.URL, Body: []byte(`{}`)}}
	metrics, results, err := pt.runVegetaAttack(context.Background(), targets, 50, 200*time.Millisecond, filepath.Join(dirs.RunTestDir, "attack.bin"))
	if err != nil {
		t.Fatalf("runVegetaAttack: %v", err)
	}
	if len(results.rpcErrors) != 1 || uint64(results.rpcErrors["fee cap less than block base fee"]) != metrics.Requests {
		t.Errorf("rpc errors %v, want all %d grouped under one kind", results.rpcErrors, metrics.Requests)
	}
}

func TestLoadTargetsTagsTheMethodWhenVerbose(t *testing.T) {
	pt, cfg, _ := newTestPerf(t, "http://localhost:8545")
	cfg.Verbose = true

	path := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(path, []byte(targetLine("http://localhost:8545")+"\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	targets, err := pt.loadTargets(path)
	if err != nil {
		t.Fatalf("loadTargets: %v", err)
	}
	if got := targets[0].URL; got != "http://localhost:8545#eth_getLogs" {
		t.Errorf("url %q, want the method as fragment", got)
	}
}

// apiServer answers eth_call with an error and any other method with a result.
func apiServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Fragment != "" || strings.Contains(r.RequestURI, "#") {
			t.Errorf("the fragment reached the server: %q", r.RequestURI)
		}
		if strings.Contains(string(body), `"eth_call"`) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted: no"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunVegetaAttackCountsResultsByAPI(t *testing.T) {
	srv := apiServer(t)
	pt, cfg, dirs := newTestPerf(t, srv.URL)
	cfg.Verbose = true
	targets := []vegeta.Target{
		{Method: "POST", URL: srv.URL + "#eth_call", Body: []byte(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`)},
		{Method: "POST", URL: srv.URL + "#eth_getLogs", Body: []byte(`{"jsonrpc":"2.0","method":"eth_getLogs","params":[],"id":1}`)},
	}
	out := filepath.Join(dirs.RunTestDir, "attack.bin")
	metrics, results, err := pt.runVegetaAttack(context.Background(), targets, 50, 200*time.Millisecond, out)
	if err != nil {
		t.Fatalf("runVegetaAttack: %v", err)
	}
	calls, logs := results.apis["eth_call"], results.apis["eth_getLogs"]
	if calls == nil || logs == nil {
		t.Fatalf("apis %v, want eth_call and eth_getLogs", results.apis)
	}
	if uint64(calls.requests+logs.requests) != metrics.Requests {
		t.Errorf("api requests %d+%d, want %d", calls.requests, logs.requests, metrics.Requests)
	}
	if calls.errors["execution reverted"] != calls.requests || len(logs.errors) != 0 {
		t.Errorf("eth_call errors %v of %d, eth_getLogs errors %v", calls.errors, calls.requests, logs.errors)
	}
	if calls.latencies.Max <= 0 || logs.latencies.Max <= 0 || max(calls.latencies.Max, logs.latencies.Max) != metrics.Latencies.Max {
		t.Errorf("api max latencies %v and %v, want the attack max %v", calls.latencies.Max, logs.latencies.Max, metrics.Latencies.Max)
	}

	// The .bin keeps the URL the node received, without the fragment.
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open bin: %v", err)
	}
	defer f.Close()
	dec := vegeta.NewDecoder(f)
	for {
		var r vegeta.Result
		if err := dec.Decode(&r); err != nil {
			break
		}
		if r.URL != srv.URL {
			t.Fatalf("bin url %q, want %q", r.URL, srv.URL)
		}
	}
}

// newAPIResult returns the result of an API with the given latencies and errors.
func newAPIResult(errors map[string]int, latencies ...time.Duration) *apiResult {
	a := &apiResult{requests: len(latencies), errors: errors}
	for _, l := range latencies {
		a.latencies.Add(l)
	}
	return a
}

func TestFormatAPIResults(t *testing.T) {
	ms := time.Millisecond
	apis := map[string]*apiResult{
		"eth_call":    newAPIResult(map[string]int{"execution reverted": 2}, 2*ms, 2*ms, 2*ms, 2*ms, 2*ms, 2*ms),
		"eth_getLogs": newAPIResult(map[string]int{}, 500*time.Microsecond, 500*time.Microsecond),
	}
	apis["eth_getLogs"].empty = 1
	got := formatAPIResults(apis, false)
	want := []string{
		"      api                               reqs  share   result    empty    error       p50       p99       max",
		"      eth_call                             6  75.0%    66.7%     0.0%    33.3%    2.00ms    2.00ms    2.00ms  top=execution reverted (x2)",
		"      eth_getLogs                          2  25.0%   100.0%    50.0%     0.0%     500µs     500µs     500µs",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFormatAPIResultsMorePercentiles(t *testing.T) {
	apis := map[string]*apiResult{
		"eth_blockNumber": newAPIResult(map[string]int{}, 3*time.Millisecond, 3*time.Millisecond),
	}
	got := formatAPIResults(apis, true)
	want := []string{
		"      api                               reqs  share   result    empty    error       p50       p90       p95       p99       max",
		"      eth_blockNumber                      2 100.0%   100.0%     0.0%     0.0%    3.00ms    3.00ms    3.00ms    3.00ms    3.00ms",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestFormatAPIResultsLatencies checks that the percentiles of an API come from its own
// latencies, in order and up to its max.
func TestFormatAPIResultsLatencies(t *testing.T) {
	var latencies []time.Duration
	for i := 1; i <= 100; i++ {
		latencies = append(latencies, time.Duration(i)*time.Millisecond)
	}
	a := newAPIResult(map[string]int{}, latencies...)
	p50, p99 := a.latencies.Quantile(0.50), a.latencies.Quantile(0.99)
	if p50 < 45*time.Millisecond || p50 > 55*time.Millisecond {
		t.Errorf("p50 %v, want about 50ms", p50)
	}
	if p50 > p99 || p99 > a.latencies.Max {
		t.Errorf("p50 %v, p99 %v, max %v, want them in order", p50, p99, a.latencies.Max)
	}
	if a.latencies.Max != 100*time.Millisecond {
		t.Errorf("max %v, want 100ms", a.latencies.Max)
	}
	row := formatAPIResults(map[string]*apiResult{"eth_getLogs": a}, false)[1]
	if !strings.HasSuffix(row, " "+FormatDuration(p50)+" "+fmt.Sprintf("%9s", FormatDuration(p99))+"  100.00ms") {
		t.Errorf("row %q, want p50 %v, p99 %v and max 100.00ms", row, p50, p99)
	}
}

// newJSONLPerf wires a PerfTest on a JSONL pattern holding lines, replayed against clientAddress.
func newJSONLPerf(t *testing.T, clientAddress string, lines ...string) (*PerfTest, *Config, *RunDirs) {
	t.Helper()
	quietStdout(t)
	dir := t.TempDir()
	cfg := NewConfig()
	cfg.TestType = "eth_call"
	cfg.VegetaPatternTarFile = filepath.Join(dir, "eth_call.jsonl")
	if err := os.WriteFile(cfg.VegetaPatternTarFile, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	cfg.ClientAddress = clientAddress
	cfg.CheckServerAlive = false
	cfg.Repetitions = 1

	dirs := NewRunDirs()
	dirs.RunTestDir = filepath.Join(dir, "run")
	dirs.PatternDir = dirs.RunTestDir + "/erigon_stress_test"
	dirs.ReportFile = dirs.RunTestDir + "/vegeta_report.hrd"
	dirs.TarFileName = dirs.RunTestDir + "/vegeta_TAR_File"
	dirs.PatternBase = dirs.RunTestDir + "/erigon_stress_test/vegeta_erigon_"

	pt, err := NewPerfTest(cfg, NewTestReport(cfg, dirs), dirs)
	if err != nil {
		t.Fatalf("NewPerfTest: %v", err)
	}
	t.Cleanup(func() { _ = pt.Cleanup(true) })
	return pt, cfg, dirs
}

const jsonlCall = `{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x02"},"0x10"],"id":1}`

func TestLoadTargetsJSONLPostsEachLineToTheClientAddress(t *testing.T) {
	pt, cfg, _ := newJSONLPerf(t, "10.0.0.5", jsonlCall, "", `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`)

	targets, err := pt.loadTargets(cfg.VegetaPatternTarFile)
	if err != nil {
		t.Fatalf("loadTargets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets: got %d, want 2 (blank lines skipped)", len(targets))
	}
	tg := targets[0]
	if tg.Method != "POST" || tg.URL != "http://10.0.0.5:8545" {
		t.Errorf("target %s %s, want POST http://10.0.0.5:8545", tg.Method, tg.URL)
	}
	if got := tg.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type header: got %q", got)
	}
	if string(tg.Body) != jsonlCall {
		t.Errorf("body %s, want the line %s", tg.Body, jsonlCall)
	}
}

func TestLoadTargetsJSONLKeepsThePortOfTheClientAddress(t *testing.T) {
	pt, cfg, _ := newJSONLPerf(t, "10.0.0.5:8546", jsonlCall)

	targets, err := pt.loadTargets(cfg.VegetaPatternTarFile)
	if err != nil {
		t.Fatalf("loadTargets: %v", err)
	}
	if targets[0].URL != "http://10.0.0.5:8546" {
		t.Errorf("url %s, want http://10.0.0.5:8546", targets[0].URL)
	}
}

func TestLoadTargetsJSONLRejectsALineThatIsNotJSON(t *testing.T) {
	pt, cfg, _ := newJSONLPerf(t, "localhost", jsonlCall, "not json")

	if _, err := pt.loadTargets(cfg.VegetaPatternTarFile); err == nil {
		t.Error("want an error for a line that is not JSON")
	}
}

func TestExecuteReplaysAJSONLPattern(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x"}`))
	}))
	defer server.Close()

	pt, _, _ := newJSONLPerf(t, strings.TrimPrefix(server.URL, "http://"), jsonlCall)

	format := ResultFormat{MaxRepetitionDigits: 1, MaxQpsDigits: 2, MaxDurationDigits: 1}
	if err := pt.Execute(context.Background(), 1, 0, "rpcdaemon", 10, 1, format); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("the attack should have reached the server")
	}
	for _, b := range bodies {
		if b != jsonlCall {
			t.Errorf("server received %s, want %s", b, jsonlCall)
		}
	}
}

func TestJSONLTestType(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"one method", []string{jsonlCall, "", jsonlCall}, "eth_call"},
		{"several methods", []string{jsonlCall, `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`}, "mixed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(tc.lines, "\n")+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := JSONLTestType(path)
			if err != nil {
				t.Fatalf("JSONLTestType: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJSONLTestTypeWithoutRequestsFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.jsonl")
	if err := os.WriteFile(path, []byte("\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := JSONLTestType(path); err == nil {
		t.Error("want an error for a pattern without requests")
	}
}

func TestLoadTargetsByIDTagsEachRequestWithItsID(t *testing.T) {
	pt, cfg, _ := newJSONLPerf(t, "localhost",
		`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":"2-sload-storm"}`,
		`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":7}`,
		`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":"3-50%-gas"}`)
	cfg.ByID = true

	targets, err := pt.loadTargets(cfg.VegetaPatternTarFile)
	if err != nil {
		t.Fatalf("loadTargets: %v", err)
	}
	got := make([]string, 0, len(targets))
	for _, tg := range targets {
		_, frag, _ := strings.Cut(tg.URL, "#")
		got = append(got, frag)
	}
	if want := []string{"2-sload-storm", "7", "3-50%25-gas"}; !slices.Equal(got, want) {
		t.Errorf("fragments %v, want %v", got, want)
	}
}

func TestRunVegetaAttackByIDCountsResultsByID(t *testing.T) {
	srv := apiServer(t)
	pt, cfg, dirs := newTestPerf(t, srv.URL)
	cfg.ByID = true
	targets := []vegeta.Target{
		{Method: "POST", URL: srv.URL + "#1-div-loop", Body: []byte(`{"jsonrpc":"2.0","method":"eth_getLogs","params":[],"id":"1-div-loop"}`)},
		{Method: "POST", URL: srv.URL + "#3-50%25-gas", Body: []byte(`{"jsonrpc":"2.0","method":"eth_getLogs","params":[],"id":"3-50%-gas"}`)},
	}
	_, results, err := pt.runVegetaAttack(context.Background(), targets, 50, 200*time.Millisecond, filepath.Join(dirs.RunTestDir, "attack.bin"))
	if err != nil {
		t.Fatalf("runVegetaAttack: %v", err)
	}
	if results.apis["1-div-loop"] == nil || results.apis["3-50%-gas"] == nil {
		t.Errorf("results by %v, want 1-div-loop and 3-50%%-gas", slices.Collect(maps.Keys(results.apis)))
	}
}

func TestFormatIDResultsSortsByLeadingNumberAndFitsLongNames(t *testing.T) {
	ms := time.Millisecond
	ids := map[string]*apiResult{
		"10-call-loop":                         newAPIResult(map[string]int{}, ms),
		"2-div-loop-28M":                       newAPIResult(map[string]int{}, ms),
		"16-52c62f26-create-selfdestruct-2c20": newAPIResult(map[string]int{}, ms),
		"unnumbered":                           newAPIResult(map[string]int{}, ms),
	}
	got := formatIDResults(ids, false)
	want := []string{
		"      id                                      reqs  share   result    empty    error       p50       p99       max",
		"      2-div-loop-28M                             1  25.0%   100.0%     0.0%     0.0%    1.00ms    1.00ms    1.00ms",
		"      10-call-loop                               1  25.0%   100.0%     0.0%     0.0%    1.00ms    1.00ms    1.00ms",
		"      16-52c62f26-create-selfdestruct-2c20       1  25.0%   100.0%     0.0%     0.0%    1.00ms    1.00ms    1.00ms",
		"      unnumbered                                 1  25.0%   100.0%     0.0%     0.0%    1.00ms    1.00ms    1.00ms",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAttackResultsCountsTheOKLatencies(t *testing.T) {
	results := attackResults{rpcErrors: map[string]int{}, apis: map[string]*apiResult{}}
	for _, r := range []vegeta.Result{
		{Code: http.StatusOK, Latency: time.Millisecond, Body: []byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`)},
		{Code: http.StatusOK, Latency: 2 * time.Millisecond, Body: []byte(`{"jsonrpc":"2.0","id":1,"result":[]}`)},
		{Code: http.StatusOK, Latency: 100 * time.Millisecond, Body: []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted"}}`)},
		{Code: http.StatusInternalServerError, Latency: 200 * time.Millisecond},
		{Error: "connection refused", Latency: 300 * time.Millisecond},
	} {
		results.add(&r)
	}
	if results.okRequests != 2 {
		t.Errorf("ok requests = %d, want 2: a result, also empty, with HTTP 200", results.okRequests)
	}
	if results.ok.Max != 2*time.Millisecond {
		t.Errorf("ok max = %v, want 2ms: the failed requests are left out", results.ok.Max)
	}
}

func TestFormatOKResults(t *testing.T) {
	ok := OKMetrics{Ratio: 0.5}
	for range 2 {
		ok.Latencies.Add(2 * time.Millisecond)
	}
	if got, want := formatOKResults(ok, false), " rpc_success= 50.00% p99_ok=  2.00ms"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	want := " rpc_success= 50.00% lat_ok=[p50=  2.00ms  p90=  2.00ms  p95=  2.00ms  p99=  2.00ms  max=  2.00ms]"
	if got := formatOKResults(ok, true); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatOKResultsWithoutOKResponses(t *testing.T) {
	if got, want := formatOKResults(OKMetrics{}, false), " rpc_success=  0.00% p99_ok=       -"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := okLatencies(&vegeta.LatencyMetrics{}); got["99th"] != 0 || got["max"] != 0 {
		t.Errorf("latencies without OK responses: got %v, want 0", got)
	}
}
