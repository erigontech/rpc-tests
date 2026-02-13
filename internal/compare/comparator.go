package compare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/josephburnett/jd/v2"
	jsoniter "github.com/json-iterator/go"

	"github.com/erigontech/rpc-tests/cmd/integration/jsondiff"
	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

var (
	ErrDiffTimeout  = errors.New("diff timeout")
	ErrDiffMismatch = errors.New("diff mismatch")
)

const (
	externalToolTimeout = 30 * time.Second
)

// ProcessResponse compares actual response against expected, handling all "don't care" cases.
// This is the v2 equivalent of v1's processResponse method.
func ProcessResponse(
	response, referenceResponse, responseInFile any,
	cfg *config.Config,
	cmd *testdata.JsonRpcCommand,
	outputDir, daemonFile, expRspFile, diffFile string,
	outcome *testdata.TestOutcome,
) {
	var expectedResponse any
	if referenceResponse != nil {
		expectedResponse = referenceResponse
	} else {
		expectedResponse = responseInFile
	}

	if cfg.WithoutCompareResults {
		err := dumpJSONs(cfg.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse, &outcome.Metrics)
		if err != nil {
			outcome.Error = err
			return
		}
		outcome.Success = true
		return
	}

	// Fast path: structural equality check
	if compareResponses(response, expectedResponse) {
		outcome.Metrics.EqualCount++
		err := dumpJSONs(cfg.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse, &outcome.Metrics)
		if err != nil {
			outcome.Error = err
			return
		}
		outcome.Success = true
		return
	}

	// Check "don't care" conditions
	responseMap, respIsMap := response.(map[string]any)
	expectedMap, expIsMap := expectedResponse.(map[string]any)
	if respIsMap && expIsMap {
		_, responseHasResult := responseMap["result"]
		expectedResult, expectedHasResult := expectedMap["result"]
		_, responseHasError := responseMap["error"]
		expectedError, expectedHasError := expectedMap["error"]

		// Null expected result with a non-nil reference -> accept
		if responseHasResult && expectedHasResult && expectedResult == nil && referenceResponse == nil {
			err := dumpJSONs(cfg.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse, &outcome.Metrics)
			if err != nil {
				outcome.Error = err
				return
			}
			outcome.Success = true
			return
		}
		// Null expected error -> accept
		if responseHasError && expectedHasError && expectedError == nil {
			err := dumpJSONs(cfg.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse, &outcome.Metrics)
			if err != nil {
				outcome.Error = err
				return
			}
			outcome.Success = true
			return
		}
		// Empty expected (just "jsonrpc" + "id") -> accept
		if !expectedHasResult && !expectedHasError && len(expectedMap) == 2 {
			err := dumpJSONs(cfg.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse, &outcome.Metrics)
			if err != nil {
				outcome.Error = err
				return
			}
			outcome.Success = true
			return
		}
		// Both have error and DoNotCompareError -> accept
		if responseHasError && expectedHasError && cfg.DoNotCompareError {
			err := dumpJSONs(cfg.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse, &outcome.Metrics)
			if err != nil {
				outcome.Error = err
				return
			}
			outcome.Success = true
			return
		}
	}

	// Detailed comparison: dump files and run diff
	err := dumpJSONs(true, daemonFile, expRspFile, outputDir, response, expectedResponse, &outcome.Metrics)
	if err != nil {
		outcome.Error = err
		return
	}

	var same bool
	if cfg.DiffKind == config.JsonDiffGo {
		outcome.Metrics.ComparisonCount++
		opts := &jsondiff.Options{SortArrays: true}
		if respIsMap && expIsMap {
			diff := jsondiff.DiffJSON(expectedMap, responseMap, opts)
			same = len(diff) == 0
			diffString := jsondiff.DiffString(expectedMap, responseMap, opts)
			if writeErr := os.WriteFile(diffFile, []byte(diffString), 0644); writeErr != nil {
				outcome.Error = writeErr
				return
			}
			if !same {
				outcome.Error = ErrDiffMismatch
				if cfg.ReqTestNum != -1 {
					outcome.ColoredDiff = jsondiff.ColoredString(expectedMap, responseMap, opts)
				}
			}
		} else {
			responseArray, respIsArray := response.([]any)
			expectedArray, expIsArray := expectedResponse.([]any)
			if !respIsArray || !expIsArray {
				outcome.Error = errors.New("cannot compare JSON objects (neither maps nor arrays)")
				return
			}
			diff := jsondiff.DiffJSON(expectedArray, responseArray, opts)
			same = len(diff) == 0
			diffString := jsondiff.DiffString(expectedArray, responseArray, opts)
			if writeErr := os.WriteFile(diffFile, []byte(diffString), 0644); writeErr != nil {
				outcome.Error = writeErr
				return
			}
			if !same {
				outcome.Error = ErrDiffMismatch
				if cfg.ReqTestNum != -1 {
					outcome.ColoredDiff = jsondiff.ColoredString(expectedArray, responseArray, opts)
				}
			}
		}
	} else {
		same, err = compareJSON(cfg, cmd, daemonFile, expRspFile, diffFile, &outcome.Metrics)
		if err != nil {
			outcome.Error = err
			return
		}
	}

	if same && !cfg.ForceDumpJSONs {
		os.Remove(daemonFile)
		os.Remove(expRspFile)
		os.Remove(diffFile)
	}

	outcome.Success = same
}

