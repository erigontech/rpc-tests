package jsondiff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// DiffType represents the type of difference
type DiffType string

const (
	DiffAdd    DiffType = "add"
	DiffDelete DiffType = "delete"
	DiffUpdate DiffType = "update"
	DiffEqual  DiffType = "equal"
)

// Diff represents a single difference
type Diff struct {
	Type     DiffType
	Path     string
	OldValue interface{}
	NewValue interface{}
}

// Options configures the diff behavior
type Options struct {
	// Full causes all unchanged values to be included in output
	Full bool
	// KeepUnchangedValues includes unchanged values in the diff result
	KeepUnchangedValues bool
	// OutputKeys are the keys to include in the output
	OutputKeys []string
	// Sort keys in output
	Sort bool
	// SortArrays sorts primitive values in arrays before comparing
	SortArrays bool
}

// DiffJSON computes the difference between two JSON objects
func DiffJSON(obj1, obj2 interface{}, opts *Options) map[string]interface{} {
	if opts == nil {
		opts = &Options{}
	}

	result := make(map[string]interface{})
	diff(obj1, obj2, "", result, opts)

	return result
}

// DiffString returns a human-readable string representation of differences
func DiffString(obj1, obj2 interface{}, opts *Options) string {
	if opts == nil {
		opts = &Options{}
	}

	diffs := collectDiffs(obj1, obj2, "")

	var sb strings.Builder
	for _, d := range diffs {
		switch d.Type {
		case DiffAdd:
			sb.WriteString(fmt.Sprintf("+ %s: %v\n", d.Path, formatValue(d.NewValue)))
		case DiffDelete:
			sb.WriteString(fmt.Sprintf("- %s: %v\n", d.Path, formatValue(d.OldValue)))
		case DiffUpdate:
			sb.WriteString(fmt.Sprintf("~ %s: %v -> %v\n", d.Path, formatValue(d.OldValue), formatValue(d.NewValue)))
		case DiffEqual:
			if opts.Full {
				sb.WriteString(fmt.Sprintf("  %s: %v\n", d.Path, formatValue(d.NewValue)))
			}
		}
	}

	return sb.String()
}

// ColoredString returns a colored diff string (for terminal output)
func ColoredString(obj1, obj2 interface{}, opts *Options) string {
	if opts == nil {
		opts = &Options{}
	}

	diffs := collectDiffs(obj1, obj2, "")

	const (
		colorReset  = "\033[0m"
		colorRed    = "\033[31m"
		colorGreen  = "\033[32m"
		colorYellow = "\033[33m"
	)

	var sb strings.Builder
	for _, d := range diffs {
		switch d.Type {
		case DiffAdd:
			sb.WriteString(fmt.Sprintf("%s+ %s: %v%s\n", colorGreen, d.Path, formatValue(d.NewValue), colorReset))
		case DiffDelete:
			sb.WriteString(fmt.Sprintf("%s- %s: %v%s\n", colorRed, d.Path, formatValue(d.OldValue), colorReset))
		case DiffUpdate:
			sb.WriteString(fmt.Sprintf("%s~ %s: %v -> %v%s\n", colorYellow, d.Path, formatValue(d.OldValue), formatValue(d.NewValue), colorReset))
		case DiffEqual:
			if opts.Full {
				sb.WriteString(fmt.Sprintf("  %s: %v\n", d.Path, formatValue(d.NewValue)))
			}
		}
	}

	return sb.String()
}

func diff(obj1, obj2 interface{}, path string, result map[string]interface{}, opts *Options) {
	// Handle nil cases
	if obj1 == nil && obj2 == nil {
		if opts.KeepUnchangedValues {
			result[path] = map[string]interface{}{"__old": obj1, "__new": obj2}
		}
		return
	}

	if obj1 == nil {
		result[path] = map[string]interface{}{"__old": obj1, "__new": obj2}
		return
	}

	if obj2 == nil {
		result[path] = map[string]interface{}{"__old": obj1, "__new": obj2}
		return
	}

	v1 := reflect.ValueOf(obj1)
	v2 := reflect.ValueOf(obj2)

	// If types are different, mark as changed
	if v1.Kind() != v2.Kind() {
		result[path] = map[string]interface{}{"__old": obj1, "__new": obj2}
		return
	}

	switch v1.Kind() {
	case reflect.Map:
		diffMaps(obj1, obj2, path, result, opts)
	case reflect.Slice, reflect.Array:
		diffArrays(obj1, obj2, path, result, opts)
	default:
		if !reflect.DeepEqual(obj1, obj2) {
			result[path] = map[string]interface{}{"__old": obj1, "__new": obj2}
		} else if opts.KeepUnchangedValues {
			result[path] = map[string]interface{}{"__old": obj1, "__new": obj2}
		}
	}
}

