package runner

import (
	"testing"
	"time"

	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

func BenchmarkStats_AddSuccess(b *testing.B) {
	metrics := testdata.TestMetrics{
		RoundTripTime:     100 * time.Millisecond,
		MarshallingTime:   10 * time.Millisecond,
		UnmarshallingTime: 20 * time.Millisecond,
		ComparisonCount:   1,
		EqualCount:        1,
	}
	b.ResetTimer()
	for b.Loop() {
		s := &Stats{}
		s.AddSuccess(metrics)
	}
}

func BenchmarkShouldRunTest_NoFilters(b *testing.B) {
	cfg := config.NewConfig()
	b.ResetTimer()
	for b.Loop() {
		ShouldRunTest(cfg, "test_01.json", 1)
	}
}

func BenchmarkShouldRunTest_WithTestNumber(b *testing.B) {
	cfg := config.NewConfig()
	cfg.ReqTestNum = 5
	b.ResetTimer()
	for b.Loop() {
		ShouldRunTest(cfg, "test_05.json", 5)
	}
}

func BenchmarkCheckTestNameForNumber(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {
		checkTestNameForNumber("test_01.json", 1)
	}
}

func BenchmarkIsStartTestReached(b *testing.B) {
	cfg := config.NewConfig()
	cfg.StartTest = "100"
	cfg.UpdateDirs()
	b.ResetTimer()
	for b.Loop() {
		IsStartTestReached(cfg, 50)
	}
}