// compareResponses does a fast structural equality check.
func compareResponses(lhs, rhs any) bool {
	leftMap, leftIsMap := lhs.(map[string]any)
	rightMap, rightIsMap := rhs.(map[string]any)
	if leftIsMap && rightIsMap {
		return mapsEqual(leftMap, rightMap)
	}
	leftArray, leftIsArray := lhs.([]map[string]any)
	rightArray, rightIsArray := rhs.([]map[string]any)
	if leftIsArray && rightIsArray {
		return arrayEqual(leftArray, rightArray)
	}
	return jsonValuesEqual(lhs, rhs)
}

// jsonValuesEqual compares two JSON-decoded values without reflection for common types.
// JSON only produces: string, float64, bool, nil, map[string]any, []any.
func jsonValuesEqual(lhs, rhs any) bool {
	if lhs == nil && rhs == nil {
		return true
	}
	if lhs == nil || rhs == nil {
		return false
	}
	switch l := lhs.(type) {
	case string:
		r, ok := rhs.(string)
		return ok && l == r
	case float64:
		r, ok := rhs.(float64)
		return ok && l == r
	case bool:
		r, ok := rhs.(bool)
		return ok && l == r
	case map[string]any:
		r, ok := rhs.(map[string]any)
		return ok && mapsEqual(l, r)
	case []any:
		r, ok := rhs.([]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for i := range l {
			if !jsonValuesEqual(l[i], r[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(lhs, rhs)
	}
}

func mapsEqual(lhs, rhs map[string]any) bool {
	if len(lhs) != len(rhs) {
		return false
	}
	for k, lv := range lhs {
		rv, ok := rhs[k]
		if !ok || !jsonValuesEqual(lv, rv) {
			return false
		}
	}
	return true
}

func arrayEqual(lhs, rhs []map[string]any) bool {
	if len(lhs) != len(rhs) {
		return false
	}
	for i := range lhs {
		if !mapsEqual(lhs[i], rhs[i]) {
			return false
		}
	}
	return true
}

// marshalToFile marshals a value to JSON and writes it to a file using a pooled buffer.
func marshalToFile(value any, filename string, metrics *testdata.TestMetrics) error {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	start := time.Now()
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return err
	}
	metrics.MarshallingTime += time.Since(start)

	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("exception on file write: %w", err)
	}
	return nil
}

// dumpJSONs writes actual/expected responses to files if needed.
func dumpJSONs(dump bool, daemonFile, expRspFile, outputDir string, response, expectedResponse any, metrics *testdata.TestMetrics) error {
	if !dump {
		return nil
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("exception on makedirs: %s %w", outputDir, err)
	}

	if daemonFile != "" {
		if err := marshalToFile(response, daemonFile, metrics); err != nil {
			return err
		}
	}

	if expRspFile != "" {
		if err := marshalToFile(expectedResponse, expRspFile, metrics); err != nil {
			return err
		}
	}
	return nil
}

// compareJSON dispatches to the appropriate external diff tool.
func compareJSON(cfg *config.Config, cmd *testdata.JsonRpcCommand, daemonFile, expRspFile, diffFile string, metrics *testdata.TestMetrics) (bool, error) {
	metrics.ComparisonCount++

	switch cfg.DiffKind {
	case config.JdLibrary:
		return runCompareJD(cmd, expRspFile, daemonFile, diffFile)
	case config.JsonDiffTool:
		return runExternalCompare(true, "/dev/null", expRspFile, daemonFile, diffFile)
	case config.DiffTool:
		return runExternalCompare(false, "/dev/null", expRspFile, daemonFile, diffFile)
	default:
		return false, fmt.Errorf("unknown JSON diff kind: %d", cfg.DiffKind)
	}
}

// runCompareJD uses the JD library for comparison, with 30s timeout and pathOptions support.
func runCompareJD(cmd *testdata.JsonRpcCommand, file1, file2, diffFile string) (bool, error) {
	node1, err := jd.ReadJsonFile(file1)
	if err != nil {
		return false, err
	}
	node2, err := jd.ReadJsonFile(file2)
	if err != nil {
		return false, err
	}

	type result struct {
		diff jd.Diff
		err  error
	}

	resChan := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), externalToolTimeout)
	defer cancel()

	go func() {
		var d jd.Diff
		if cmd.TestInfo != nil && cmd.TestInfo.Metadata != nil && cmd.TestInfo.Metadata.Response != nil && cmd.TestInfo.Metadata.Response.PathOptions != nil {
			options, err := jd.ReadOptionsString(string(cmd.TestInfo.Metadata.Response.PathOptions))
			if err != nil {
				resChan <- result{err: err}
				return
			}
			d = node1.Diff(node2, options...)
		} else {
			d = node1.Diff(node2)
		}
		resChan <- result{diff: d}
	}()

	select {
	case <-ctx.Done():
		return false, fmt.Errorf("JSON diff (JD) timeout for files %s and %s", file1, file2)
	case res := <-resChan:
		if res.err != nil {
			return false, res.err
		}
		diffString := res.diff.Render()
		if err := os.WriteFile(diffFile, []byte(diffString), 0644); err != nil {
			return false, err
		}
		// Check if diff file is empty (no differences)
		info, err := os.Stat(diffFile)
		if err != nil {
			return false, err
		}
		return info.Size() == 0, nil
	}
}

