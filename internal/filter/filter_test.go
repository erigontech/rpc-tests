package filter

import (
	"testing"
)

func defaultCfg() FilterConfig {
	return FilterConfig{
		Net:        "mainnet",
		ReqTestNum: -1,
	}
}

func TestIsSkipped_DefaultList(t *testing.T) {
	f := New(defaultCfg())

	// engine_ APIs should be skipped by default
	if !f.IsSkipped("engine_getClientVersionV1", "engine_getClientVersionV1/test_01.json", 1) {
		t.Error("engine_getClientVersionV1 should be skipped by default")
	}
	if !f.IsSkipped("engine_exchangeCapabilities", "engine_exchangeCapabilities/test_01.json", 2) {
		t.Error("engine_ APIs should be skipped by default")
	}
	if !f.IsSkipped("trace_rawTransaction", "trace_rawTransaction/test_01.json", 3) {
		t.Error("trace_rawTransaction should be skipped by default")
	}

	// Normal API should not be skipped
	if f.IsSkipped("eth_call", "eth_call/test_01.json", 10) {
		t.Error("eth_call should not be skipped by default")
	}
}

func TestIsSkipped_DefaultListDisabledByExcludeAPI(t *testing.T) {
	cfg := defaultCfg()
	cfg.ExcludeAPIList = "eth_getLogs"
	f := New(cfg)

	// When ExcludeAPIList is set, the default skip list is NOT applied
	if f.IsSkipped("engine_getClientVersionV1", "engine_getClientVersionV1/test_01.json", 1) {
		t.Error("default skip list should be disabled when ExcludeAPIList is set")
	}

	// But the explicit exclude should work
	if !f.IsSkipped("eth_getLogs", "eth_getLogs/test_01.json", 10) {
		t.Error("eth_getLogs should be excluded by ExcludeAPIList")
	}
}

func TestIsSkipped_ExcludeTestList(t *testing.T) {
	cfg := defaultCfg()
	cfg.ExcludeTestList = "5,10,15"
	f := New(cfg)

	if !f.IsSkipped("eth_call", "eth_call/test_01.json", 5) {
		t.Error("test 5 should be excluded")
	}
	if !f.IsSkipped("eth_call", "eth_call/test_01.json", 10) {
		t.Error("test 10 should be excluded")
	}
	if f.IsSkipped("eth_call", "eth_call/test_01.json", 7) {
		t.Error("test 7 should not be excluded")
	}
}

func TestIsSkipped_ExcludeAPIPattern(t *testing.T) {
	cfg := defaultCfg()
	cfg.ExcludeAPIList = "eth_getLogs/test_01,trace_rawTransaction"
	f := New(cfg)

	if !f.IsSkipped("eth_getLogs", "eth_getLogs/test_01.json", 1) {
		t.Error("eth_getLogs/test_01 should be excluded")
	}
	if f.IsSkipped("eth_getLogs", "eth_getLogs/test_02.json", 2) {
		t.Error("eth_getLogs/test_02 should not be excluded")
	}
	if !f.IsSkipped("trace_rawTransaction", "trace_rawTransaction/test_01.json", 3) {
		t.Error("trace_rawTransaction should be excluded")
	}
}

func TestIsSkipped_DefaultListDisabledByReqTestAndAPI(t *testing.T) {
	// When both ReqTestNum and TestingAPIs are set, the v1 condition evaluates to false
	// so the default skip list is NOT applied (the XOR-like condition excludes this combo)
	cfg := defaultCfg()
	cfg.ReqTestNum = 5
	cfg.TestingAPIs = "engine_getClientVersionV1"
	f := New(cfg)

	if f.IsSkipped("engine_getClientVersionV1", "engine_getClientVersionV1/test_01.json", 5) {
		t.Error("default skip list should NOT apply when both ReqTestNum and TestingAPIs are set")
	}
}

func TestAPIUnderTest_NoFilters(t *testing.T) {
	f := New(defaultCfg())

	if !f.APIUnderTest("eth_call", "eth_call/test_01.json") {
		t.Error("with no filters, all APIs should be under test")
	}
}

func TestAPIUnderTest_ExactAPI(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestingAPIs = "eth_call"
	f := New(cfg)

	if !f.APIUnderTest("eth_call", "eth_call/test_01.json") {
		t.Error("eth_call should match exact API filter")
	}
	if f.APIUnderTest("eth_getBalance", "eth_getBalance/test_01.json") {
		t.Error("eth_getBalance should not match exact API filter for eth_call")
	}
}

