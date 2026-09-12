package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleEntries() []reportEntry {
	return []reportEntry{
		{TestNumber: 1, TransportType: "http", TestName: "eth_call/test_01.json", Result: "OK"},
		{TestNumber: 2, TransportType: "http", TestName: "eth_call/test_02.json", Result: "FAILED", ErrorMessage: "boom"},
		{TestNumber: 3, TransportType: "http", TestName: "eth_getLogs/test_01.json", Result: "OK"},
		{TestNumber: 4, TransportType: "http", TestName: "eth_getLogs/test_02.json", Result: "SKIPPED"},
		{TestNumber: 5, TransportType: "http", TestName: "debug_traceCall/test_01.json", Result: "FAILED"},
	}
}

func TestGenerateCSVReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.csv")
	if err := generateCSVReport(path, sampleEntries()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	want := []string{
		"#,api_name,tests_done,tests_failed,NOTA",
		"1,debug_traceCall,1,1,",
		"2,eth_call,2,1,",
		// The SKIPPED entry is excluded, so eth_getLogs counts one test.
		"3,eth_getLogs,1,0,",
		",TOTAL,4,2,",
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(want), data)
	}
	for i := range want {
		if strings.TrimRight(lines[i], "\r") != want[i] {
			t.Errorf("line %d: got %q, want %q", i, lines[i], want[i])
		}
	}
}

// TestGenerateCSVReportTextFormat covers the aligned-table branch used for any
// extension other than .csv.
func TestGenerateCSVReportTextFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.txt")
	if err := generateCSVReport(path, sampleEntries()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	text := string(data)
	for _, want := range []string{"api_name", "tests_done", "tests_failed", "NOTA", "TOTAL", "eth_call", "debug_traceCall"} {
		if !strings.Contains(text, want) {
			t.Errorf("report is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, ",") {
		t.Errorf("the text format should not be comma separated:\n%s", text)
	}
}

// TestGenerateCSVReportTestNameWithoutAPI covers entries whose name carries no
// "api/" prefix: the whole name becomes the API bucket.
func TestGenerateCSVReportTestNameWithoutAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.csv")
	entries := []reportEntry{{TestNumber: 1, TestName: "loose_test.json", Result: "OK"}}
	if err := generateCSVReport(path, entries); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "loose_test.json") {
		t.Errorf("report should bucket the bare name:\n%s", data)
	}
}

func TestGenerateCSVReportEmptyEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.csv")
	if err := generateCSVReport(path, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(data), ",TOTAL,0,0,") {
		t.Errorf("expected a zeroed total row, got:\n%s", data)
	}
}

func TestGenerateCSVReportUnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "summary.csv")
	if err := generateCSVReport(path, sampleEntries()); err == nil {
		t.Error("expected an error when the report file cannot be created")
	}
}

func TestGenerateReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_report.json")
	start := time.Date(2026, 9, 11, 10, 30, 0, 0, time.UTC)
	stats := &Stats{
		SuccessTests:  7,
		FailedTests:   2,
		ExecutedTests: 9,
		SkippedTests:  3,
	}

	err := generateReport(path, start, 90*time.Second, stats, 42, 5, 2, sampleEntries())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report testReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}

	summary := report.Summary
	if summary.StartTime != "2026-09-11T10:30:00Z" {
		t.Errorf("StartTime: got %q", summary.StartTime)
	}
	if summary.TimeElapsed != "1m30s" {
		t.Errorf("TimeElapsed: got %q, want 1m30s", summary.TimeElapsed)
	}
	if summary.AvailableTests != 42 || summary.AvailableAPIs != 5 || summary.NumberOfLoops != 2 {
		t.Errorf("suite counters: %+v", summary)
	}
	if summary.ExecutedTests != 9 || summary.SuccessTests != 7 ||
		summary.FailedTests != 2 || summary.NotExecutedTests != 3 {
		t.Errorf("stats counters: %+v", summary)
	}
	if len(report.TestResults) != len(sampleEntries()) {
		t.Errorf("TestResults: got %d, want %d", len(report.TestResults), len(sampleEntries()))
	}
}

func TestGenerateReportUnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "test_report.json")
	err := generateReport(path, time.Now(), time.Second, &Stats{}, 0, 0, 1, nil)
	if err == nil {
		t.Error("expected an error when the report file cannot be written")
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()

	fn()
	os.Stdout = original
	w.Close()
	output := <-done
	r.Close()
	return output
}

func TestStatsPrintSummary(t *testing.T) {
	stats := &Stats{
		SuccessTests:           7,
		FailedTests:            2,
		ExecutedTests:          9,
		SkippedTests:           3,
		ScheduledTests:         12,
		TotalRoundTripTime:     1500 * time.Millisecond,
		TotalMarshallingTime:   20 * time.Millisecond,
		TotalUnmarshallingTime: 30 * time.Millisecond,
		TotalComparisonCount:   11,
		TotalEqualCount:        9,
	}
	start := time.Date(2026, 9, 11, 10, 30, 0, 0, time.UTC)

	output := captureStdout(t, func() {
		stats.PrintSummary(start, 90*time.Second, 2, 5, 42)
	})

	for _, want := range []string{
		"2026-09-11 10:30:00",
		"Total HTTP round-trip time:   1.5s",
		"Total Comparison count:       11",
		"Total Equal count:            9",
		"Test session duration:        1m30s",
		"Test session iterations:      2",
		"Test suite total APIs:        5",
		"Test suite total tests:       42",
		"Number of skipped tests:      3",
		"Number of selected tests:     12",
		"Number of executed tests:     9",
		"Number of success tests:      7",
		"Number of failed tests:       2",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("summary is missing %q:\n%s", want, output)
		}
	}
}
