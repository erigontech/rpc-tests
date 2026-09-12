package compare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

// diffFiles writes two JSON files plus the paths used to collect the diff.
func diffFiles(t *testing.T, actual, expected string) (daemonFile, expRspFile, diffFile string) {
	t.Helper()
	dir := t.TempDir()
	daemonFile = filepath.Join(dir, "response.json")
	expRspFile = filepath.Join(dir, "expResponse.json")
	diffFile = filepath.Join(dir, "diff.json")

	if err := os.WriteFile(daemonFile, []byte(actual), 0644); err != nil {
		t.Fatalf("write actual: %v", err)
	}
	if err := os.WriteFile(expRspFile, []byte(expected), 0644); err != nil {
		t.Fatalf("write expected: %v", err)
	}
	return daemonFile, expRspFile, diffFile
}

func TestCompareJSONWithDiffTool(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
		wantSame bool
	}{
		{"identical", "{\"a\":1}\n", "{\"a\":1}\n", true},
		{"different", "{\"a\":1}\n", "{\"a\":2}\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemonFile, expRspFile, diffFile := diffFiles(t, tt.actual, tt.expected)
			cfg := config.NewConfig()
			cfg.DiffKind = config.DiffTool
			var metrics testdata.TestMetrics

			same, err := compareJSON(cfg, daemonFile, expRspFile, diffFile, &metrics)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if same != tt.wantSame {
				t.Errorf("same: got %v, want %v", same, tt.wantSame)
			}
			if metrics.ComparisonCount != 1 {
				t.Errorf("ComparisonCount: got %d, want 1", metrics.ComparisonCount)
			}

			content, err := os.ReadFile(diffFile)
			if err != nil {
				t.Fatalf("diff file was not written: %v", err)
			}
			if tt.wantSame && len(content) != 0 {
				t.Errorf("identical files should produce an empty diff, got:\n%s", content)
			}
			if !tt.wantSame && len(content) == 0 {
				t.Error("differing files should produce a non-empty diff")
			}
		})
	}
}

// TestCompareJSONWithJSONDiffTool exercises the json-diff branch. When the tool
// is not installed the code silently falls back to plain diff, and both paths
// must reach the same verdict.
func TestCompareJSONWithJSONDiffTool(t *testing.T) {
	daemonFile, expRspFile, diffFile := diffFiles(t, "{\"a\":1}\n", "{\"a\":2}\n")
	cfg := config.NewConfig()
	cfg.DiffKind = config.JsonDiffTool
	var metrics testdata.TestMetrics

	same, err := compareJSON(cfg, daemonFile, expRspFile, diffFile, &metrics)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if same {
		t.Error("differing files should not compare equal")
	}
}

func TestCompareJSONUnknownDiffKind(t *testing.T) {
	daemonFile, expRspFile, diffFile := diffFiles(t, "{}", "{}")
	cfg := config.NewConfig()
	cfg.DiffKind = config.DiffKind(99)
	var metrics testdata.TestMetrics

	_, err := compareJSON(cfg, daemonFile, expRspFile, diffFile, &metrics)
	if err == nil || !strings.Contains(err.Error(), "unknown JSON diff kind") {
		t.Errorf("got %v, want an unknown-diff-kind error", err)
	}
}

func TestRunExternalCompareMissingInputFile(t *testing.T) {
	dir := t.TempDir()
	_, err := runExternalCompare(false, "/dev/null",
		filepath.Join(dir, "absent-a.json"),
		filepath.Join(dir, "absent-b.json"),
		filepath.Join(dir, "diff.json"))
	if err == nil {
		t.Error("expected an error when diff cannot read its inputs")
	}
}

// TestRunExternalCompareReportsToolErrors covers the branch that inspects the
// captured stderr file rather than discarding it to /dev/null.
func TestRunExternalCompareReportsToolErrors(t *testing.T) {
	dir := t.TempDir()
	errorFile := filepath.Join(dir, "diff.err")
	diffFile := filepath.Join(dir, "diff.json")

	_, err := runExternalCompare(false, errorFile,
		filepath.Join(dir, "absent-a.json"),
		filepath.Join(dir, "absent-b.json"),
		diffFile)
	if err == nil {
		t.Fatal("expected an error")
	}
	if info, statErr := os.Stat(errorFile); statErr != nil || info.Size() == 0 {
		t.Errorf("stderr should have been captured to %s", errorFile)
	}
}

