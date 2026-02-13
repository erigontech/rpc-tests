package perf

import (
	"testing"
	"time"
)

func BenchmarkParseTestSequence(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseTestSequence(DefaultTestSequence)
	}
}

func BenchmarkFormatDuration_Microseconds(b *testing.B) {
	d := 500 * time.Microsecond
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FormatDuration(d)
	}
}

func BenchmarkFormatDuration_Milliseconds(b *testing.B) {
	d := 150 * time.Millisecond
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FormatDuration(d)
	}
}

func BenchmarkFormatDuration_Seconds(b *testing.B) {
	d := 2500 * time.Millisecond
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FormatDuration(d)
	}
}

func BenchmarkCountDigits(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CountDigits(10000)
	}
}

func BenchmarkGetCompressionType(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		getCompressionType("test.tar.gz")
		getCompressionType("test.tar.bz2")
		getCompressionType("test.tar")
	}
}
