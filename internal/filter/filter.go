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

	return f
}

// IsSkipped determines if a test should be skipped.
// This matches v1 isSkipped() exactly.
func (f *TestFilter) IsSkipped(currAPI, testName string, globalTestNumber int) bool {
	apiFullName := f.cfg.Net + "/" + currAPI
	apiFullTestName := f.cfg.Net + "/" + testName

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
// tcLatest reflects the metadata.latest field from the test fixture.
func (f *TestFilter) APIUnderTest(currAPI, testName string, tcLatest bool) bool {
	if len(f.testingWithList) == 0 && len(f.testingAPIsList) == 0 && !f.cfg.TestsOnLatestBlock {
		return true
	}

	if len(f.testingWithList) > 0 {
		for _, test := range f.testingWithList {
			if strings.Contains(currAPI, test) {
				if f.cfg.TestsOnLatestBlock {
					return tcLatest
				}
				return true
			}
		}
		return false
	}

	if len(f.testingAPIsList) > 0 {
		for _, test := range f.testingAPIsList {
			if test == currAPI {
				if f.cfg.TestsOnLatestBlock {
					return tcLatest
				}
				return true
			}
		}
		return false
	}

	if f.cfg.TestsOnLatestBlock {
		return tcLatest
	}

	return false
}

// VerifyInLatestList reports whether a test is a latest-block test.
func (f *TestFilter) VerifyInLatestList(tcLatest bool) bool {
	return f.cfg.TestsOnLatestBlock && tcLatest
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