// runExternalCompare runs json-diff or diff as an external process with timeout.
func runExternalCompare(useJsonDiff bool, errorFile, file1, file2, diffFile string) (bool, error) {
	var cmdStr string
	if useJsonDiff {
		if _, err := exec.LookPath("json-diff"); err != nil {
			// Fall back to regular diff
			useJsonDiff = false
		}
	}

	if useJsonDiff {
		cmdStr = fmt.Sprintf("json-diff -s %s %s > %s 2> %s", file1, file2, diffFile, errorFile)
	} else {
		cmdStr = fmt.Sprintf("diff %s %s > %s 2> %s", file1, file2, diffFile, errorFile)
	}

	ctx, cancel := context.WithTimeout(context.Background(), externalToolTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	if err := cmd.Run(); err != nil {
		// diff returns 1 when files differ, which is not an error for us
		var exitErr *exec.ExitError
		if !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && !useJsonDiff) {
			return false, fmt.Errorf("external compare command failed: %w", err)
		}
	}

	// Check error file
	if errorFile != "/dev/null" {
		fi, err := os.Stat(errorFile)
		if err == nil && fi.Size() > 0 {
			if !useJsonDiff {
				return false, fmt.Errorf("diff command produced errors")
			}
			// Fall back to regular diff
			return runExternalCompare(false, errorFile, file1, file2, diffFile)
		}
	}

	// Check diff file size
	fi, err := os.Stat(diffFile)
	if err != nil {
		return false, err
	}
	return fi.Size() == 0, nil
}

// OutputFilePaths returns the standard output file paths for a test.
func OutputFilePaths(outputDir, jsonFile string) (outputAPIFilename, outputDirName, diffFile, daemonFile, expRspFile string) {
	outputAPIFilename = filepath.Join(outputDir, strings.TrimSuffix(jsonFile, filepath.Ext(jsonFile)))
	outputDirName = filepath.Dir(outputAPIFilename)
	diffFile = outputAPIFilename + "-diff.json"
	daemonFile = outputAPIFilename + "-response.json"
	expRspFile = outputAPIFilename + "-expResponse.json"
	return
}
