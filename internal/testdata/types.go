package testdata

import (
	"time"

	jsoniter "github.com/json-iterator/go"
)

// TestCase represents a discovered test file with its global numbering.
type TestCase struct {
	Name          string // Relative path: "api_name/test_NN.json"
	Number        int    // Global test number (1-based, across all APIs)
	APIName       string // API directory name
	TransportType string // Assigned at scheduling time
}

// TestDescriptor is a scheduled test sent to workers.
type TestDescriptor struct {
	Name          string
	Number        int
	TransportType string
	Index         int // Position in scheduled order (for ordered output)
}

// TestResult holds a test outcome and its descriptor.
type TestResult struct {
	Outcome TestOutcome
	Test    *TestDescriptor
}

// ErrorDetails holds structured failure information for the JSON report.
type ErrorDetails struct {
	Message          string `json:"message,omitempty"`
	Target           string `json:"target,omitempty"`
	ActualResponse   any    `json:"actual_response,omitempty"`
	ExpectedResponse any    `json:"expected_response,omitempty"`
	Diff             string `json:"diff,omitempty"`
	Request          any    `json:"request,omitempty"`
}

// TestOutcome holds the result of executing a single test.
type TestOutcome struct {
	Success      bool
	Error        error
	ColoredDiff  string
	Metrics      TestMetrics
	ErrorDetails *ErrorDetails
}

// TestMetrics tracks timing and comparison statistics for a single test.
type TestMetrics struct {
	RoundTripTime     time.Duration
	MarshallingTime   time.Duration
	UnmarshallingTime time.Duration
	ComparisonCount   int
	EqualCount        int
}

// JsonRpcTest holds test-level information (identifier, description).
type JsonRpcTest struct {
	Identifier  string `json:"id"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
}

// JsonRpcCommand represents a single JSON-RPC command in a test fixture.
type JsonRpcCommand struct {
	Request  jsoniter.RawMessage `json:"request"`
	Response any                 `json:"response"`
	TestInfo *JsonRpcTest        `json:"test"`
}

// DiscoveryResult holds the results of test discovery.
type DiscoveryResult struct {
	Tests      []TestCase
	TotalAPIs  int
	TotalTests int // Global test count (including non-matching tests)
}
