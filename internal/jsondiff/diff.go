package jsondiff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
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
	OldValue any
	NewValue any
}

// Options configures the diff behaviour
type Options struct {
	// Full causes all unchanged values to be included in the output
	Full bool
	// KeepUnchangedValues includes unchanged values in the diff result
	KeepUnchangedValues bool
	// OutputKeys are the keys to include in the output
	OutputKeys []string
	// Sort keys in the output
	Sort bool
	// SortArrays sorts primitive values in arrays before comparing
	SortArrays bool
	// IgnorePatterns excludes volatile or don't-care fields from diff output.
	IgnorePatterns []*regexp.Regexp
}

// CompileIgnorePattern converts a fixture ignoreFields pattern such as
// "result[*].structLogs[*].error" into a compiled regexp. [*] matches any
// array index; the pattern also matches any sub-paths beneath the given path.
func CompileIgnorePattern(pattern string) (*regexp.Regexp, error) {
	// QuoteMeta escapes dots and brackets; replace the escaped [*] with [\d+]
	re := strings.ReplaceAll(regexp.QuoteMeta(pattern), `\[\*\]`, `\[\d+\]`)
	return regexp.Compile(`^` + re + `([\.\[].*)?$`)
}

func shouldIgnore(path string, opts *Options) bool {
	if len(opts.IgnorePatterns) == 0 {
		return false
	}
	for _, re := range opts.IgnorePatterns {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

// DiffJSON computes the difference between two JSON objects
func DiffJSON(obj1, obj2 any, opts *Options) map[string]any {
	if opts == nil {
		opts = &Options{}
	}

	result := make(map[string]any)
	diff(obj1, obj2, "", result, opts)

	return result
}

// DiffString returns a human-readable string representation of differences
func DiffString(obj1, obj2 any, opts *Options) string {
	if opts == nil {
		opts = &Options{}
	}

	diffs := collectDiffs(obj1, obj2, "", opts)

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
func ColoredString(obj1, obj2 any, opts *Options) string {
	if opts == nil {
		opts = &Options{}
	}

	diffs := collectDiffs(obj1, obj2, "", opts)

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

func diff(obj1, obj2 any, path string, result map[string]any, opts *Options) {
	if shouldIgnore(path, opts) {
		return
	}
	// Handle nil cases
	if obj1 == nil && obj2 == nil {
		if opts.KeepUnchangedValues {
			result[path] = map[string]any{"__old": obj1, "__new": obj2}
		}
		return
	}

	if obj1 == nil {
		result[path] = map[string]any{"__old": obj1, "__new": obj2}
		return
	}

	if obj2 == nil {
		result[path] = map[string]any{"__old": obj1, "__new": obj2}
		return
	}

	v1 := reflect.ValueOf(obj1)
	v2 := reflect.ValueOf(obj2)

	// If types are different, mark as changed
	if v1.Kind() != v2.Kind() {
		result[path] = map[string]any{"__old": obj1, "__new": obj2}
		return
	}

	switch v1.Kind() {
	case reflect.Map:
		diffMaps(obj1, obj2, path, result, opts)
	case reflect.Slice, reflect.Array:
		diffArrays(obj1, obj2, path, result, opts)
	default:
		if !reflect.DeepEqual(obj1, obj2) {
			result[path] = map[string]any{"__old": obj1, "__new": obj2}
		} else if opts.KeepUnchangedValues {
			result[path] = map[string]any{"__old": obj1, "__new": obj2}
		}
	}
}

func diffMaps(obj1, obj2 any, path string, result map[string]any, opts *Options) {
	m1, ok1 := obj1.(map[string]any)
	m2, ok2 := obj2.(map[string]any)

	if !ok1 || !ok2 {
		result[path] = map[string]any{"__old": obj1, "__new": obj2}
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
			result[newPath] = map[string]any{"__new": v2}
		} else if !exists2 {
			result[newPath] = map[string]any{"__old": v1}
		} else {
			diff(v1, v2, newPath, result, opts)
		}
	}
}

func diffArrays(obj1, obj2 any, path string, result map[string]any, opts *Options) {
	v1 := reflect.ValueOf(obj1)
	v2 := reflect.ValueOf(obj2)

	// Sort arrays if required
	if opts.SortArrays {
		v1 = reflect.ValueOf(sortArray(obj1, path, opts))
		v2 = reflect.ValueOf(sortArray(obj2, path, opts))
	}

	len1 := v1.Len()
	len2 := v2.Len()

	maxLen := max(len1, len2)

	for i := range maxLen {
		newPath := fmt.Sprintf("%s[%d]", path, i)

		if i >= len1 {
			result[newPath] = map[string]any{"__new": v2.Index(i).Interface()}
		} else if i >= len2 {
			result[newPath] = map[string]any{"__old": v1.Index(i).Interface()}
		} else {
			diff(v1.Index(i).Interface(), v2.Index(i).Interface(), newPath, result, opts)
		}
	}
}

func collectDiffs(obj1, obj2 any, path string, opts *Options) []Diff {
	var diffs []Diff
	collectDiffsRec(obj1, obj2, path, &diffs, opts)
	return diffs
}

func collectDiffsRec(obj1, obj2 any, path string, diffs *[]Diff, opts *Options) {
	if shouldIgnore(path, opts) {
		return
	}
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
		collectMapDiffs(obj1, obj2, path, diffs, opts)
	case reflect.Slice, reflect.Array:
		collectArrayDiffs(obj1, obj2, path, diffs, opts)
	default:
		if !reflect.DeepEqual(obj1, obj2) {
			*diffs = append(*diffs, Diff{Type: DiffUpdate, Path: path, OldValue: obj1, NewValue: obj2})
		} else {
			*diffs = append(*diffs, Diff{Type: DiffEqual, Path: path, NewValue: obj2})
		}
	}
}

func collectMapDiffs(obj1, obj2 any, path string, diffs *[]Diff, opts *Options) {
	m1, ok1 := obj1.(map[string]any)
	m2, ok2 := obj2.(map[string]any)

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
			collectDiffsRec(v1, v2, newPath, diffs, opts)
		}
	}
}

