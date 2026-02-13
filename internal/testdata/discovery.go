package testdata

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var numberRe = regexp.MustCompile(`\d+`)

// ExtractNumber extracts the first number from a filename for sorting.
func ExtractNumber(filename string) int {
	match := numberRe.FindString(filename)
	if match != "" {
		num, _ := strconv.Atoi(match)
		return num
	}
	return 0
}

// validTestExtensions lists the file extensions accepted as test fixtures.
var validTestExtensions = map[string]bool{
	".json": true,
	".tar":  true,
	".zip":  true,
	".gzip": true,
}

// DiscoverTests scans the test directory and returns all test cases with global numbering.
// The global numbering matches v1 exactly: alphabetical API dirs, numeric sort within API,
// global counter increments for every valid test file regardless of filtering.
func DiscoverTests(jsonDir, resultsDir string) (*DiscoveryResult, error) {
	dirs, err := os.ReadDir(jsonDir)
	if err != nil {
		return nil, fmt.Errorf("error reading directory %s: %w", jsonDir, err)
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name() < dirs[j].Name()
	})

	result := &DiscoveryResult{}
	globalTestNumber := 0

	for _, dirEntry := range dirs {
		apiName := dirEntry.Name()

		// Skip results folder and hidden folders
		if apiName == resultsDir || strings.HasPrefix(apiName, ".") {
			continue
		}

		testDir := filepath.Join(jsonDir, apiName)
		info, err := os.Stat(testDir)
		if err != nil || !info.IsDir() {
			continue
		}

		result.TotalAPIs++

		testEntries, err := os.ReadDir(testDir)
		if err != nil {
			continue
		}

		// Sort test files by number (matching v1 extractNumber sort)
		sort.Slice(testEntries, func(i, j int) bool {
			return ExtractNumber(testEntries[i].Name()) < ExtractNumber(testEntries[j].Name())
		})

		for _, testEntry := range testEntries {
			testName := testEntry.Name()

			if !strings.HasPrefix(testName, "test_") {
				continue
			}

			ext := filepath.Ext(testName)
			if !validTestExtensions[ext] {
				continue
			}

			globalTestNumber++

			result.Tests = append(result.Tests, TestCase{
				Name:    filepath.Join(apiName, testName),
				Number:  globalTestNumber,
				APIName: apiName,
			})
		}
	}

	result.TotalTests = globalTestNumber
	return result, nil
}