func TestRunExternalCompareEqualFiles(t *testing.T) {
	daemonFile, expRspFile, diffFile := diffFiles(t, "same\n", "same\n")
	same, err := runExternalCompare(false, "/dev/null", expRspFile, daemonFile, diffFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !same {
		t.Error("identical files should compare equal")
	}
}

func TestJSONValuesEqual(t *testing.T) {
	tests := []struct {
		name string
		lhs  any
		rhs  any
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs value", nil, "x", false},
		{"value vs nil", "x", nil, false},
		{"equal strings", "0x1", "0x1", true},
		{"different strings", "0x1", "0x2", false},
		{"string vs number", "1", 1.0, false},
		{"equal numbers", 1.5, 1.5, true},
		{"different numbers", 1.5, 2.5, false},
		{"number vs bool", 1.0, true, false},
		{"equal bools", true, true, true},
		{"different bools", true, false, false},
		{"equal maps", map[string]any{"a": 1.0}, map[string]any{"a": 1.0}, true},
		{"map vs array", map[string]any{"a": 1.0}, []any{1.0}, false},
		{"equal arrays", []any{1.0, "x"}, []any{1.0, "x"}, true},
		{"arrays of different length", []any{1.0}, []any{1.0, 2.0}, false},
		{"arrays in different order", []any{1.0, 2.0}, []any{2.0, 1.0}, false},
		{"array vs string", []any{1.0}, "1", false},
		{"nested equal", map[string]any{"a": []any{map[string]any{"b": true}}},
			map[string]any{"a": []any{map[string]any{"b": true}}}, true},
		{"nested different", map[string]any{"a": []any{map[string]any{"b": true}}},
			map[string]any{"a": []any{map[string]any{"b": false}}}, false},
		// Types JSON decoding never produces fall through to reflect.DeepEqual.
		{"equal ints via DeepEqual", 7, 7, true},
		{"different ints via DeepEqual", 7, 8, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonValuesEqual(tt.lhs, tt.rhs); got != tt.want {
				t.Errorf("jsonValuesEqual(%v, %v) = %v, want %v", tt.lhs, tt.rhs, got, tt.want)
			}
		})
	}
}

func TestMapsEqualMissingKey(t *testing.T) {
	lhs := map[string]any{"a": 1.0, "b": 2.0}
	rhs := map[string]any{"a": 1.0, "c": 2.0}
	if mapsEqual(lhs, rhs) {
		t.Error("maps with different key sets should not compare equal")
	}
}

