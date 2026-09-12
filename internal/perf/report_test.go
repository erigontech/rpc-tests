package perf

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// reportDirs points a report at a scratch directory instead of ./perf/reports.
func reportDirs(t *testing.T) *RunDirs {
	t.Helper()
	quietStdout(t)
	dirs := NewRunDirs()
	dirs.RunTestDir = filepath.Join(t.TempDir(), "run")
	dirs.PatternDir = dirs.RunTestDir + "/erigon_stress_test"
	dirs.TarFileName = dirs.RunTestDir + "/vegeta_TAR_File"
	dirs.PatternBase = dirs.RunTestDir + "/erigon_stress_test/vegeta_erigon_"
	return dirs
}

// csvReportPath returns the single .csv file the report created under dirs.
func csvReportPath(t *testing.T, dirs *RunDirs, chain string) string {
	t.Helper()
	var found string
	root := filepath.Join(dirs.RunTestDir, chain)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".csv") {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if found == "" {
		t.Fatalf("no CSV report found under %s", root)
	}
	return found
}

func sampleMetrics(repetition int) *PerfMetrics {
	return &PerfMetrics{
		ClientName:   "rpcdaemon",
		TestNumber:   1,
		Repetition:   repetition,
		QPS:          100,
		Duration:     5,
		MinLatency:   "1.00ms",
		Mean:         "2.00ms",
		P50:          "1.80ms",
		P90:          "3.00ms",
		P95:          "3.50ms",
		P99:          "4.00ms",
		MaxLatency:   "9.00ms",
		SuccessRatio: "100.00%",
	}
}

func TestTestReportCSVLifecycle(t *testing.T) {
	dirs := reportDirs(t)
	cfg := NewConfig()
	cfg.TestType = "eth_getLogs"
	cfg.TestingClient = "rpcdaemon"

	report := NewTestReport(cfg, dirs)
	if err := report.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := report.WriteTestReport(sampleMetrics(0)); err != nil {
		t.Fatalf("WriteTestReport: %v", err)
	}
	if err := report.WriteTestReport(sampleMetrics(1)); err != nil {
		t.Fatalf("WriteTestReport: %v", err)
	}
	if err := report.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := csvReportPath(t, dirs, cfg.ChainName)
	if !strings.Contains(filepath.Base(path), "eth_getLogs") ||
		!strings.Contains(filepath.Base(path), "rpcdaemon") {
		t.Errorf("report filename should carry the API and client, got %q", filepath.Base(path))
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open report: %v", err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse report: %v", err)
	}

	var header []string
	var dataRows [][]string
	for _, row := range records {
		if len(row) == 14 && row[0] == "ClientName" {
			header = row
			continue
		}
		if header != nil && len(row) == 14 {
			dataRows = append(dataRows, row)
		}
	}
	if header == nil {
		t.Fatalf("column header row not found in:\n%v", records)
	}
	if len(dataRows) != 2 {
		t.Fatalf("data rows: got %d, want 2", len(dataRows))
	}
	if dataRows[0][0] != "rpcdaemon" || dataRows[0][3] != "100" || dataRows[0][12] != "100.00%" {
		t.Errorf("unexpected first data row: %v", dataRows[0])
	}
	if dataRows[1][2] != "1" {
		t.Errorf("second row should be repetition 1, got %v", dataRows[1])
	}

	// The preamble carries the platform/configuration key-value rows.
	flat := flatten(records)
	for _, key := range []string{"vendor", "cpu", "kernel", "taskset", "vegetaFile", "goVersion", "clientVersion"} {
		if !strings.Contains(flat, key) {
			t.Errorf("preamble is missing the %q row", key)
		}
	}
}

