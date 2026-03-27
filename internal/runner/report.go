package runner

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// reportEntry mirrors the Python test_results list entry.
type reportEntry struct {
	TestNumber    int    `json:"test_number"`
	TransportType string `json:"transport_type"`
	TestName      string `json:"test_name"`
	Result        string `json:"result"`
	ErrorMessage  any    `json:"error_message"`
}

type reportSummary struct {
	StartTime        string `json:"start_time"`
	TimeElapsed      string `json:"time_elapsed"`
	AvailableTests   int    `json:"available_tests"`
	AvailableAPIs    int    `json:"available_tested_api"`
	NumberOfLoops    int    `json:"number_of_loops"`
	ExecutedTests    int    `json:"executed_tests"`
	NotExecutedTests int    `json:"not_executed_tests"`
	SuccessTests     int    `json:"success_tests"`
	FailedTests      int    `json:"failed_tests"`
}

type testReport struct {
	Summary     reportSummary `json:"summary"`
	TestResults []reportEntry `json:"test_results"`
}

// apiSummary holds per-API aggregated counts for the CSV report.
type apiSummary struct {
	APIName     string
	TestsDone   int
	TestsFailed int
}

// generateCSVReport writes a summary report with one row per API and an empty NOTA column.
// The format is determined by the file extension: .csv for CSV, anything else for aligned text.
func generateCSVReport(filename string, entries []reportEntry) error {
	// Aggregate by API name (extracted from "api_name/test_NN.json")
	order := []string{}
	byAPI := map[string]*apiSummary{}
	for _, e := range entries {
		if e.Result == "SKIPPED" {
			continue
		}
		apiName := e.TestName
		if idx := strings.Index(e.TestName, "/"); idx >= 0 {
			apiName = e.TestName[:idx]
		}
		s, exists := byAPI[apiName]
		if !exists {
			s = &apiSummary{APIName: apiName}
			byAPI[apiName] = s
			order = append(order, apiName)
		}
		s.TestsDone++
		if e.Result == "FAILED" {
			s.TestsFailed++
		}
	}
	slices.Sort(order)

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create report file: %w", err)
	}
	defer f.Close()

	var totalDone, totalFailed int
	for _, name := range order {
		s := byAPI[name]
		totalDone += s.TestsDone
		totalFailed += s.TestsFailed
	}

	if strings.ToLower(filepath.Ext(filename)) == ".csv" {
		w := csv.NewWriter(f)
		_ = w.Write([]string{"#", "api_name", "tests_done", "tests_failed", "NOTA"})
		for i, name := range order {
			s := byAPI[name]
			_ = w.Write([]string{
				strconv.Itoa(i + 1),
				s.APIName,
				strconv.Itoa(s.TestsDone),
				strconv.Itoa(s.TestsFailed),
				"",
			})
		}
		_ = w.Write([]string{"", "TOTAL", strconv.Itoa(totalDone), strconv.Itoa(totalFailed), ""})
		w.Flush()
		return w.Error()
	}

	// Aligned text table
	tw := tabwriter.NewWriter(f, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tapi_name\ttests_done\ttests_failed\tNOTA")
	fmt.Fprintln(tw, "-\t--------\t----------\t------------\t----")
	for i, name := range order {
		s := byAPI[name]
		fmt.Fprintf(tw, "%d\t%s\t%d\t%d\t\n", i+1, s.APIName, s.TestsDone, s.TestsFailed)
	}
	fmt.Fprintln(tw, "-\t--------\t----------\t------------\t----")
	fmt.Fprintf(tw, "\tTOTAL\t%d\t%d\t\n", totalDone, totalFailed)
	return tw.Flush()
}

func generateReport(filename string, startTime time.Time, elapsed time.Duration, s *Stats, totalTests, totalAPIs, loopNumber int, entries []reportEntry) error {
	report := testReport{
		Summary: reportSummary{
			StartTime:        startTime.Format(time.RFC3339),
			TimeElapsed:      elapsed.String(),
			AvailableTests:   totalTests,
			AvailableAPIs:    totalAPIs,
			NumberOfLoops:    loopNumber,
			ExecutedTests:    s.ExecutedTests,
			NotExecutedTests: s.SkippedTests,
			SuccessTests:     s.SuccessTests,
			FailedTests:      s.FailedTests,
		},
		TestResults: entries,
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}
	return nil
}
