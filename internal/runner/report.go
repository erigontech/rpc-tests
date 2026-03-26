package runner

import (
	"encoding/json"
	"fmt"
	"os"
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
	Summary     reportSummary  `json:"summary"`
	TestResults []reportEntry  `json:"test_results"`
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