func diffMaps(obj1, obj2 interface{}, path string, result map[string]interface{}, opts *Options) {
	m1, ok1 := obj1.(map[string]interface{})
	m2, ok2 := obj2.(map[string]interface{})

	if !ok1 || !ok2 {
		result[path] = map[string]interface{}{"__old": obj1, "__new": obj2}
		return
	}

	// Collect all keys
	allKeys := make(map[string]bool)
	for k := range m1 {
		allKeys[k] = true
	}
	for k := range m2 {
		allKeys[k] = true
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}

	if opts.Sort {
		sort.Strings(keys)
	}

	for _, key := range keys {
		v1, exists1 := m1[key]
		v2, exists2 := m2[key]

		newPath := key
		if path != "" {
			newPath = path + "." + key
		}

		if !exists1 {
			result[newPath] = map[string]interface{}{"__new": v2}
		} else if !exists2 {
			result[newPath] = map[string]interface{}{"__old": v1}
		} else {
			diff(v1, v2, newPath, result, opts)
		}
	}
}

func diffArrays(obj1, obj2 interface{}, path string, result map[string]interface{}, opts *Options) {
	v1 := reflect.ValueOf(obj1)
	v2 := reflect.ValueOf(obj2)

	// Sort arrays if option is enabled
	arr1 := obj1
	arr2 := obj2

	if opts.SortArrays {
		arr1 = sortArrayIfPrimitive(obj1)
		arr2 = sortArrayIfPrimitive(obj2)
		v1 = reflect.ValueOf(arr1)
		v2 = reflect.ValueOf(arr2)
	}

	len1 := v1.Len()
	len2 := v2.Len()

	maxLen := len1
	if len2 > maxLen {
		maxLen = len2
	}

	for i := 0; i < maxLen; i++ {
		newPath := fmt.Sprintf("%s[%d]", path, i)

		if i >= len1 {
			result[newPath] = map[string]interface{}{"__new": v2.Index(i).Interface()}
		} else if i >= len2 {
			result[newPath] = map[string]interface{}{"__old": v1.Index(i).Interface()}
		} else {
			diff(v1.Index(i).Interface(), v2.Index(i).Interface(), newPath, result, opts)
		}
	}
}

func collectDiffs(obj1, obj2 interface{}, path string) []Diff {
	var diffs []Diff
	collectDiffsRec(obj1, obj2, path, &diffs)
	return diffs
}

func collectDiffsRec(obj1, obj2 interface{}, path string, diffs *[]Diff) {
	if obj1 == nil && obj2 == nil {
		*diffs = append(*diffs, Diff{Type: DiffEqual, Path: path, NewValue: obj2})
		return
	}

	if obj1 == nil {
		*diffs = append(*diffs, Diff{Type: DiffAdd, Path: path, NewValue: obj2})
		return
	}

	if obj2 == nil {
		*diffs = append(*diffs, Diff{Type: DiffDelete, Path: path, OldValue: obj1})
		return
	}

	v1 := reflect.ValueOf(obj1)
	v2 := reflect.ValueOf(obj2)

	if v1.Kind() != v2.Kind() {
		*diffs = append(*diffs, Diff{Type: DiffUpdate, Path: path, OldValue: obj1, NewValue: obj2})
		return
	}

	switch v1.Kind() {
	case reflect.Map:
		collectMapDiffs(obj1, obj2, path, diffs)
	case reflect.Slice, reflect.Array:
		collectArrayDiffs(obj1, obj2, path, diffs)
	default:
		if !reflect.DeepEqual(obj1, obj2) {
			*diffs = append(*diffs, Diff{Type: DiffUpdate, Path: path, OldValue: obj1, NewValue: obj2})
		} else {
			*diffs = append(*diffs, Diff{Type: DiffEqual, Path: path, NewValue: obj2})
		}
	}
}

