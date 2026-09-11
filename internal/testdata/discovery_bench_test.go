package testdata

import "testing"

func BenchmarkExtractNumber(b *testing.B) {
	b.ResetTimer()
	for range b.N {
		ExtractNumber("test_01.json")
		ExtractNumber("test_10.tar.gz")
		ExtractNumber("test_99.tar.bz2")
	}
}

func BenchmarkIsArchive(b *testing.B) {
	b.ResetTimer()
	for range b.N {
		IsArchive("test_01.json")
		IsArchive("test_01.tar.gz")
		IsArchive("test_01.tar.bz2")
	}
}