func TestAPIUnderTest_MultipleExactAPIs(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestingAPIs = "eth_call,eth_getBalance"
	f := New(cfg)

	if !f.APIUnderTest("eth_call", "eth_call/test_01.json") {
		t.Error("eth_call should match")
	}
	if !f.APIUnderTest("eth_getBalance", "eth_getBalance/test_01.json") {
		t.Error("eth_getBalance should match")
	}
	if f.APIUnderTest("eth_getCode", "eth_getCode/test_01.json") {
		t.Error("eth_getCode should not match")
	}
}

func TestAPIUnderTest_PatternAPI(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestingAPIsWith = "eth_"
	f := New(cfg)

	if !f.APIUnderTest("eth_call", "eth_call/test_01.json") {
		t.Error("eth_call should match pattern eth_")
	}
	if !f.APIUnderTest("eth_getBalance", "eth_getBalance/test_01.json") {
		t.Error("eth_getBalance should match pattern eth_")
	}
	if f.APIUnderTest("trace_call", "trace_call/test_01.json") {
		t.Error("trace_call should not match pattern eth_")
	}
}

func TestAPIUnderTest_LatestBlock(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestsOnLatestBlock = true
	f := New(cfg)

	if !f.APIUnderTest("eth_blockNumber", "eth_blockNumber/test_01.json") {
		t.Error("eth_blockNumber is on latest list")
	}
	if f.APIUnderTest("eth_call", "eth_call/test_01.json") {
		t.Error("eth_call/test_01 is NOT on latest list")
	}
	if !f.APIUnderTest("eth_call", "eth_call/test_20.json") {
		t.Error("eth_call/test_20 IS on latest list")
	}
}

func TestAPIUnderTest_PatternWithLatest(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestingAPIsWith = "eth_call"
	cfg.TestsOnLatestBlock = true
	f := New(cfg)

	// eth_call/test_20.json is on the latest list
	if !f.APIUnderTest("eth_call", "eth_call/test_20.json") {
		t.Error("eth_call/test_20 matches pattern and is on latest list")
	}
	// eth_call/test_01.json is NOT on the latest list
	if f.APIUnderTest("eth_call", "eth_call/test_01.json") {
		t.Error("eth_call/test_01 matches pattern but is NOT on latest list")
	}
}

func TestVerifyInLatestList(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestsOnLatestBlock = true
	f := New(cfg)

	if !f.VerifyInLatestList("eth_blockNumber/test_01.json") {
		t.Error("eth_blockNumber should be in latest list")
	}
	if !f.VerifyInLatestList("eth_gasPrice/test_01.json") {
		t.Error("eth_gasPrice should be in latest list")
	}
	if f.VerifyInLatestList("eth_call/test_01.json") {
		t.Error("eth_call/test_01 should NOT be in latest list")
	}
}

func TestVerifyInLatestList_FlagOff(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestsOnLatestBlock = false
	f := New(cfg)

	if f.VerifyInLatestList("eth_blockNumber/test_01.json") {
		t.Error("should return false when flag is off")
	}
}

func TestCheckTestNameForNumber(t *testing.T) {
	tests := []struct {
		name   string
		num    int
		expect bool
	}{
		{"test_01.json", 1, true},
		{"test_01.json", 2, false},
		{"test_10.json", 10, true},
		{"test_10.json", 1, false},
		{"test_001.json", 1, true},
		{"test_100.json", 10, false},
		{"test_100.json", 100, true},
		{"test_01.tar", 1, true},
		{"any_name", -1, true},
	}

	for _, tt := range tests {
		got := CheckTestNameForNumber(tt.name, tt.num)
		if got != tt.expect {
			t.Errorf("CheckTestNameForNumber(%q, %d): got %v, want %v", tt.name, tt.num, got, tt.expect)
		}
	}
}

func TestShouldCompareError_GlobalFlag(t *testing.T) {
	cfg := defaultCfg()
	cfg.DoNotCompareError = true
	f := New(cfg)

	if f.ShouldCompareError("eth_call/test_01.json") {
		t.Error("should not compare error when global flag is set")
	}
}

func TestShouldCompareError_Default(t *testing.T) {
	f := New(defaultCfg())

	if !f.ShouldCompareError("eth_call/test_01.json") {
		t.Error("should compare error by default")
	}
}

func TestShouldCompareMessage_Default(t *testing.T) {
	f := New(defaultCfg())

	if !f.ShouldCompareMessage("eth_call/test_01.json") {
		t.Error("should compare message by default")
	}
}

func TestTestsOnLatestList_Count(t *testing.T) {
	// Verify the list has the expected number of entries from v1
	if len(testsOnLatest) < 100 {
		t.Errorf("testsOnLatest has %d entries, expected at least 100", len(testsOnLatest))
	}
}
