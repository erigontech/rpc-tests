package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/filter"
	internalrpc "github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

// suite lays out a fixture tree of api -> test files and returns a Config
// pointing at it, wired to server. The process is moved into a scratch
// directory because Run cleans up relative paths such as ./temp_rpc_tests.
func suite(t *testing.T, server *httptest.Server, files map[string][]map[string]any) *config.Config {
	t.Helper()
	root := t.TempDir()
	for name, commands := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		data, err := json.Marshal(commands)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	host, portStr, ok := strings.Cut(strings.TrimPrefix(server.URL, "http://"), ":")
	if !ok {
		t.Fatalf("unexpected server URL %q", server.URL)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.NewConfig()
	cfg.DaemonOnHost = host
	cfg.ServerPort = port
	cfg.JSONDir = root
	cfg.OutputDir = filepath.Join(root, config.ResultsDir) + string(os.PathSeparator)
	cfg.ExitOnFail = false
	cfg.DisplayOnlyFail = true

	t.Chdir(t.TempDir())
	return cfg
}

func newFilter(cfg *config.Config) *filter.TestFilter {
	return filter.New(filter.FilterConfig{
		Net:                cfg.Net,
		ReqTestNum:         cfg.ReqTestNum,
		TestingAPIs:        cfg.TestingAPIs,
		TestingAPIsWith:    cfg.TestingAPIsWith,
		ExcludeAPIList:     cfg.ExcludeAPIList,
		ExcludeTestList:    cfg.ExcludeTestList,
		TestsOnLatestBlock: cfg.TestsOnLatestBlock,
		DoNotCompareError:  cfg.DoNotCompareError,
		CommitmentHistory:  cfg.CommitmentHistory,
	})
}

func collect(t *testing.T, cfg *config.Config, transports []string) ([]*testdata.TestDescriptor, *Stats, []reportEntry) {
	t.Helper()
	discovery, err := testdata.DiscoverTests(cfg.JSONDir, cfg.ResultsDir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	stats := &Stats{}
	var entries []reportEntry
	var mu sync.Mutex
	descriptors := collectTestDescriptors(context.Background(), discovery, newFilter(cfg), cfg,
		transports, stats, &entries, &mu)
	return descriptors, stats, entries
}

func descriptorNames(descriptors []*testdata.TestDescriptor) []string {
	names := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		names = append(names, d.TransportType+":"+d.Name)
	}
	return names
}

func threeAPISuite(t *testing.T, server *httptest.Server) *config.Config {
	t.Helper()
	return suite(t, server, map[string][]map[string]any{
		filepath.Join("eth_call", "test_01.json"):    {command("eth_call", "0x1")},
		filepath.Join("eth_call", "test_02.json"):    {command("eth_call", "0x1")},
		filepath.Join("eth_getLogs", "test_01.json"): {command("eth_getLogs", []any{})},
	})
}

func TestCollectTestDescriptorsSelectsEverythingByDefault(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)

	descriptors, stats, _ := collect(t, cfg, []string{"http"})

	want := []string{
		"http:eth_call/test_01.json",
		"http:eth_call/test_02.json",
		"http:eth_getLogs/test_01.json",
	}
	if strings.Join(descriptorNames(descriptors), ",") != strings.Join(want, ",") {
		t.Errorf("descriptors: got %v, want %v", descriptorNames(descriptors), want)
	}
	if stats.ScheduledTests != 3 {
		t.Errorf("ScheduledTests: got %d, want 3", stats.ScheduledTests)
	}
	for i, d := range descriptors {
		if d.Number != i+1 {
			t.Errorf("descriptor %d has global number %d, want %d", i, d.Number, i+1)
		}
	}
}

