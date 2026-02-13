package filter

import (
	"strconv"
	"strings"
)

// FilterConfig provides the configuration fields needed by TestFilter.
// This avoids a direct dependency on the config package.
type FilterConfig struct {
	Net                string
	ReqTestNum         int
	TestingAPIs        string
	TestingAPIsWith    string
	ExcludeAPIList     string
	ExcludeTestList    string
	TestsOnLatestBlock bool
	DoNotCompareError  bool
}

// TestFilter handles all test filtering logic, matching v1 behavior exactly.
// Pre-computes split lists and sets at construction time for zero-alloc lookups.
type TestFilter struct {
	cfg FilterConfig

	// Pre-split lists (computed once at construction)
	excludeAPIs     []string
	excludeTestSet  map[int]struct{} // O(1) lookup by test number
	testingAPIsList []string
	testingWithList []string
	useDefaultSkip  bool
}

// New creates a new TestFilter from the given configuration.
// Pre-splits comma-separated lists and builds lookup sets.
func New(cfg FilterConfig) *TestFilter {
	f := &TestFilter{cfg: cfg}

	if cfg.ExcludeAPIList != "" {
		f.excludeAPIs = strings.Split(cfg.ExcludeAPIList, ",")
	}

	if cfg.ExcludeTestList != "" {
		parts := strings.Split(cfg.ExcludeTestList, ",")
		f.excludeTestSet = make(map[int]struct{}, len(parts))
		for _, p := range parts {
			if n, err := strconv.Atoi(p); err == nil {
				f.excludeTestSet[n] = struct{}{}
			}
		}
	}

	if cfg.TestingAPIs != "" {
		f.testingAPIsList = strings.Split(cfg.TestingAPIs, ",")
	}

	if cfg.TestingAPIsWith != "" {
		f.testingWithList = strings.Split(cfg.TestingAPIsWith, ",")
	}

	// Default skip list applies when no specific test/API is requested and no exclude filters are set.
	f.useDefaultSkip = (cfg.ReqTestNum == -1 || cfg.TestingAPIs != "" || cfg.TestingAPIsWith != "") &&
		!(cfg.ReqTestNum != -1 && (cfg.TestingAPIs != "" || cfg.TestingAPIsWith != "")) &&
		cfg.ExcludeAPIList == "" && cfg.ExcludeTestList == ""

	return f
}

// IsSkipped determines if a test should be skipped.
// This matches v1 isSkipped() exactly.
func (f *TestFilter) IsSkipped(currAPI, testName string, globalTestNumber int) bool {
	apiFullName := f.cfg.Net + "/" + currAPI
	apiFullTestName := f.cfg.Net + "/" + testName

	if f.useDefaultSkip {
		for _, currTestName := range apiNotCompared {
			if strings.Contains(apiFullName, currTestName) {
				return true
			}
		}
	}

	for _, excludeAPI := range f.excludeAPIs {
		if strings.Contains(apiFullName, excludeAPI) || strings.Contains(apiFullTestName, excludeAPI) {
			return true
		}
	}

	if f.excludeTestSet != nil {
		if _, excluded := f.excludeTestSet[globalTestNumber]; excluded {
			return true
		}
	}

	return false
}

// APIUnderTest determines if a test should run based on API/pattern/latest filters.
// This matches v1 apiUnderTest() exactly.
func (f *TestFilter) APIUnderTest(currAPI, testName string) bool {
	if len(f.testingWithList) == 0 && len(f.testingAPIsList) == 0 && !f.cfg.TestsOnLatestBlock {
		return true
	}

	if len(f.testingWithList) > 0 {
		for _, test := range f.testingWithList {
			if strings.Contains(currAPI, test) {
				if f.cfg.TestsOnLatestBlock && f.VerifyInLatestList(testName) {
					return true
				}
				if f.cfg.TestsOnLatestBlock {
					return false
				}
				return true
			}
		}
		return false
	}

	if len(f.testingAPIsList) > 0 {
		for _, test := range f.testingAPIsList {
			if test == currAPI {
				if f.cfg.TestsOnLatestBlock && f.VerifyInLatestList(testName) {
					return true
				}
				if f.cfg.TestsOnLatestBlock {
					return false
				}
				return true
			}
		}
		return false
	}

	if f.cfg.TestsOnLatestBlock {
		return f.VerifyInLatestList(testName)
	}

	return false
}

// VerifyInLatestList checks if a test is in the latest block list.
// This matches v1 verifyInLatestList() exactly.
func (f *TestFilter) VerifyInLatestList(testName string) bool {
	apiFullTestName := f.cfg.Net + "/" + testName
	if f.cfg.TestsOnLatestBlock {
		for _, currTest := range testsOnLatest {
			if strings.Contains(apiFullTestName, currTest) {
				return true
			}
		}
	}
	return false
}

// CheckTestNameForNumber checks if a test filename like "test_01.json" matches a requested
// test number. Zero-alloc: extracts the number after the last "_" without regex.
func CheckTestNameForNumber(testName string, reqTestNumber int) bool {
	if reqTestNumber == -1 {
		return true
	}
	idx := strings.LastIndex(testName, "_")
	if idx < 0 || idx+1 >= len(testName) {
		return false
	}
	numStr := testName[idx+1:]
	end := 0
	for end < len(numStr) && numStr[end] >= '0' && numStr[end] <= '9' {
		end++
	}
	if end == 0 {
		return false
	}
	n, err := strconv.Atoi(numStr[:end])
	if err != nil {
		return false
	}
	return n == reqTestNumber
}

// ShouldCompareMessage checks if the message field should be compared for a given test.
func (f *TestFilter) ShouldCompareMessage(testPath string) bool {
	fullPath := f.cfg.Net + "/" + testPath
	for _, pattern := range testsNotComparedMessage {
		if pattern == fullPath {
			return false
		}
	}
	return true
}

// ShouldCompareError checks if the error field should be compared for a given test.
func (f *TestFilter) ShouldCompareError(testPath string) bool {
	if f.cfg.DoNotCompareError {
		return false
	}
	fullPath := f.cfg.Net + "/" + testPath
	for _, pattern := range testsNotComparedError {
		if pattern == fullPath {
			return false
		}
	}
	return true
}
