package runner

import (
	"fmt"
	"time"

	"github.com/erigontech/rpc-tests/internal/testdata"
)

// Stats aggregates metrics and counts across all tests.
type Stats struct {
	SuccessTests   int
	FailedTests    int
	ExecutedTests  int
	SkippedTests   int
	ScheduledTests int

	TotalRoundTripTime     time.Duration
	TotalMarshallingTime   time.Duration
	TotalUnmarshallingTime time.Duration
	TotalComparisonCount   int
	TotalEqualCount        int
}

// AddSuccess records a successful test result.
func (s *Stats) AddSuccess(metrics testdata.TestMetrics) {
	s.SuccessTests++
	s.ExecutedTests++
	s.TotalRoundTripTime += metrics.RoundTripTime
	s.TotalMarshallingTime += metrics.MarshallingTime
	s.TotalUnmarshallingTime += metrics.UnmarshallingTime
	s.TotalComparisonCount += metrics.ComparisonCount
	s.TotalEqualCount += metrics.EqualCount
}

// AddFailure records a failed test result.
func (s *Stats) AddFailure() {
	s.FailedTests++
	s.ExecutedTests++
}

// PrintSummary prints the v1-compatible summary output.
func (s *Stats) PrintSummary(elapsed time.Duration, iterations, totalAPIs, totalTests int) {
	fmt.Println("\n                                                                                                                  ")
	fmt.Printf("Total HTTP round-trip time:   %v\n", s.TotalRoundTripTime)
	fmt.Printf("Total Marshalling time:       %v\n", s.TotalMarshallingTime)
	fmt.Printf("Total Unmarshalling time:     %v\n", s.TotalUnmarshallingTime)
	fmt.Printf("Total Comparison count:       %v\n", s.TotalComparisonCount)
	fmt.Printf("Total Equal count:            %v\n", s.TotalEqualCount)
	fmt.Printf("Test session duration:        %v\n", elapsed)
	fmt.Printf("Test session iterations:      %d\n", iterations)
	fmt.Printf("Test suite total APIs:        %d\n", totalAPIs)
	fmt.Printf("Test suite total tests:       %d\n", totalTests)
	fmt.Printf("Number of skipped tests:      %d\n", s.SkippedTests)
	fmt.Printf("Number of selected tests:     %d\n", s.ScheduledTests)
	fmt.Printf("Number of executed tests:     %d\n", s.ExecutedTests)
	fmt.Printf("Number of success tests:      %d\n", s.SuccessTests)
	fmt.Printf("Number of failed tests:       %d\n", s.FailedTests)
}