func TestCollectTestDescriptorsRepeatsPerTransport(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)

	descriptors, stats, _ := collect(t, cfg, []string{"http", "websocket"})

	if len(descriptors) != 6 {
		t.Fatalf("descriptors: got %d, want 6", len(descriptors))
	}
	if stats.ScheduledTests != 6 {
		t.Errorf("ScheduledTests: got %d, want 6", stats.ScheduledTests)
	}
	// Numbering restarts per transport.
	if descriptors[0].Number != 1 || descriptors[3].Number != 1 {
		t.Errorf("each transport should renumber from 1, got %d and %d",
			descriptors[0].Number, descriptors[3].Number)
	}
	if descriptors[3].TransportType != "websocket" {
		t.Errorf("second block transport: got %q", descriptors[3].TransportType)
	}
}

func TestCollectTestDescriptorsAppliesAPIFilter(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)
	cfg.TestingAPIs = "eth_getLogs"

	descriptors, _, _ := collect(t, cfg, []string{"http"})

	if len(descriptors) != 1 || descriptors[0].Name != filepath.Join("eth_getLogs", "test_01.json") {
		t.Fatalf("descriptors: got %v, want only eth_getLogs/test_01.json", descriptorNames(descriptors))
	}
	// Global numbering is unaffected by filtering: eth_getLogs/test_01 is 3rd.
	if descriptors[0].Number != 3 {
		t.Errorf("global number: got %d, want 3", descriptors[0].Number)
	}
}

func TestCollectTestDescriptorsSingleTestNumber(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)
	cfg.ReqTestNum = 2

	descriptors, _, _ := collect(t, cfg, []string{"http"})

	if len(descriptors) != 1 || descriptors[0].Number != 2 {
		t.Fatalf("descriptors: got %v, want only test number 2", descriptorNames(descriptors))
	}
}

func TestCollectTestDescriptorsStartFromTest(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)
	cfg.StartTest = "3"
	cfg.StartTestNum = 3

	descriptors, _, _ := collect(t, cfg, []string{"http"})

	if len(descriptors) != 1 || descriptors[0].Number != 3 {
		t.Fatalf("descriptors: got %v, want only test number 3", descriptorNames(descriptors))
	}
}

func TestCollectTestDescriptorsHonoursCancellation(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)

	discovery, err := testdata.DiscoverTests(cfg.JSONDir, cfg.ResultsDir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stats := &Stats{}
	var entries []reportEntry
	var mu sync.Mutex
	descriptors := collectTestDescriptors(ctx, discovery, newFilter(cfg), cfg,
		[]string{"http"}, stats, &entries, &mu)

	if len(descriptors) != 0 {
		t.Errorf("a cancelled context should collect nothing, got %v", descriptorNames(descriptors))
	}
}

func TestRunTestSliceExecutesEveryTest(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)

	descriptors, _, _ := collect(t, cfg, []string{"http"})
	stats := &Stats{}
	var entries []reportEntry
	var mu sync.Mutex
	w := bufio.NewWriter(io.Discard)

	clients := map[string]*internalrpc.Client{"http": internalrpc.NewClient("http", "", 0)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runTestSlice(ctx, cancel, descriptors, cfg, clients, 2, stats, w, &entries, &mu)
	w.Flush()

	if stats.ExecutedTests != 3 {
		t.Errorf("ExecutedTests: got %d, want 3", stats.ExecutedTests)
	}
	if stats.SuccessTests != 3 {
		t.Errorf("SuccessTests: got %d, want 3 (failures: %d)", stats.SuccessTests, stats.FailedTests)
	}
	// runTestSlice re-indexes the slice from 0 so results can be ordered.
	for i, d := range descriptors {
		if d.Index != i {
			t.Errorf("descriptor %d has Index %d", i, d.Index)
		}
	}
}