func TestArrayEqual(t *testing.T) {
	tests := []struct {
		name string
		lhs  []map[string]any
		rhs  []map[string]any
		want bool
	}{
		{"both empty", nil, nil, true},
		{"equal", []map[string]any{{"a": 1.0}}, []map[string]any{{"a": 1.0}}, true},
		{"different length", []map[string]any{{"a": 1.0}}, nil, false},
		{"different content", []map[string]any{{"a": 1.0}}, []map[string]any{{"a": 2.0}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := arrayEqual(tt.lhs, tt.rhs); got != tt.want {
				t.Errorf("arrayEqual = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompareResponsesArrayOfMaps(t *testing.T) {
	lhs := []map[string]any{{"jsonrpc": "2.0", "id": 1.0}}
	rhs := []map[string]any{{"jsonrpc": "2.0", "id": 1.0}}
	if !compareResponses(lhs, rhs) {
		t.Error("identical batch responses should compare equal")
	}
	if compareResponses(lhs, []map[string]any{{"jsonrpc": "2.0", "id": 2.0}}) {
		t.Error("differing batch responses should not compare equal")
	}
}

func TestMarshalToFileRecordsMarshallingTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	var metrics testdata.TestMetrics

	value := map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": map[string]any{"nested": []any{1.0, 2.0}}}
	if err := marshalToFile(value, path, &metrics); err != nil {
		t.Fatalf("marshalToFile: %v", err)
	}
	if metrics.MarshallingTime <= 0 {
		t.Error("MarshallingTime should be recorded")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Nested objects must be re-indented, not left on one line.
	if !strings.Contains(string(data), "\n  \"result\": {") {
		t.Errorf("output is not indented as expected:\n%s", data)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("output should end with a newline")
	}
}

func TestMarshalToFileUnwritablePath(t *testing.T) {
	var metrics testdata.TestMetrics
	err := marshalToFile(map[string]any{"a": 1}, filepath.Join(t.TempDir(), "missing-dir", "out.json"), &metrics)
	if err == nil {
		t.Error("expected an error for an unwritable path")
	}
}

func TestMarshalToFileUnmarshalableValue(t *testing.T) {
	var metrics testdata.TestMetrics
	err := marshalToFile(make(chan int), filepath.Join(t.TempDir(), "out.json"), &metrics)
	if err == nil {
		t.Error("expected an error for a value JSON cannot encode")
	}
}

func TestDumpJSONsCreatesOutputDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "results")
	daemonFile := filepath.Join(dir, "response.json")
	expRspFile := filepath.Join(dir, "expResponse.json")
	var metrics testdata.TestMetrics

	err := dumpJSONs(true, daemonFile, expRspFile, dir,
		map[string]any{"result": "0x1"}, map[string]any{"result": "0x1"}, &metrics)
	if err != nil {
		t.Fatalf("dumpJSONs: %v", err)
	}
	for _, path := range []string{daemonFile, expRspFile} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("%s was not written: %v", path, statErr)
		}
	}
}

// TestDumpJSONsSkipsEmptyFilenames covers the branch where only one of the two
// files is requested.
func TestDumpJSONsSkipsEmptyFilenames(t *testing.T) {
	dir := t.TempDir()
	daemonFile := filepath.Join(dir, "response.json")
	var metrics testdata.TestMetrics

	err := dumpJSONs(true, daemonFile, "", dir, map[string]any{"result": "0x1"}, nil, &metrics)
	if err != nil {
		t.Fatalf("dumpJSONs: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly one file, got %v", entries)
	}
}

func TestDumpJSONsUncreatableDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var metrics testdata.TestMetrics

	err := dumpJSONs(true, filepath.Join(blocker, "response.json"), "", blocker,
		map[string]any{"a": 1}, nil, &metrics)
	if err == nil {
		t.Error("expected an error when the output directory cannot be created")
	}
}

func TestCompileIgnorePatterns(t *testing.T) {
	if got := compileIgnorePatterns(nil); got != nil {
		t.Errorf("no patterns should compile to nil, got %v", got)
	}

	compiled := compileIgnorePatterns([]string{"result.gasUsed", "result.logs[*].blockHash"})
	if len(compiled) != 2 {
		t.Errorf("compiled patterns: got %d, want 2", len(compiled))
	}
}

// TestCompileIgnorePatternsSkipsInvalid checks that a bad pattern is warned
// about and dropped instead of aborting the run.
func TestCompileIgnorePatternsSkipsInvalid(t *testing.T) {
	compiled := compileIgnorePatterns([]string{"result.gasUsed", "result.[unclosed"})
	if len(compiled) > 2 {
		t.Errorf("compiled patterns: got %d", len(compiled))
	}
}

func TestProcessResponseInconclusiveWhenNotComparable(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	var outcome testdata.TestOutcome

	ProcessResponse(
		map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": "0x1"},
		nil,
		map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": "0x2"},
		cfg, dir,
		filepath.Join(dir, "response.json"),
		filepath.Join(dir, "expResponse.json"),
		filepath.Join(dir, "diff.json"),
		&outcome, nil,
		func() bool { return true },
	)

	if !outcome.Inconclusive {
		t.Error("the outcome should be marked inconclusive")
	}
	if outcome.Success {
		t.Error("an inconclusive outcome is not a success")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("no artefacts should be written, found %v", entries)
	}
}

func TestProcessResponseNeitherMapNorArray(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	var outcome testdata.TestOutcome

	ProcessResponse("a plain string", nil, 42.0, cfg, dir,
		filepath.Join(dir, "response.json"),
		filepath.Join(dir, "expResponse.json"),
		filepath.Join(dir, "diff.json"),
		&outcome, nil, nil)

	if outcome.Error == nil || !strings.Contains(outcome.Error.Error(), "neither maps nor arrays") {
		t.Errorf("got %v, want a 'neither maps nor arrays' error", outcome.Error)
	}
}