func flatten(records [][]string) string {
	var sb strings.Builder
	for _, row := range records {
		sb.WriteString(strings.Join(row, ","))
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestTestReportCSVWithoutClientName covers the filename branch used when no
// testing client is configured.
func TestTestReportCSVWithoutClientName(t *testing.T) {
	dirs := reportDirs(t)
	cfg := NewConfig()
	cfg.TestType = "eth_call"
	cfg.TestingClient = ""

	report := NewTestReport(cfg, dirs)
	if err := report.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := report.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	name := filepath.Base(csvReportPath(t, dirs, cfg.ChainName))
	if !strings.HasPrefix(name, "eth_call_") || !strings.HasSuffix(name, "_perf.csv") {
		t.Errorf("unexpected report filename %q", name)
	}
}

func TestTestReportOpenUnwritableLocation(t *testing.T) {
	dirs := reportDirs(t)
	// A regular file where the report directory must go makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	dirs.RunTestDir = blocker

	cfg := NewConfig()
	if err := NewTestReport(cfg, dirs).Open(); err == nil {
		t.Error("expected an error when the report directory cannot be created")
	}
}

// writeVegetaBinary produces a vegeta result file like the one an attack writes.
func writeVegetaBinary(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer file.Close()

	encoder := vegeta.NewEncoder(file)
	start := time.Now()
	for i := range 20 {
		result := &vegeta.Result{
			Attack:    "vegeta-attack",
			Seq:       uint64(i),
			Code:      200,
			Timestamp: start.Add(time.Duration(i) * time.Millisecond),
			Latency:   time.Duration(i+1) * time.Millisecond,
			BytesOut:  64,
			BytesIn:   128,
		}
		if err := encoder.Encode(result); err != nil {
			t.Fatalf("encode result: %v", err)
		}
	}
}

func TestGenerateJSONReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin", "run.bin")
	writeVegetaBinary(t, path)

	report, err := generateJSONReport(path)
	if err != nil {
		t.Fatalf("generateJSONReport: %v", err)
	}
	if got, ok := report["requests"].(float64); !ok || got != 20 {
		t.Errorf("requests: got %v, want 20", report["requests"])
	}
	for _, key := range []string{"latencies", "status_codes", "success", "throughput"} {
		if _, ok := report[key]; !ok {
			t.Errorf("report is missing the %q field", key)
		}
	}
}

func TestGenerateJSONReportMissingFile(t *testing.T) {
	if _, err := generateJSONReport(filepath.Join(t.TempDir(), "absent.bin")); err == nil {
		t.Error("expected an error for a missing binary file")
	}
}

func TestGenerateJSONReportCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.bin")
	if err := os.WriteFile(path, []byte("not a vegeta stream"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := generateJSONReport(path); err == nil {
		t.Error("expected an error for a corrupt binary file")
	}
}

func TestGenerateHdrPlot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin", "run.bin")
	writeVegetaBinary(t, path)

	plot, err := generateHdrPlot(path)
	if err != nil {
		t.Fatalf("generateHdrPlot: %v", err)
	}
	if plot == "" {
		t.Fatal("HDR plot should not be empty")
	}
	if !strings.Contains(plot, "Value") || !strings.Contains(plot, "Percentile") {
		t.Errorf("HDR plot is missing its header:\n%s", plot)
	}
}

func TestGenerateHdrPlotMissingFile(t *testing.T) {
	if _, err := generateHdrPlot(filepath.Join(t.TempDir(), "absent.bin")); err == nil {
		t.Error("expected an error for a missing binary file")
	}
}

func TestTestReportJSONLifecycle(t *testing.T) {
	dirs := reportDirs(t)
	jsonPath := filepath.Join(t.TempDir(), "report.json")

	cfg := NewConfig()
	cfg.TestType = "eth_getLogs"
	cfg.TestingClient = "rpcdaemon"
	cfg.JSONReportFile = jsonPath
	cfg.ClientVegetaOnCore = "0-3:4-7"
	cfg.TestSequence = "100:5"
	cfg.Repetitions = 2

	binary := filepath.Join(dirs.RunTestDir, BinaryDir, "run.bin")
	writeVegetaBinary(t, binary)
	cfg.BinaryFile = "run.bin"
	cfg.BinaryFileFullPathname = binary

	report := NewTestReport(cfg, dirs)
	if err := report.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Repetition 0 opens a new QPS/duration group; repetition 1 joins it.
	if err := report.WriteTestReport(sampleMetrics(0)); err != nil {
		t.Fatalf("WriteTestReport(0): %v", err)
	}
	if err := report.WriteTestReport(sampleMetrics(1)); err != nil {
		t.Fatalf("WriteTestReport(1): %v", err)
	}
	if err := report.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read JSON report: %v", err)
	}
	var parsed JSONReport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON report is malformed: %v", err)
	}

	if parsed.Configuration.TestingDaemon != "rpcdaemon" {
		t.Errorf("testingDaemon: got %q", parsed.Configuration.TestingDaemon)
	}
	if parsed.Configuration.TestingAPI != "eth_getLogs" {
		t.Errorf("testingApi: got %q", parsed.Configuration.TestingAPI)
	}
	if parsed.Configuration.TestSequence != "100:5" {
		t.Errorf("testSequence: got %q", parsed.Configuration.TestSequence)
	}
	if parsed.Configuration.TestRepetitions != 2 {
		t.Errorf("testRepetitions: got %d", parsed.Configuration.TestRepetitions)
	}
	if parsed.Configuration.Taskset != "0-3:4-7" {
		t.Errorf("taskset: got %q", parsed.Configuration.Taskset)
	}
	if parsed.Platform.GoVersion == "" {
		t.Error("platform.goVersion should be populated")
	}

	if len(parsed.Results) != 1 {
		t.Fatalf("results: got %d groups, want 1", len(parsed.Results))
	}
	group := parsed.Results[0]
	if group.QPS != "100" || group.Duration != "5" {
		t.Errorf("group key: got qps=%q duration=%q", group.QPS, group.Duration)
	}
	if len(group.TestRepetitions) != 2 {
		t.Fatalf("repetitions: got %d, want 2", len(group.TestRepetitions))
	}
	for i, rep := range group.TestRepetitions {
		if rep.VegetaBinary != "run.bin" {
			t.Errorf("repetition %d binary name: got %q", i, rep.VegetaBinary)
		}
		if rep.VegetaReportHdrPlot == "" {
			t.Errorf("repetition %d is missing its HDR plot", i)
		}
		if _, ok := rep.VegetaReport["requests"]; !ok {
			t.Errorf("repetition %d is missing the vegeta report", i)
		}
	}
}

