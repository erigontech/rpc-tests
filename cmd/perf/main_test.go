package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

// patternArchive writes the tar.gz layout rpc_perf expects:
// erigon_stress_test/vegeta_erigon_<testType>.txt holding one JSON target
// per line.
func patternArchive(t *testing.T, testType, targetURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pattern.tar.gz")

	target, err := json.Marshal(map[string]any{
		"method": "POST",
		"url":    targetURL,
		"body":   []byte(`{"jsonrpc":"2.0","method":"eth_getLogs","params":[],"id":1}`),
		"header": map[string][]string{"Content-Type": {"application/json"}},
	})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	// extractTarGz only creates directories it sees a header for, so the
	// enclosing directory entry has to be present, as it is in real archives.
	if err := tw.WriteHeader(&tar.Header{
		Name: "erigon_stress_test", Typeflag: tar.TypeDir, Mode: 0755,
	}); err != nil {
		t.Fatalf("tar dir header: %v", err)
	}
	name := "erigon_stress_test/vegeta_erigon_" + testType + ".txt"
	body := string(target) + "\n"
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := io.WriteString(tw, body); err != nil {
		t.Fatalf("tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return path
}

// runPerf invokes the rpc_perf CLI with the given arguments, silencing the
// progress output it writes to stdout.
func runPerf(t *testing.T, args ...string) error {
	t.Helper()
	quietStdout(t)

	app := &cli.App{
		Name:           "rpc_perf",
		Flags:          perfFlags(),
		Action:         runPerfTests,
		Writer:         io.Discard,
		ErrWriter:      io.Discard,
		ExitErrHandler: func(*cli.Context, error) {},
	}
	return app.Run(append([]string{"rpc_perf"}, args...))
}

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

func fakeNode(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRunPerfTestsSuccess(t *testing.T) {
	server := fakeNode(t)
	pattern := patternArchive(t, "eth_getLogs", server.URL)

	err := runPerf(t,
		"--pattern-file", pattern,
		"--test-type", "eth_getLogs",
		"--test-sequence", "10:1",
		"--repetitions", "1",
		"--wait-after-test-sequence", "0",
		"--not-verify-server-alive",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPerfTestsInvalidSequence(t *testing.T) {
	server := fakeNode(t)
	pattern := patternArchive(t, "eth_getLogs", server.URL)

	err := runPerf(t, "--pattern-file", pattern, "--test-sequence", "not-a-sequence")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "failed to parse test sequence") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunPerfTestsMissingPatternFile(t *testing.T) {
	err := runPerf(t,
		"--pattern-file", filepath.Join(t.TempDir(), "absent.tar.gz"),
		"--test-sequence", "10:1",
		"--repetitions", "1",
	)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "failed to initialize performance test") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunPerfTestsInvalidBuildDir(t *testing.T) {
	err := runPerf(t,
		"--client-build-dir", filepath.Join(t.TempDir(), "absent"),
		"--test-sequence", "10:1",
	)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "configuration validation failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunPerfTestsEmptyCacheRequiresRoot(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine the current user: %v", err)
	}
	if current.Username == "root" {
		t.Skip("running as root, the restriction does not apply")
	}

	runErr := runPerf(t, "--empty-cache", "--test-sequence", "10:1")
	if runErr == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(runErr.Error(), "empty-cache option can only be used by root") {
		t.Errorf("unexpected error: %v", runErr)
	}
}

// TestRunPerfTestsWritesJSONReport drives the reporting path end to end.
func TestRunPerfTestsWritesJSONReport(t *testing.T) {
	server := fakeNode(t)
	pattern := patternArchive(t, "eth_getLogs", server.URL)
	reportPath := filepath.Join(t.TempDir(), "perf.json")

	err := runPerf(t,
		"--pattern-file", pattern,
		"--test-type", "eth_getLogs",
		"--test-sequence", "10:1",
		"--repetitions", "1",
		"--wait-after-test-sequence", "0",
		"--not-verify-server-alive",
		"--tmp-test-report",
		"--json-report", reportPath,
		"--more-percentiles",
		"--instant-report",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("JSON report was not written: %v", err)
	}
	var report struct {
		Configuration struct {
			TestingAPI      string `json:"testingApi"`
			TestSequence    string `json:"testSequence"`
			TestRepetitions int    `json:"testRepetitions"`
		} `json:"configuration"`
		Results []struct {
			QPS             string `json:"qps"`
			Duration        string `json:"duration"`
			TestRepetitions []struct {
				VegetaBinary string `json:"vegetaBinary"`
			} `json:"testRepetitions"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("JSON report is malformed: %v", err)
	}
	if report.Configuration.TestingAPI != "eth_getLogs" {
		t.Errorf("testingApi: got %q", report.Configuration.TestingAPI)
	}
	if report.Configuration.TestSequence != "10:1" {
		t.Errorf("testSequence: got %q", report.Configuration.TestSequence)
	}
	if len(report.Results) != 1 {
		t.Fatalf("results: got %d groups, want 1", len(report.Results))
	}
	if report.Results[0].QPS != "10" || report.Results[0].Duration != "1" {
		t.Errorf("group key: qps=%q duration=%q", report.Results[0].QPS, report.Results[0].Duration)
	}
	if len(report.Results[0].TestRepetitions) != 1 {
		t.Errorf("repetitions: got %d, want 1", len(report.Results[0].TestRepetitions))
	}
}

func TestRunPerfTestsPropagatesAttackFailure(t *testing.T) {
	pattern := patternArchive(t, "eth_getLogs", "http://127.0.0.1:1")

	err := runPerf(t,
		"--pattern-file", pattern,
		"--test-type", "eth_getLogs",
		"--test-sequence", "5:1",
		"--repetitions", "1",
		"--wait-after-test-sequence", "0",
		"--not-verify-server-alive",
		"--response-timeout", "1s",
	)
	if err == nil {
		t.Fatal("expected an error when every request fails")
	}
	if !strings.Contains(err.Error(), "ratio is 0.00%") {
		t.Errorf("unexpected error: %v", err)
	}
}

// testingAPIOfJSONL replays a JSONL pattern of eth_getTransactionReceipt requests with args and
// returns the testingApi of the JSON report.
func testingAPIOfJSONL(t *testing.T, args ...string) string {
	t.Helper()
	server := fakeNode(t)
	pattern := filepath.Join(t.TempDir(), "receipts.jsonl")
	line := `{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0x01"],"id":1}`
	if err := os.WriteFile(pattern, []byte(line+"\n"+line+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(t.TempDir(), "perf.json")
	err := runPerf(t, append([]string{
		"--pattern-file", pattern,
		"--rpc-client-address", strings.TrimPrefix(server.URL, "http://"),
		"--test-sequence", "10:1",
		"--repetitions", "1",
		"--wait-after-test-sequence", "0",
		"--not-verify-server-alive",
		"--tmp-test-report",
		"--json-report", reportPath,
	}, args...)...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("JSON report was not written: %v", err)
	}
	var report struct {
		Configuration struct {
			TestingAPI string `json:"testingApi"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("JSON report is malformed: %v", err)
	}
	return report.Configuration.TestingAPI
}

func TestRunPerfTestsNamesAJSONLPatternAfterItsMethod(t *testing.T) {
	if got := testingAPIOfJSONL(t); got != "eth_getTransactionReceipt" {
		t.Errorf("testingApi without --test-type: got %q, want the method of the requests", got)
	}
	if got := testingAPIOfJSONL(t, "--test-type", "receipts_15M"); got != "receipts_15M" {
		t.Errorf("testingApi with --test-type: got %q, want receipts_15M", got)
	}
}

func TestRunPerfTestsByIDPrintsALinePerID(t *testing.T) {
	server := fakeNode(t)
	pattern := filepath.Join(t.TempDir(), "txs.jsonl")
	lines := `{"jsonrpc":"2.0","method":"eth_call","params":[],"id":"2-sload-storm"}` + "\n" +
		`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":"1-div-loop"}` + "\n"
	if err := os.WriteFile(pattern, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = w
	app := &cli.App{Name: "rpc_perf", Flags: perfFlags(), Action: runPerfTests, Writer: io.Discard, ErrWriter: io.Discard}
	runErr := app.Run([]string{"rpc_perf", "--pattern-file", pattern, "--rpc-client-address", strings.TrimPrefix(server.URL, "http://"),
		"--test-sequence", "20:1", "--repetitions", "1", "--wait-after-test-sequence", "0", "--not-verify-server-alive", "--by-id"})
	os.Stdout = original
	w.Close()
	out, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("rpc_perf --by-id: %v", runErr)
	}
	first, second := strings.Index(string(out), "      1-div-loop "), strings.Index(string(out), "      2-sload-storm ")
	if first < 0 || second < first {
		t.Errorf("want a line for 1-div-loop and then for 2-sload-storm, got:\n%s", out)
	}
}
