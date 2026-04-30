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

	// engine_ APIs are no longer skipped by default — use -x engine_ to exclude them
	if f.IsSkipped("engine_getClientVersionV1", "engine_getClientVersionV1/test_01.json", 1) {
		t.Error("engine_getClientVersionV1 should not be skipped by default")
	}
	if f.IsSkipped("eth_call", "eth_call/test_01.json", 10) {
		t.Error("eth_call should not be skipped by default")
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

func TestAPIUnderTest_NoFilters(t *testing.T) {
	f := New(defaultCfg())

	if !f.APIUnderTest("eth_call", "eth_call/test_01.json", false) {
		t.Error("without -L, historical tests (tcLatest=false) should run")
	}
	if f.APIUnderTest("eth_call", "eth_call/test_02.json", true) {
		t.Error("without -L, latest-block tests (tcLatest=true) should not run")
	}
}

func TestAPIUnderTest_ExactAPI(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestingAPIs = "eth_call"
	f := New(cfg)

	if !f.APIUnderTest("eth_call", "eth_call/test_01.json", false) {
		t.Error("eth_call historical test should match exact API filter")
	}
	if f.APIUnderTest("eth_call", "eth_call/test_02.json", true) {
		t.Error("eth_call latest test should not run without -L")
	}
	if f.APIUnderTest("eth_getBalance", "eth_getBalance/test_01.json", false) {
		t.Error("eth_getBalance should not match exact API filter for eth_call")
	}
}

func TestAPIUnderTest_MultipleExactAPIs(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestingAPIs = "eth_call,eth_getBalance"
	f := New(cfg)

	if !f.APIUnderTest("eth_call", "eth_call/test_01.json", false) {
		t.Error("eth_call should match")
	}
	if !f.APIUnderTest("eth_getBalance", "eth_getBalance/test_01.json", false) {
		t.Error("eth_getBalance should match")
	}
	if f.APIUnderTest("eth_getCode", "eth_getCode/test_01.json", false) {
		t.Error("eth_getCode should not match")
	}
}

func TestAPIUnderTest_PatternAPI(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestingAPIsWith = "eth_"
	f := New(cfg)

	if !f.APIUnderTest("eth_call", "eth_call/test_01.json", false) {
		t.Error("eth_call historical should match pattern eth_")
	}
	if f.APIUnderTest("eth_call", "eth_call/test_02.json", true) {
		t.Error("eth_call latest test should not run without -L")
	}
	if !f.APIUnderTest("eth_getBalance", "eth_getBalance/test_01.json", false) {
		t.Error("eth_getBalance should match pattern eth_")
	}
	if f.APIUnderTest("trace_call", "trace_call/test_01.json", false) {
		t.Error("trace_call should not match pattern eth_")
	}
}

func TestAPIUnderTest_LatestBlock(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestsOnLatestBlock = true
	f := New(cfg)

	if !f.APIUnderTest("eth_blockNumber", "eth_blockNumber/test_01.json", true) {
		t.Error("tcLatest=true should be included when TestsOnLatestBlock is set")
	}
	if f.APIUnderTest("eth_call", "eth_call/test_01.json", false) {
		t.Error("tcLatest=false should be excluded when TestsOnLatestBlock is set")
	}
	if !f.APIUnderTest("eth_call", "eth_call/test_20.json", true) {
		t.Error("tcLatest=true should be included")
	}
}

func TestAPIUnderTest_PatternWithLatest(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestingAPIsWith = "eth_call"
	cfg.TestsOnLatestBlock = true
	f := New(cfg)

	if !f.APIUnderTest("eth_call", "eth_call/test_20.json", true) {
		t.Error("tcLatest=true matches pattern and is latest")
	}
	if f.APIUnderTest("eth_call", "eth_call/test_01.json", false) {
		t.Error("tcLatest=false matches pattern but is not latest")
	}
}

func TestVerifyInLatestList(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestsOnLatestBlock = true
	f := New(cfg)

	if !f.VerifyInLatestList(true) {
		t.Error("tcLatest=true should be in latest list")
	}
	if f.VerifyInLatestList(false) {
		t.Error("tcLatest=false should NOT be in latest list")
	}
}

func TestVerifyInLatestList_FlagOff(t *testing.T) {
	cfg := defaultCfg()
	cfg.TestsOnLatestBlock = false
	f := New(cfg)

	if f.VerifyInLatestList(true) {
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
