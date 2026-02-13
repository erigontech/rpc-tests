package runner

import (
	"testing"
	"time"

	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

func TestStats_AddSuccess(t *testing.T) {
	s := &Stats{}
	metrics := testdata.TestMetrics{
		RoundTripTime:     100 * time.Millisecond,
		MarshallingTime:   10 * time.Millisecond,
		UnmarshallingTime: 20 * time.Millisecond,
		ComparisonCount:   1,
		EqualCount:        1,
	}

	s.AddSuccess(metrics)
	s.AddSuccess(metrics)

	if s.SuccessTests != 2 {
		t.Errorf("SuccessTests: got %d, want 2", s.SuccessTests)
	}
	if s.ExecutedTests != 2 {
		t.Errorf("ExecutedTests: got %d, want 2", s.ExecutedTests)
	}
	if s.TotalRoundTripTime != 200*time.Millisecond {
		t.Errorf("TotalRoundTripTime: got %v, want 200ms", s.TotalRoundTripTime)
	}
	if s.TotalComparisonCount != 2 {
		t.Errorf("TotalComparisonCount: got %d, want 2", s.TotalComparisonCount)
	}
	if s.TotalEqualCount != 2 {
		t.Errorf("TotalEqualCount: got %d, want 2", s.TotalEqualCount)
	}
}

func TestStats_AddFailure(t *testing.T) {
	s := &Stats{}
	s.AddFailure()
	s.AddFailure()

	if s.FailedTests != 2 {
		t.Errorf("FailedTests: got %d, want 2", s.FailedTests)
	}
	if s.ExecutedTests != 2 {
		t.Errorf("ExecutedTests: got %d, want 2", s.ExecutedTests)
	}
}

func TestMustAtoi(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"abc", 0},
	}

	for _, tt := range tests {
		got := mustAtoi(tt.input)
		if got != tt.want {
			t.Errorf("mustAtoi(%q): got %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestIsStartTestReached(t *testing.T) {
	cfg := config.NewConfig()

	// No start test set
	if !IsStartTestReached(cfg, 1) {
		t.Error("should return true when no start test is set")
	}

	cfg.StartTest = "10"
	cfg.UpdateDirs()
	if IsStartTestReached(cfg, 5) {
		t.Error("test 5 should not be reached when start is 10")
	}
	if !IsStartTestReached(cfg, 10) {
		t.Error("test 10 should be reached when start is 10")
	}
	if !IsStartTestReached(cfg, 15) {
		t.Error("test 15 should be reached when start is 10")
	}
}

func TestShouldRunTest_NoFilters(t *testing.T) {
	cfg := config.NewConfig()
	if !ShouldRunTest(cfg, "test_01.json", 1) {
		t.Error("no filters should run all tests")
	}
}

func TestShouldRunTest_SpecificTestNumber(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ReqTestNum = 5
	if ShouldRunTest(cfg, "test_01.json", 1) {
		t.Error("test 1 should not run when ReqTestNum=5")
	}
	if !ShouldRunTest(cfg, "test_01.json", 5) {
		t.Error("test 5 should run when ReqTestNum=5")
	}
}

func TestShouldRunTest_WithAPIPatternFilter(t *testing.T) {
	cfg := config.NewConfig()
	cfg.TestingAPIsWith = "eth_"
	// When TestingAPIsWith is set but no specific test number, should run
	if !ShouldRunTest(cfg, "test_01.json", 1) {
		t.Error("should run when API pattern matches and no test number filter")
	}
}

func TestShouldRunTest_WithExactAPIFilter(t *testing.T) {
	cfg := config.NewConfig()
	cfg.TestingAPIs = "eth_call"
	if !ShouldRunTest(cfg, "test_01.json", 1) {
		t.Error("should run when exact API matches and no test number filter")
	}
}

func TestCheckTestNameForNumber(t *testing.T) {
	tests := []struct {
		name string
		num  int
		want bool
	}{
		{"test_01.json", 1, true},
		{"test_01.json", 2, false},
		{"test_10.json", 10, true},
		{"test_10.json", 1, false},
		{"test_01.json", -1, true},
	}

	for _, tt := range tests {
		got := checkTestNameForNumber(tt.name, tt.num)
		if got != tt.want {
			t.Errorf("checkTestNameForNumber(%q, %d): got %v, want %v", tt.name, tt.num, got, tt.want)
		}
	}
}
