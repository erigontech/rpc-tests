package jsondiff

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffJSON_NilInputs(t *testing.T) {
	tests := []struct {
		name string
		obj1 any
		obj2 any
		opts *Options
	}{
		{"both nil", nil, nil, nil},
		{"first nil", nil, map[string]any{"a": 1}, nil},
		{"second nil", map[string]any{"a": 1}, nil, nil},
		{"both nil with keep unchanged", nil, nil, &Options{KeepUnchangedValues: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DiffJSON(tt.obj1, tt.obj2, tt.opts)
			if result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestDiffJSON_PrimitiveValues(t *testing.T) {
	tests := []struct {
		name          string
		obj1          any
		obj2          any
		expectDiff    bool
		keepUnchanged bool
	}{
		{"equal strings", "hello", "hello", false, false},
		{"different strings", "hello", "world", true, false},
		{"equal numbers", 42.0, 42.0, false, false},
		{"different numbers", 42.0, 43.0, true, false},
		{"equal bools", true, true, false, false},
		{"different bools", true, false, true, false},
		{"keep unchanged equal", "hello", "hello", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Options{KeepUnchangedValues: tt.keepUnchanged}
			result := DiffJSON(tt.obj1, tt.obj2, opts)
			hasDiff := len(result) > 0
			if tt.expectDiff && !hasDiff && !tt.keepUnchanged {
				t.Error("expected diff but got none")
			}
		})
	}
}

func TestDiffJSON_DifferentTypes(t *testing.T) {
	result := DiffJSON("string", 42, nil)
	if len(result) == 0 {
		t.Error("expected diff for different types")
	}
}

func TestDiffJSON_Maps(t *testing.T) {
	tests := []struct {
		name string
		obj1 map[string]any
		obj2 map[string]any
		opts *Options
	}{
		{
			"equal maps",
			map[string]any{"a": 1, "b": 2},
			map[string]any{"a": 1, "b": 2},
			nil,
		},
		{
			"added key",
			map[string]any{"a": 1},
			map[string]any{"a": 1, "b": 2},
			nil,
		},
		{
			"removed key",
			map[string]any{"a": 1, "b": 2},
			map[string]any{"a": 1},
			nil,
		},
		{
			"changed value",
			map[string]any{"a": 1},
			map[string]any{"a": 2},
			nil,
		},
		{
			"sorted keys",
			map[string]any{"b": 1, "a": 2},
			map[string]any{"a": 2, "b": 1},
			&Options{Sort: true},
		},
		{
			"nested maps",
			map[string]any{"a": map[string]any{"b": 1}},
			map[string]any{"a": map[string]any{"b": 2}},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DiffJSON(tt.obj1, tt.obj2, tt.opts)
			if result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestDiffJSON_Arrays(t *testing.T) {
	tests := []struct {
		name string
		obj1 any
		obj2 any
		opts *Options
	}{
		{
			"equal arrays",
			[]any{1, 2, 3},
			[]any{1, 2, 3},
			nil,
		},
		{
			"different arrays",
			[]any{1, 2, 3},
			[]any{1, 2, 4},
			nil,
		},
		{
			"longer second array",
			[]any{1, 2},
			[]any{1, 2, 3},
			nil,
		},
		{
			"shorter second array",
			[]any{1, 2, 3},
			[]any{1, 2},
			nil,
		},
		{
			"sorted arrays",
			[]any{3, 1, 2},
			[]any{1, 2, 3},
			&Options{SortArrays: true},
		},
		{
			"empty arrays",
			[]any{},
			[]any{},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DiffJSON(tt.obj1, tt.obj2, tt.opts)
			if result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestDiffJSON_NonStringKeyMaps(t *testing.T) {
	// Test with non-map[string]any types
	obj1 := map[string]any{"a": 1}
	obj2 := "not a map"

	result := DiffJSON(obj1, obj2, nil)
	if len(result) == 0 {
		t.Error("expected diff for different types")
	}
}

func TestDiffString(t *testing.T) {
	tests := []struct {
		name     string
		obj1     any
		obj2     any
		opts     *Options
		contains []string
	}{
		{
			"added value",
			map[string]any{},
			map[string]any{"a": 1},
			nil,
			[]string{"+", "a"},
		},
		{
			"deleted value",
			map[string]any{"a": 1},
			map[string]any{},
			nil,
			[]string{"-", "a"},
		},
		{
			"updated value",
			map[string]any{"a": 1},
			map[string]any{"a": 2},
			nil,
			[]string{"~", "a", "->"},
		},
		{
			"full output with equal",
			map[string]any{"a": 1},
			map[string]any{"a": 1},
			&Options{Full: true},
			[]string{"a"},
		},
		{
			"nil options",
			map[string]any{"a": 1},
			map[string]any{"a": 2},
			nil,
			[]string{"~"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DiffString(tt.obj1, tt.obj2, tt.opts)
			for _, substr := range tt.contains {
				if !strings.Contains(result, substr) {
					t.Errorf("expected result to contain %q, got: %s", substr, result)
				}
			}
		})
	}
}

func TestColoredString(t *testing.T) {
	tests := []struct {
		name     string
		obj1     any
		obj2     any
		opts     *Options
		contains []string
	}{
		{
			"added value green",
			map[string]any{},
			map[string]any{"a": 1},
			nil,
			[]string{"\033[32m", "+"},
		},
		{
			"deleted value red",
			map[string]any{"a": 1},
			map[string]any{},
			nil,
			[]string{"\033[31m", "-"},
		},
		{
			"updated value yellow",
			map[string]any{"a": 1},
			map[string]any{"a": 2},
			nil,
			[]string{"\033[33m", "~"},
		},
		{
			"full output with equal",
			map[string]any{"a": 1},
			map[string]any{"a": 1},
			&Options{Full: true},
			[]string{"a"},
		},
		{
			"nil options",
			map[string]any{"a": 1},
			map[string]any{"a": 2},
			nil,
			[]string{"\033[0m"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ColoredString(tt.obj1, tt.obj2, tt.opts)
			for _, substr := range tt.contains {
				if !strings.Contains(result, substr) {
					t.Errorf("expected result to contain %q, got: %s", substr, result)
				}
			}
		})
	}
}

func TestCollectDiffs(t *testing.T) {
	tests := []struct {
		name         string
		obj1         any
		obj2         any
		expectedType DiffType
	}{
		{"both nil", nil, nil, DiffEqual},
		{"first nil", nil, "value", DiffAdd},
		{"second nil", "value", nil, DiffDelete},
		{"different types", "string", 42, DiffUpdate},
		{"equal primitives", "hello", "hello", DiffEqual},
		{"different primitives", "hello", "world", DiffUpdate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := collectDiffs(tt.obj1, tt.obj2, "")
			if len(diffs) == 0 {
				t.Error("expected at least one diff")
				return
			}
			if diffs[0].Type != tt.expectedType {
				t.Errorf("expected type %v, got %v", tt.expectedType, diffs[0].Type)
			}
		})
	}
}

func TestCollectMapDiffs(t *testing.T) {
	tests := []struct {
		name string
		obj1 any
		obj2 any
	}{
		{
			"non-map types",
			"not a map",
			"also not a map",
		},
		{
			"nested maps",
			map[string]any{"a": map[string]any{"b": 1}},
			map[string]any{"a": map[string]any{"b": 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := collectDiffs(tt.obj1, tt.obj2, "")
			if diffs == nil {
				t.Error("expected non-nil diffs")
			}
		})
	}
}

func TestCollectArrayDiffs(t *testing.T) {
	tests := []struct {
		name string
		obj1 any
		obj2 any
	}{
		{
			"equal arrays",
			[]any{1, 2, 3},
			[]any{1, 2, 3},
		},
		{
			"first longer",
			[]any{1, 2, 3},
			[]any{1, 2},
		},
		{
			"second longer",
			[]any{1, 2},
			[]any{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := collectDiffs(tt.obj1, tt.obj2, "")
			if diffs == nil {
				t.Error("expected non-nil diffs")
			}
		})
	}
}

func TestSortArrayIfPrimitive(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{"non-slice", "string", "string"},
		{"empty slice", []any{}, []any{}},
		{"primitive ints", []any{3, 1, 2}, []any{1, 2, 3}},
		{"primitive strings", []any{"c", "a", "b"}, []any{"a", "b", "c"}},
		{"non-primitive", []any{map[string]any{"a": 1}}, []any{map[string]any{"a": 1}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sortArrayIfPrimitive(tt.input)
			if result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestIsPrimitive(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected bool
	}{
		{"nil", nil, true},
		{"bool", true, true},
		{"string", "hello", true},
		{"int", 42, true},
		{"int8", int8(42), true},
		{"int16", int16(42), true},
		{"int32", int32(42), true},
		{"int64", int64(42), true},
		{"uint", uint(42), true},
		{"uint8", uint8(42), true},
		{"uint16", uint16(42), true},
		{"uint32", uint32(42), true},
		{"uint64", uint64(42), true},
		{"float32", float32(3.14), true},
		{"float64", 3.14, true},
		{"map", map[string]any{}, false},
		{"slice", []any{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPrimitive(tt.input)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparePrimitives(t *testing.T) {
	tests := []struct {
		name     string
		a        any
		b        any
		expected int
	}{
		{"both nil", nil, nil, 0},
		{"first nil", nil, "a", -1},
		{"second nil", "a", nil, 1},
		{"different types", "a", 1, 1}, // string > int by type name
		{"equal bools true", true, true, 0},
		{"equal bools false", false, false, 0},
		{"true > false", true, false, 1},
		{"false < true", false, true, -1},
		{"equal strings", "hello", "hello", 0},
		{"string less", "a", "b", -1},
		{"string greater", "b", "a", 1},
		{"equal ints", 42, 42, 0},
		{"int less", 1, 2, -1},
		{"int greater", 2, 1, 1},
		{"equal int64", int64(42), int64(42), 0},
		{"int64 less", int64(1), int64(2), -1},
		{"int64 greater", int64(2), int64(1), 1},
		{"equal float64", 3.14, 3.14, 0},
		{"float64 less", 1.0, 2.0, -1},
		{"float64 greater", 2.0, 1.0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := comparePrimitives(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparePrimitives_Fallback(t *testing.T) {
	// Test fallback case with unknown type
	type customType struct {
		value int
	}
	a := customType{value: 1}
	b := customType{value: 2}

	result := comparePrimitives(a, b)
	// Should use string comparison fallback
	if result == 0 {
		t.Error("expected non-zero result for different values")
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"nil", nil, "null"},
		{"string", "hello", `"hello"`},
		{"number", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool", true, "true"},
		{"map", map[string]any{"a": 1}, `{"a":1}`},
		{"slice", []any{1, 2, 3}, "[1,2,3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatValue(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDiffJSON_ComplexNested(t *testing.T) {
	obj1 := map[string]any{
		"users": []any{
			map[string]any{
				"name": "Alice",
				"age":  30,
			},
			map[string]any{
				"name": "Bob",
				"age":  25,
			},
		},
		"metadata": map[string]any{
			"version": "1.0",
			"count":   2,
		},
	}

	obj2 := map[string]any{
		"users": []any{
			map[string]any{
				"name": "Alice",
				"age":  31, // changed
			},
			map[string]any{
				"name": "Bob",
				"age":  25,
			},
			map[string]any{
				"name": "Charlie", // added
				"age":  35,
			},
		},
		"metadata": map[string]any{
			"version": "1.1", // changed
			"count":   3,     // changed
		},
	}

	result := DiffJSON(obj1, obj2, &Options{Sort: true})
	if len(result) == 0 {
		t.Error("expected diffs for nested changes")
	}
}

func TestDiffJSON_WithJSONUnmarshal(t *testing.T) {
	json1 := `{"name": "test", "value": 42}`
	json2 := `{"name": "test", "value": 43, "extra": true}`

	var obj1, obj2 map[string]any
	if err := json.Unmarshal([]byte(json1), &obj1); err != nil {
		t.Fatalf("failed to unmarshal json1: %v", err)
	}
	if err := json.Unmarshal([]byte(json2), &obj2); err != nil {
		t.Fatalf("failed to unmarshal json2: %v", err)
	}

	result := DiffJSON(obj1, obj2, nil)
	if len(result) == 0 {
		t.Error("expected diffs")
	}
}

func TestDiffTypes(t *testing.T) {
	// Ensure all DiffType constants are defined
	types := []DiffType{DiffAdd, DiffDelete, DiffUpdate, DiffEqual}
	expectedValues := []string{"add", "delete", "update", "equal"}

	for i, dt := range types {
		if string(dt) != expectedValues[i] {
			t.Errorf("expected %q, got %q", expectedValues[i], string(dt))
		}
	}
}

func TestDiffStruct(t *testing.T) {
	// Test the Diff struct fields
	d := Diff{
		Type:     DiffUpdate,
		Path:     "test.path",
		OldValue: 1,
		NewValue: 2,
	}

	if d.Type != DiffUpdate {
		t.Errorf("expected DiffUpdate, got %v", d.Type)
	}
	if d.Path != "test.path" {
		t.Errorf("expected test.path, got %v", d.Path)
	}
	if d.OldValue != 1 {
		t.Errorf("expected 1, got %v", d.OldValue)
	}
	if d.NewValue != 2 {
		t.Errorf("expected 2, got %v", d.NewValue)
	}
}

func TestOptions(t *testing.T) {
	// Test the Options struct fields
	opts := Options{
		Full:                true,
		KeepUnchangedValues: true,
		OutputKeys:          []string{"a", "b"},
		Sort:                true,
		SortArrays:          true,
	}

	if !opts.Full {
		t.Error("expected Full to be true")
	}
	if !opts.KeepUnchangedValues {
		t.Error("expected KeepUnchangedValues to be true")
	}
	if len(opts.OutputKeys) != 2 {
		t.Errorf("expected 2 output keys, got %d", len(opts.OutputKeys))
	}
	if !opts.Sort {
		t.Error("expected Sort to be true")
	}
	if !opts.SortArrays {
		t.Error("expected SortArrays to be true")
	}
}

func TestDiffMaps_NonStringKeyMap(t *testing.T) {
	// Test diffMaps with invalid map types
	result := make(map[string]any)
	diffMaps("not a map", "also not a map", "", result, &Options{})
	if len(result) == 0 {
		t.Error("expected result for non-map types")
	}
}

func TestDiffArrays_SortArraysOption(t *testing.T) {
	obj1 := []any{3, 1, 2}
	obj2 := []any{1, 2, 3}

	result := DiffJSON(obj1, obj2, &Options{SortArrays: true})
	// After sorting, arrays should be equal
	if len(result) != 0 {
		t.Errorf("expected no diff for sorted arrays, got result: %v", result)
	}
}

func TestCollectDiffs_Path(t *testing.T) {
	obj1 := map[string]any{
		"level1": map[string]any{
			"level2": "value1",
		},
	}
	obj2 := map[string]any{
		"level1": map[string]any{
			"level2": "value2",
		},
	}

	diffs := collectDiffs(obj1, obj2, "")
	found := false
	for _, d := range diffs {
		if d.Path == "level1.level2" && d.Type == DiffUpdate {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find diff at level1.level2")
	}
}

func TestSortArrayIfPrimitive_MixedPrimitives(t *testing.T) {
	// Test sorting with mixed primitive types
	input := []any{"b", "a", "c"}
	result := sortArrayIfPrimitive(input)

	resultSlice, ok := result.([]any)
	if !ok {
		t.Fatal("expected slice result")
	}

	if resultSlice[0] != "a" || resultSlice[1] != "b" || resultSlice[2] != "c" {
		t.Errorf("expected sorted slice [a, b, c], got %v", resultSlice)
	}
}

func TestDiffJSON_ArrayInMap(t *testing.T) {
	obj1 := map[string]any{
		"items": []any{"a", "b"},
	}
	obj2 := map[string]any{
		"items": []any{"a", "b", "c"},
	}

	result := DiffJSON(obj1, obj2, nil)
	if len(result) == 0 {
		t.Error("expected diff for array change in map")
	}

	// Check that the result contains the expected added value
	found := false
	for path, val := range result {
		if path == "items[2]" {
			if diffMap, ok := val.(map[string]any); ok {
				if diffMap["__new"] == "c" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Errorf("expected to find added item 'c' at items[2], got: %v", result)
	}
}

func TestDiffJSON_EmptyMap(t *testing.T) {
	obj1 := map[string]any{}
	obj2 := map[string]any{}

	result := DiffJSON(obj1, obj2, nil)
	if len(result) != 0 {
		t.Errorf("expected no diffs for equal empty maps, got %v", result)
	}
}

func TestDiffString_NilBoth(t *testing.T) {
	result := DiffString(nil, nil, nil)
	// Both nil should show as equal
	if result != "" {
		t.Errorf("expected no diffs for both nil, got %v", result)
	}
}

func TestColoredString_NilBoth(t *testing.T) {
	result := ColoredString(nil, nil, nil)
	// Both nil should show as equal
	if result != "" {
		t.Errorf("expected no diffs for both nil, got %v", result)
	}
}