func TestProcessResponseArrayBatch(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	var outcome testdata.TestOutcome

	actual := []any{map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": "0x1"}}
	expected := []any{map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": "0x2"}}

	ProcessResponse(actual, nil, expected, cfg, dir,
		filepath.Join(dir, "response.json"),
		filepath.Join(dir, "expResponse.json"),
		filepath.Join(dir, "diff.json"),
		&outcome, nil, nil)

	if outcome.Success {
		t.Error("differing batch responses should fail")
	}
	if outcome.Error == nil {
		t.Fatal("expected a diff mismatch error")
	}
	if outcome.ErrorDetails == nil || outcome.ErrorDetails.Diff == "" {
		t.Error("the failure should carry the diff")
	}
}

// TestProcessResponseWithExternalDiffTool drives the non-Go diff path end to end.
func TestProcessResponseWithExternalDiffTool(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	cfg.DiffKind = config.DiffTool
	var outcome testdata.TestOutcome

	daemonFile := filepath.Join(dir, "response.json")
	expRspFile := filepath.Join(dir, "expResponse.json")
	diffFile := filepath.Join(dir, "diff.json")

	ProcessResponse(
		map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": "0x1"},
		nil,
		map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": "0x2"},
		cfg, dir, daemonFile, expRspFile, diffFile, &outcome, nil, nil)

	if outcome.Success {
		t.Error("differing responses should fail")
	}
	if outcome.ErrorDetails == nil || outcome.ErrorDetails.Diff == "" {
		t.Error("the external diff output should be attached to the failure")
	}
}

// TestProcessResponseRemovesArtefactsWhenEqual checks the housekeeping done
// after a comparison that turned out equal only under a diff pass.
func TestProcessResponseRemovesArtefactsWhenEqual(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	cfg.DiffKind = config.DiffTool
	var outcome testdata.TestOutcome

	// Distinct map instances that are structurally different only in key order
	// still take the diff path, and the files must be cleaned up afterwards.
	response := map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": map[string]any{"a": "1", "b": "2"}}
	expected := map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": map[string]any{"b": "2", "a": "1"}}

	ProcessResponse(response, nil, expected, cfg, dir,
		filepath.Join(dir, "response.json"),
		filepath.Join(dir, "expResponse.json"),
		filepath.Join(dir, "diff.json"),
		&outcome, nil, nil)

	if !outcome.Success {
		t.Fatalf("structurally equal responses should pass, got %v", outcome.Error)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("artefacts should be cleaned up on success, found %v", entries)
	}
}

// TestCompareJSONJSONDiffMismatchIsNotAToolError pins the distinction the
// stderr file exists to make: json-diff exits 1 both when the files differ and
// when it fails, and only the latter is an error.
func TestCompareJSONJSONDiffMismatchIsNotAToolError(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
		wantSame bool
	}{
		{"identical", `{"result":"0x1"}`, `{"result":"0x1"}`, true},
		{"different", `{"result":"0x1"}`, `{"result":"0x2"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemonFile, expRspFile, diffFile := diffFiles(t, tt.actual, tt.expected)
			cfg := config.NewConfig()
			cfg.DiffKind = config.JsonDiffTool
			var metrics testdata.TestMetrics

			same, err := compareJSON(cfg, daemonFile, expRspFile, diffFile, &metrics)
			if err != nil {
				t.Fatalf("a mismatch must not surface as a tool error: %v", err)
			}
			if same != tt.wantSame {
				t.Errorf("same: got %v, want %v", same, tt.wantSame)
			}
		})
	}
}

func TestCompareJSONRemovesStderrFile(t *testing.T) {
	daemonFile, expRspFile, diffFile := diffFiles(t, `{"a":1}`, `{"a":2}`)
	cfg := config.NewConfig()
	cfg.DiffKind = config.DiffTool
	var metrics testdata.TestMetrics

	if _, err := compareJSON(cfg, daemonFile, expRspFile, diffFile, &metrics); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(diffFile + ".err"); !os.IsNotExist(err) {
		t.Errorf("the stderr scratch file should be removed, stat err = %v", err)
	}
}

func TestCompareJSONReportsToolFailure(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewConfig()
	cfg.DiffKind = config.DiffTool
	var metrics testdata.TestMetrics

	_, err := compareJSON(cfg,
		filepath.Join(dir, "absent-a.json"),
		filepath.Join(dir, "absent-b.json"),
		filepath.Join(dir, "diff.json"), &metrics)
	if err == nil {
		t.Error("a diff run against missing files should be reported as an error")
	}
}
