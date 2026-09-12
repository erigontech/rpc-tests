package perf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	if err := pt.processResults(1, 0, "rpcdaemon", 100, 5, metrics); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProcessResultsAllFailedIsAnError(t *testing.T) {
	pt, _, _ := newTestPerf(t, "http://localhost:8545")

	metrics := syntheticMetrics(errResult("connection refused"), errResult("connection refused"))
	err := pt.processResults(1, 0, "rpcdaemon", 100, 5, metrics)
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
	err := pt.processResults(1, 0, "rpcdaemon", 100, 5, metrics)
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
	if err := pt.processResults(1, 0, "rpcdaemon", 100, 5, metrics); err != nil {
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
