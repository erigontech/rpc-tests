package testdata

import "testing"

func BenchmarkExtractNumber(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExtractNumber("test_01.json")
		ExtractNumber("test_10.tar.gz")
		ExtractNumber("test_99.tar.bz2")
	}
}

func BenchmarkIsArchive(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsArchive("test_01.json")
		IsArchive("test_01.tar.gz")
		IsArchive("test_01.tar.bz2")
	}
}
