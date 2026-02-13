package testdata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractNumber(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"test_01.json", 1},
		{"test_10.json", 10},
		{"test_100.tar", 100},
		{"test_001.gzip", 1},
		{"no_number.json", 0},
		{"", 0},
	}

	for _, tt := range tests {
		got := ExtractNumber(tt.input)
		if got != tt.want {
			t.Errorf("ExtractNumber(%q): got %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestIsArchive(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"test_01.json", false},
		{"test_01.tar", true},
		{"test_01.gzip", true},
		{"test_01.tar.gz", true},
		{"test_01.tar.bz2", true},
	}

	for _, tt := range tests {
		got := IsArchive(tt.input)
		if got != tt.want {
			t.Errorf("IsArchive(%q): got %v, want %v", tt.input, got, tt.want)
		}
	}
}

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create API dirs with test files
	apis := []struct {
		name  string
		tests []string
	}{
		{"eth_call", []string{"test_01.json", "test_02.json", "test_10.json"}},
		{"eth_getBalance", []string{"test_01.json"}},
		{"debug_traceCall", []string{"test_01.json", "test_02.json"}},
	}

	for _, api := range apis {
		apiDir := filepath.Join(dir, api.name)
		if err := os.MkdirAll(apiDir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, test := range api.tests {
			content := `[{"request":{"jsonrpc":"2.0","method":"` + api.name + `","params":[],"id":1},"response":{"jsonrpc":"2.0","id":1,"result":"0x0"}}]`
			if err := os.WriteFile(filepath.Join(apiDir, test), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Add results dir (should be skipped)
	if err := os.MkdirAll(filepath.Join(dir, "results"), 0755); err != nil {
		t.Fatal(err)
	}

	// Add hidden dir (should be skipped)
	if err := os.MkdirAll(filepath.Join(dir, ".hidden"), 0755); err != nil {
		t.Fatal(err)
	}

	// Add a non-test file (should be skipped)
	apiDir := filepath.Join(dir, "eth_call")
	if err := os.WriteFile(filepath.Join(apiDir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestDiscoverTests(t *testing.T) {
	dir := setupTestDir(t)

	result, err := DiscoverTests(dir, "results")
	if err != nil {
		t.Fatalf("DiscoverTests: %v", err)
	}

	if result.TotalAPIs != 3 {
		t.Errorf("TotalAPIs: got %d, want 3", result.TotalAPIs)
	}

	// debug_traceCall(2) + eth_call(3) + eth_getBalance(1) = 6 total tests
	if result.TotalTests != 6 {
		t.Errorf("TotalTests: got %d, want 6", result.TotalTests)
	}

	if len(result.Tests) != 6 {
		t.Fatalf("len(Tests): got %d, want 6", len(result.Tests))
	}

	// Verify alphabetical API order
	expectedAPIs := []string{"debug_traceCall", "debug_traceCall", "eth_call", "eth_call", "eth_call", "eth_getBalance"}
	for i, tc := range result.Tests {
		if tc.APIName != expectedAPIs[i] {
			t.Errorf("test[%d] API: got %q, want %q", i, tc.APIName, expectedAPIs[i])
		}
	}

	// Verify global numbering is sequential
	for i, tc := range result.Tests {
		if tc.Number != i+1 {
			t.Errorf("test[%d] Number: got %d, want %d", i, tc.Number, i+1)
		}
	}
}

func TestDiscoverTests_NumericSort(t *testing.T) {
	dir := setupTestDir(t)

	result, err := DiscoverTests(dir, "results")
	if err != nil {
		t.Fatalf("DiscoverTests: %v", err)
	}

	// eth_call tests should be sorted: test_01, test_02, test_10 (numeric, not lexicographic)
	ethCallTests := []TestCase{}
	for _, tc := range result.Tests {
		if tc.APIName == "eth_call" {
			ethCallTests = append(ethCallTests, tc)
		}
	}

	if len(ethCallTests) != 3 {
		t.Fatalf("eth_call tests: got %d, want 3", len(ethCallTests))
	}

	expectedNames := []string{
		"eth_call/test_01.json",
		"eth_call/test_02.json",
		"eth_call/test_10.json",
	}
	for i, tc := range ethCallTests {
		// Normalize path separator for comparison
		got := filepath.ToSlash(tc.Name)
		if got != expectedNames[i] {
			t.Errorf("eth_call test[%d]: got %q, want %q", i, got, expectedNames[i])
		}
	}
}

func TestDiscoverTests_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	result, err := DiscoverTests(dir, "results")
	if err != nil {
		t.Fatalf("DiscoverTests: %v", err)
	}

	if result.TotalAPIs != 0 {
		t.Errorf("TotalAPIs: got %d, want 0", result.TotalAPIs)
	}
	if result.TotalTests != 0 {
		t.Errorf("TotalTests: got %d, want 0", result.TotalTests)
	}
}

func TestDiscoverTests_NonexistentDir(t *testing.T) {
	_, err := DiscoverTests("/nonexistent/path", "results")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestLoadFixture_JSON(t *testing.T) {
	dir := setupTestDir(t)
	metrics := &TestMetrics{}

	commands, err := LoadFixture(filepath.Join(dir, "eth_call", "test_01.json"), false, metrics)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("commands: got %d, want 1", len(commands))
	}

	if commands[0].Request == nil {
		t.Error("request should not be nil")
	}
	if commands[0].Response == nil {
		t.Error("response should not be nil")
	}
	if metrics.UnmarshallingTime == 0 {
		t.Error("UnmarshallingTime should be > 0")
	}
}

func TestLoadFixture_FileNotFound(t *testing.T) {
	metrics := &TestMetrics{}
	_, err := LoadFixture("/nonexistent/path.json", false, metrics)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadFixture_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	metrics := &TestMetrics{}
	_, err := LoadFixture(path, false, metrics)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