func TestTestReportJSONMissingBinaryFile(t *testing.T) {
	dirs := reportDirs(t)
	cfg := NewConfig()
	cfg.TestingClient = "rpcdaemon"
	cfg.JSONReportFile = filepath.Join(t.TempDir(), "report.json")
	cfg.BinaryFileFullPathname = filepath.Join(t.TempDir(), "absent.bin")

	report := NewTestReport(cfg, dirs)
	if err := report.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer report.Close()

	if err := report.WriteTestReport(sampleMetrics(0)); err == nil {
		t.Error("expected an error when the vegeta binary is missing")
	}
}

func TestTestReportCloseUnwritableJSONPath(t *testing.T) {
	dirs := reportDirs(t)
	cfg := NewConfig()
	cfg.TestingClient = "rpcdaemon"
	cfg.JSONReportFile = filepath.Join(t.TempDir(), "missing-dir", "report.json")

	report := NewTestReport(cfg, dirs)
	if err := report.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := report.Close(); err == nil {
		t.Error("expected an error when the JSON report cannot be written")
	}
}

// TestTestReportCloseWithoutOpen checks that Close is safe on a report that was
// never opened, which is how a failed setup unwinds.
func TestTestReportCloseWithoutOpen(t *testing.T) {
	cfg := NewConfig()
	if err := NewTestReport(cfg, reportDirs(t)).Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetFileChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, []byte("some content"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := GetFileChecksum(path)
	if got == "" {
		t.Skip("the 'sum' utility is not available on this system")
	}
	if got != GetFileChecksum(path) {
		t.Error("the checksum should be stable across calls")
	}

	other := filepath.Join(t.TempDir(), "other.bin")
	if err := os.WriteFile(other, []byte("different content entirely"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if GetFileChecksum(other) == got {
		t.Error("different content should produce a different checksum")
	}
}

func TestGetFileChecksumMissingFile(t *testing.T) {
	if got := GetFileChecksum(filepath.Join(t.TempDir(), "absent")); got != "" {
		t.Errorf("got %q, want empty for a missing file", got)
	}
}

func TestGetGCCVersion(t *testing.T) {
	// Returns either the compiler banner or the "unknown" fallback; both are
	// valid, so only the empty string would be a bug.
	if got := GetGCCVersion(); got == "" {
		t.Error("GetGCCVersion should never return an empty string")
	}
}

func TestGetGitCommit(t *testing.T) {
	if got := GetGitCommit(t.TempDir()); got != "" {
		t.Errorf("a non-repository directory should yield no commit, got %q", got)
	}
}
