package filter

import "testing"

func BenchmarkAPIUnderTest_NoFilters(b *testing.B) {
	f := New(FilterConfig{Net: "mainnet", ReqTestNum: -1})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.APIUnderTest("eth_call", "eth_call/test_01.json")
	}
}

func BenchmarkAPIUnderTest_WithExactAPI(b *testing.B) {
	f := New(FilterConfig{Net: "mainnet", ReqTestNum: -1, TestingAPIs: "eth_call"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.APIUnderTest("eth_call", "eth_call/test_01.json")
	}
}

func BenchmarkAPIUnderTest_WithPattern(b *testing.B) {
	f := New(FilterConfig{Net: "mainnet", ReqTestNum: -1, TestingAPIsWith: "eth_"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.APIUnderTest("eth_call", "eth_call/test_01.json")
	}
}

func BenchmarkAPIUnderTest_WithExclude(b *testing.B) {
	f := New(FilterConfig{Net: "mainnet", ReqTestNum: -1, ExcludeAPIList: "eth_call,eth_getBalance,debug_traceCall"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.APIUnderTest("eth_getLogs", "eth_getLogs/test_01.json")
	}
}

func BenchmarkIsSkipped_DefaultList(b *testing.B) {
	f := New(FilterConfig{Net: "mainnet", ReqTestNum: -1})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.IsSkipped("eth_call", "eth_call/test_01.json", 1)
	}
}

func BenchmarkIsSkipped_LatestBlock(b *testing.B) {
	f := New(FilterConfig{Net: "mainnet", ReqTestNum: -1, TestsOnLatestBlock: true})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.IsSkipped("eth_call", "eth_call/test_01.json", 1)
	}
}

func BenchmarkVerifyInLatestList(b *testing.B) {
	f := New(FilterConfig{Net: "mainnet", ReqTestNum: -1})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.VerifyInLatestList("eth_getBlockByNumber/test_01.json")
	}
}
