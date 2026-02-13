package perf

import (
	"testing"
	"time"
)

func BenchmarkParseTestSequence(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {
		ParseTestSequence(DefaultTestSequence)
	}
}

func BenchmarkFormatDuration_Microseconds(b *testing.B) {
	d := 500 * time.Microsecond
	b.ResetTimer()
	for b.Loop() {
		FormatDuration(d)
	}
}

func BenchmarkFormatDuration_Milliseconds(b *testing.B) {
	d := 150 * time.Millisecond
	b.ResetTimer()
	for b.Loop() {
		FormatDuration(d)
	}
}

func BenchmarkFormatDuration_Seconds(b *testing.B) {
	d := 2500 * time.Millisecond
	b.ResetTimer()
	for b.Loop() {
		FormatDuration(d)
	}
}

func BenchmarkCountDigits(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {
		CountDigits(10000)
	}
}

func BenchmarkGetCompressionType(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {
		getCompressionType("test.tar.gz")
		getCompressionType("test.tar.bz2")
		getCompressionType("test.tar")
	}
}