func TestRunTestSliceEmptySliceIsANoOp(t *testing.T) {
	cfg := config.NewConfig()
	stats := &Stats{}
	var entries []reportEntry
	var mu sync.Mutex
	w := bufio.NewWriter(io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runTestSlice(ctx, cancel, nil, cfg, nil, 2, stats, w, &entries, &mu)

	if stats.ExecutedTests != 0 {
		t.Errorf("ExecutedTests: got %d, want 0", stats.ExecutedTests)
	}
}

func TestRunTestSliceStopsOnCancelledContext(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)

	descriptors, _, _ := collect(t, cfg, []string{"http"})
	stats := &Stats{}
	var entries []reportEntry
	var mu sync.Mutex
	w := bufio.NewWriter(io.Discard)

	clients := map[string]*internalrpc.Client{"http": internalrpc.NewClient("http", "", 0)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runTestSlice(ctx, cancel, descriptors, cfg, clients, 2, stats, w, &entries, &mu)

	if stats.ExecutedTests != 0 {
		t.Errorf("no test should run under a cancelled context, got %d", stats.ExecutedTests)
	}
}

func TestRunAllTestsPass(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var code int
	var err error
	output := captureStdout(t, func() { code, err = Run(ctx, cancel, cfg) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code: got %d, want 0\n%s", code, output)
	}
	for _, want := range []string{"Number of executed tests:     3", "Number of failed tests:       0"} {
		if !strings.Contains(output, want) {
			t.Errorf("summary is missing %q:\n%s", want, output)
		}
	}
}

func TestRunReportsFailures(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0xdifferent"}, &calls)
	defer server.Close()
	cfg := suite(t, server, map[string][]map[string]any{
		filepath.Join("eth_call", "test_01.json"): {command("eth_call", "0x1")},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var code int
	var err error
	output := captureStdout(t, func() { code, err = Run(ctx, cancel, cfg) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code: got %d, want 1\n%s", code, output)
	}
	if !strings.Contains(output, "Number of failed tests:       1") {
		t.Errorf("summary should report the failure:\n%s", output)
	}
}

func TestRunWritesCSVAndJSONReports(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)
	cfg.VerboseLevel = 1
	reportPath := filepath.Join(t.TempDir(), "summary.csv")
	cfg.ReportFile = reportPath

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	captureStdout(t, func() { _, _ = Run(ctx, cancel, cfg) })

	csvData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("CSV report was not written: %v", err)
	}
	if !strings.Contains(string(csvData), "eth_call") {
		t.Errorf("CSV report is missing the tested API:\n%s", csvData)
	}

	jsonPath := filepath.Join(cfg.OutputDir, "test_report.json")
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("JSON report was not written: %v", err)
	}
	var report testReport
	if err := json.Unmarshal(jsonData, &report); err != nil {
		t.Fatalf("JSON report is malformed: %v", err)
	}
	if report.Summary.ExecutedTests != 3 {
		t.Errorf("JSON report ExecutedTests: got %d, want 3", report.Summary.ExecutedTests)
	}
	if len(report.TestResults) != 3 {
		t.Errorf("JSON report entries: got %d, want 3", len(report.TestResults))
	}
}

func TestRunLoopsRequestedNumberOfTimes(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1"}, &calls)
	defer server.Close()
	cfg := suite(t, server, map[string][]map[string]any{
		filepath.Join("eth_call", "test_01.json"): {command("eth_call", "0x1")},
	})
	cfg.LoopNumber = 3

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := captureStdout(t, func() { _, _ = Run(ctx, cancel, cfg) })

	if !strings.Contains(output, "Number of executed tests:     3") {
		t.Errorf("three iterations of one test should execute 3 tests:\n%s", output)
	}
	if !strings.Contains(output, "Test iteration: 3") {
		t.Errorf("iteration banners are missing:\n%s", output)
	}
}

func TestRunUnknownTestDirectory(t *testing.T) {
	cfg := config.NewConfig()
	cfg.JSONDir = filepath.Join(t.TempDir(), "does-not-exist")
	cfg.OutputDir = filepath.Join(t.TempDir(), "results") + string(os.PathSeparator)
	t.Chdir(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var code int
	var err error
	captureStdout(t, func() { code, err = Run(ctx, cancel, cfg) })
	if err == nil {
		t.Error("expected an error for a missing test directory")
	}
	if code != -1 {
		t.Errorf("exit code: got %d, want -1", code)
	}
}

func TestRunWarnsWhenAPIFilterSelectsNothing(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1"}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)
	cfg.TestingAPIsWith = "no_such_api"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := captureStdout(t, func() { _, _ = Run(ctx, cancel, cfg) })

	if !strings.Contains(output, "WARN: API filter no_such_api selected no tests") {
		t.Errorf("expected a warning about the empty selection:\n%s", output)
	}
}

func TestRunCleansOutputDirectory(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(cfg.OutputDir, "stale-diff.json")
	if err := os.WriteFile(stale, []byte("old"), 0644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	captureStdout(t, func() { _, _ = Run(ctx, cancel, cfg) })

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale result should have been removed, stat err = %v", err)
	}
}

func TestRunSerialMode(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)
	cfg.Parallel = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := captureStdout(t, func() { _, _ = Run(ctx, cancel, cfg) })

	if !strings.Contains(output, "Run tests in serial") {
		t.Errorf("expected the serial banner:\n%s", output)
	}
	if !strings.Contains(output, "Number of executed tests:     3") {
		t.Errorf("all tests should still run:\n%s", output)
	}
}

func TestRunCompressionBanner(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1"}, &calls)
	defer server.Close()
	cfg := suite(t, server, map[string][]map[string]any{
		filepath.Join("eth_call", "test_01.json"): {command("eth_call", "0x1")},
	})
	cfg.TransportType = "http_comp"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := captureStdout(t, func() { _, _ = Run(ctx, cancel, cfg) })

	if !strings.Contains(output, "Run tests using compression") {
		t.Errorf("expected the compression banner:\n%s", output)
	}
}

// latestCommand builds a fixture command carrying metadata.latest, which is
// what the -L (tests-on-latest-block) filter selects on.
func latestCommand(method string, expectedResult any) map[string]any {
	cmd := command(method, expectedResult)
	cmd["metadata"] = map[string]any{"latest": true}
	return cmd
}

func TestCollectTestDescriptorsRecordsSkips(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)
	cfg.ExcludeTestList = "2"
	cfg.DisplayOnlyFail = false
	cfg.ReportFile = filepath.Join(t.TempDir(), "summary.csv")

	var descriptors []*testdata.TestDescriptor
	var stats *Stats
	var entries []reportEntry
	output := captureStdout(t, func() {
		descriptors, stats, entries = collect(t, cfg, []string{"http"})
	})

	if len(descriptors) != 2 {
		t.Errorf("descriptors: got %v, want 2 tests", descriptorNames(descriptors))
	}
	if stats.SkippedTests != 1 {
		t.Errorf("SkippedTests: got %d, want 1", stats.SkippedTests)
	}
	if !strings.Contains(output, "skipped") {
		t.Errorf("the skipped test should be printed:\n%s", output)
	}
	var skipped int
	for _, e := range entries {
		if e.Result == "SKIPPED" {
			skipped++
			if e.TestNumber != 2 {
				t.Errorf("skipped entry number: got %d, want 2", e.TestNumber)
			}
		}
	}
	if skipped != 1 {
		t.Errorf("report entries with SKIPPED: got %d, want 1", skipped)
	}
}

func TestCollectTestDescriptorsExcludedAPIIsNotScheduled(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_getLogs": []any{}}, &calls)
	defer server.Close()
	cfg := threeAPISuite(t, server)
	cfg.ExcludeAPIList = "eth_call"
	cfg.DisplayOnlyFail = false

	var descriptors []*testdata.TestDescriptor
	var stats *Stats
	captureStdout(t, func() { descriptors, stats, _ = collect(t, cfg, []string{"http"}) })

	if len(descriptors) != 1 || !strings.Contains(descriptors[0].Name, "eth_getLogs") {
		t.Errorf("descriptors: got %v, want only eth_getLogs", descriptorNames(descriptors))
	}
	if stats.SkippedTests != 2 {
		t.Errorf("SkippedTests: got %d, want 2", stats.SkippedTests)
	}
}

func TestSyncLatestBlockAgreeingNodes(t *testing.T) {
	var calls []string
	testing1 := rpcServer(t, map[string]any{"eth_blockNumber": "0x64"}, &calls)
	defer testing1.Close()
	reference := rpcServer(t, map[string]any{"eth_blockNumber": "0x64"}, &calls)
	defer reference.Close()

	host, portStr, _ := strings.Cut(strings.TrimPrefix(testing1.URL, "http://"), ":")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.NewConfig()
	cfg.DaemonOnHost = host
	cfg.ServerPort = port
	cfg.ExternalProviderURL = strings.TrimPrefix(reference.URL, "http://")
	cfg.SyncRetries = 2
	cfg.VerboseLevel = 1

	captureStdout(t, func() {
		if err := syncLatestBlock(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestSyncLatestBlockDivergingNodes(t *testing.T) {
	var calls []string
	testing1 := rpcServer(t, map[string]any{"eth_blockNumber": "0x64"}, &calls)
	defer testing1.Close()
	reference := rpcServer(t, map[string]any{"eth_blockNumber": "0xc8"}, &calls)
	defer reference.Close()

	host, portStr, _ := strings.Cut(strings.TrimPrefix(testing1.URL, "http://"), ":")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.NewConfig()
	cfg.DaemonOnHost = host
	cfg.ServerPort = port
	cfg.ExternalProviderURL = strings.TrimPrefix(reference.URL, "http://")
	cfg.SyncRetries = 1

	var syncErr error
	captureStdout(t, func() { syncErr = syncLatestBlock(cfg) })
	if syncErr == nil {
		t.Error("expected an error when the two nodes never agree")
	}
}

func TestRunLatestBatchMode(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0x1", "eth_blockNumber": "0x64"}, &calls)
	defer server.Close()

	files := map[string][]map[string]any{}
	for i := 1; i <= 5; i++ {
		name := filepath.Join("eth_call", "test_0"+strconv.Itoa(i)+".json")
		files[name] = []map[string]any{latestCommand("eth_call", "0x1")}
	}
	cfg := suite(t, server, files)
	cfg.TestsOnLatestBlock = true
	cfg.LatestBatchSize = 2
	cfg.VerifyWithDaemon = true
	cfg.DaemonAsReference = config.ExternalProvider
	cfg.ExternalProviderURL = strings.TrimPrefix(server.URL, "http://")
	cfg.SyncRetries = 2

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := captureStdout(t, func() { _, _ = Run(ctx, cancel, cfg) })

	if !strings.Contains(output, "Latest batch 1/3 (2 tests)") {
		t.Errorf("expected batched execution:\n%s", output)
	}
	if !strings.Contains(output, "Latest batch 3/3 (1 tests)") {
		t.Errorf("expected a final short batch:\n%s", output)
	}
}

func TestRunLatestBatchStopsAfterAFailingBatch(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"eth_call": "0xunexpected", "eth_blockNumber": "0x64"}, &calls)
	defer server.Close()

	files := map[string][]map[string]any{}
	for i := 1; i <= 4; i++ {
		name := filepath.Join("eth_call", "test_0"+strconv.Itoa(i)+".json")
		files[name] = []map[string]any{latestCommand("eth_call", "0x1")}
	}
	cfg := suite(t, server, files)
	cfg.TestsOnLatestBlock = true
	cfg.LatestBatchSize = 2
	cfg.LatestRetries = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := captureStdout(t, func() { _, _ = Run(ctx, cancel, cfg) })

	if !strings.Contains(output, "had failures, stopping") {
		t.Errorf("expected the batch loop to stop after failures:\n%s", output)
	}
	if strings.Contains(output, "Latest batch 2/2") {
		t.Errorf("the second batch should never start:\n%s", output)
	}
}