func collectArrayDiffs(obj1, obj2 any, path string, diffs *[]Diff, opts *Options) {
	if opts.SortArrays {
		obj1 = sortArray(obj1, path, opts)
		obj2 = sortArray(obj2, path, opts)
	}

	v1 := reflect.ValueOf(obj1)
	v2 := reflect.ValueOf(obj2)

	len1 := v1.Len()
	len2 := v2.Len()

	maxLen := max(len1, len2)

	for i := range maxLen {
		newPath := fmt.Sprintf("%s[%d]", path, i)

		if i >= len1 {
			*diffs = append(*diffs, Diff{Type: DiffAdd, Path: newPath, NewValue: v2.Index(i).Interface()})
		} else if i >= len2 {
			*diffs = append(*diffs, Diff{Type: DiffDelete, Path: newPath, OldValue: v1.Index(i).Interface()})
		} else {
			collectDiffsRec(v1.Index(i).Interface(), v2.Index(i).Interface(), newPath, diffs, opts)
		}
	}
}

// maxObjectSortSize is the maximum number of elements for which object arrays
// are sorted. Larger arrays (e.g. structLogs) are left in their original order
// to avoid unbounded marshalling time.
const maxObjectSortSize = 1000

// sortArray sorts arrays for order-independent comparison.
// Primitive arrays are sorted by value. Object/nested arrays up to
// maxObjectSortSize elements are sorted by pre-computed JSON keys with ignored
// fields stripped; larger arrays are returned unsorted.
func sortArray(arr any, path string, opts *Options) any {
	v := reflect.ValueOf(arr)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return arr
	}
	n := v.Len()
	if n == 0 {
		return arr
	}

	if isPrimitive(v.Index(0).Interface()) {
		slice := make([]any, n)
		for i := range n {
			slice[i] = v.Index(i).Interface()
		}
		sort.Slice(slice, func(i, j int) bool {
			return comparePrimitives(slice[i], slice[j]) < 0
		})
		return slice
	}

	if n > maxObjectSortSize {
		return arr
	}

	// Pre-compute one JSON key per element (with ignored fields stripped so they
	// don't affect sort order) then sort by key.
	elemPath := fmt.Sprintf("%s[0]", path)
	type entry struct {
		key string
		val any
	}
	entries := make([]entry, n)
	for i := range n {
		elem := v.Index(i).Interface()
		entries[i] = entry{objectSortKey(elem, elemPath, opts), elem}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})
	result := make([]any, n)
	for i, e := range entries {
		result[i] = e.val
	}
	return result
}

// objectSortKey returns a JSON sort key for elem with top-level ignored fields stripped.
func objectSortKey(elem any, elemPath string, opts *Options) string {
	if m, ok := elem.(map[string]any); ok && opts != nil && len(opts.IgnorePatterns) > 0 {
		filtered := make(map[string]any, len(m))
		for k, v := range m {
			if !shouldIgnore(elemPath+"."+k, opts) {
				filtered[k] = v
			}
		}
		b, _ := json.Marshal(filtered)
		return string(b)
	}
	b, _ := json.Marshal(elem)
	return string(b)
}

func isPrimitive(v any) bool {
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

func comparePrimitives(a, b any) int {
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

func formatValue(v any) string {
	if v == nil {
		return "null"
	}

	switch val := v.(type) {
	case string:
		return fmt.Sprintf(`"%s"`, val)
	case map[string]any, []any:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}