func collectMapDiffs(obj1, obj2 interface{}, path string, diffs *[]Diff) {
	m1, ok1 := obj1.(map[string]interface{})
	m2, ok2 := obj2.(map[string]interface{})

	if !ok1 || !ok2 {
		*diffs = append(*diffs, Diff{Type: DiffUpdate, Path: path, OldValue: obj1, NewValue: obj2})
		return
	}

	allKeys := make(map[string]bool)
	for k := range m1 {
		allKeys[k] = true
	}
	for k := range m2 {
		allKeys[k] = true
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		v1, exists1 := m1[key]
		v2, exists2 := m2[key]

		newPath := key
		if path != "" {
			newPath = path + "." + key
		}

		if !exists1 {
			*diffs = append(*diffs, Diff{Type: DiffAdd, Path: newPath, NewValue: v2})
		} else if !exists2 {
			*diffs = append(*diffs, Diff{Type: DiffDelete, Path: newPath, OldValue: v1})
		} else {
			collectDiffsRec(v1, v2, newPath, diffs)
		}
	}
}

func collectArrayDiffs(obj1, obj2 interface{}, path string, diffs *[]Diff) {
	v1 := reflect.ValueOf(obj1)
	v2 := reflect.ValueOf(obj2)

	len1 := v1.Len()
	len2 := v2.Len()

	maxLen := len1
	if len2 > maxLen {
		maxLen = len2
	}

	for i := 0; i < maxLen; i++ {
		newPath := fmt.Sprintf("%s[%d]", path, i)

		if i >= len1 {
			*diffs = append(*diffs, Diff{Type: DiffAdd, Path: newPath, NewValue: v2.Index(i).Interface()})
		} else if i >= len2 {
			*diffs = append(*diffs, Diff{Type: DiffDelete, Path: newPath, OldValue: v1.Index(i).Interface()})
		} else {
			collectDiffsRec(v1.Index(i).Interface(), v2.Index(i).Interface(), newPath, diffs)
		}
	}
}

func sortArrayIfPrimitive(arr interface{}) interface{} {
	v := reflect.ValueOf(arr)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return arr
	}

	if v.Len() == 0 {
		return arr
	}

	// Check if array contains only primitives
	firstElem := v.Index(0).Interface()
	if !isPrimitive(firstElem) {
		return arr
	}

	// Create a copy and sort it
	slice := make([]interface{}, v.Len())
	for i := 0; i < v.Len(); i++ {
		slice[i] = v.Index(i).Interface()
	}

	sort.Slice(slice, func(i, j int) bool {
		return comparePrimitives(slice[i], slice[j]) < 0
	})

	return slice
}

func isPrimitive(v interface{}) bool {
	if v == nil {
		return true
	}

	switch v.(type) {
	case bool, string, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}

func comparePrimitives(a, b interface{}) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	// Compare by type first
	typeA := fmt.Sprintf("%T", a)
	typeB := fmt.Sprintf("%T", b)

	if typeA != typeB {
		return strings.Compare(typeA, typeB)
	}

	// Compare by value
	switch v := a.(type) {
	case bool:
		if v == b.(bool) {
			return 0
		}
		if v {
			return 1
		}
		return -1
	case string:
		return strings.Compare(v, b.(string))
	case int:
		bv := b.(int)
		if v < bv {
			return -1
		} else if v > bv {
			return 1
		}
		return 0
	case int64:
		bv := b.(int64)
		if v < bv {
			return -1
		} else if v > bv {
			return 1
		}
		return 0
	case float64:
		bv := b.(float64)
		if v < bv {
			return -1
		} else if v > bv {
			return 1
		}
		return 0
	default:
		// Fallback to string comparison
		return strings.Compare(fmt.Sprintf("%v", a), fmt.Sprintf("%v", b))
	}
}

func formatValue(v interface{}) string {
	if v == nil {
		return "null"
	}

	switch val := v.(type) {
	case string:
		return fmt.Sprintf(`"%s"`, val)
	case map[string]interface{}, []interface{}:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}
