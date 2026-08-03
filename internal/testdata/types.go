package testdata

import (
	"fmt"
	"time"

	jsoniter "github.com/json-iterator/go"
)

// TestCase represents a discovered test file with its global numbering.
type TestCase struct {
	Name               string // Relative path: "api_name/test_NN.json"
	Number             int    // Global test number (1-based, across all APIs)
	APIName            string // API directory name
	TransportType      string // Assigned at scheduling time
	Latest             bool
	CommitmentHistory   bool
}

// TestDescriptor is a scheduled test sent to workers.
type TestDescriptor struct {
	Name          string
	Number        int
	TransportType string
	Index         int  // Position in scheduled order (for ordered output)
	Latest        bool // metadata.latest: the request resolves the "latest" tag on the node
}

// TestResult holds a test outcome and its descriptor.
type TestResult struct {
	Outcome TestOutcome
	Test    *TestDescriptor
}

// HeadSnapshot records the head a node was on, as read from that node.
type HeadSnapshot struct {
	Target string `json:"target"`
	Number uint64 `json:"number,omitempty"`
	Hash   string `json:"hash,omitempty"`
	Error  string `json:"error,omitempty"`
}

// String renders the snapshot compactly for log lines.
func (h HeadSnapshot) String() string {
	if h.Error != "" {
		return h.Target + "=<" + h.Error + ">"
	}
	return fmt.Sprintf("%s=%d/%s", h.Target, h.Number, h.Hash)
}

// HeadEvidence holds the heads of both nodes read before and after a latest-block test,
// so a failure can be judged against the head each node actually resolved "latest" to.
type HeadEvidence struct {
	Before []HeadSnapshot `json:"before,omitempty"`
	After  []HeadSnapshot `json:"after,omitempty"`
}

// ErrorDetails holds structured failure information for the JSON report.
type ErrorDetails struct {
	Message          string        `json:"message,omitempty"`
	Target           string        `json:"target,omitempty"`
	ActualResponse   any           `json:"actual_response,omitempty"`
	ExpectedResponse any           `json:"expected_response,omitempty"`
	Diff             string        `json:"diff,omitempty"`
	Request          any           `json:"request,omitempty"`
	Heads            *HeadEvidence `json:"heads,omitempty"`
}

// TestOutcome holds the result of executing a single test.
type TestOutcome struct {
	Success      bool
	Error        error
	ColoredDiff  string
	Metrics      TestMetrics
	ErrorDetails *ErrorDetails
	// Notes carries short diagnostic lines printed next to the test result,
	// e.g. inconclusive attempts discarded because the nodes' heads moved.
	Notes []string
	// Artifacts lists the response/diff files the last attempt may have written,
	// so a discarded attempt can clean up after itself.
	Artifacts []string
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

// TestMetadata holds runner hints embedded in a test fixture.
type TestMetadata struct {
	Latest                   bool     `json:"latest"`
	IgnoreFields             []string `json:"ignoreFields"`
	RequestCommitmentHistory  bool     `json:"erigon.request-commitment-history"`
}

// JsonRpcCommand represents a single JSON-RPC command in a test fixture.
type JsonRpcCommand struct {
	Request  jsoniter.RawMessage `json:"request"`
	Response any                 `json:"response"`
	TestInfo *JsonRpcTest        `json:"test"`
	Metadata *TestMetadata       `json:"metadata"`
}

// DiscoveryResult holds the results of test discovery.
type DiscoveryResult struct {
	Tests      []TestCase
	TotalAPIs  int
	TotalTests int // Global test count (including non-matching tests)
}
