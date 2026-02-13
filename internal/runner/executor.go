package runner

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/erigontech/rpc-tests/internal/compare"
	"github.com/erigontech/rpc-tests/internal/config"
	internalrpc "github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

// RunTest executes a single test and returns the outcome.
// This is the v2 equivalent of v1's runTest + run methods.
// The client parameter is a pre-created RPC client shared across tests (goroutine-safe).
func RunTest(ctx context.Context, descriptor *testdata.TestDescriptor, cfg *config.Config, client *internalrpc.Client) testdata.TestOutcome {
	jsonFilename := filepath.Join(cfg.JSONDir, descriptor.Name)

	outcome := testdata.TestOutcome{}

	var commands []testdata.JsonRpcCommand
	var err error
	if testdata.IsArchive(jsonFilename) {
		commands, err = testdata.LoadFixture(jsonFilename, cfg.SanitizeArchiveExt, &outcome.Metrics)
	} else {
		commands, err = testdata.LoadFixture(jsonFilename, false, &outcome.Metrics)
	}
	if err != nil {
		outcome.Error = err
		return outcome
	}

	if len(commands) != 1 {
		outcome.Error = errors.New("expected exactly one JSON RPC command in " + jsonFilename)
		return outcome
	}

	runCommand(ctx, cfg, &commands[0], descriptor, &outcome, client)
	return outcome
}

// runCommand executes a single JSON-RPC command against the target.
func runCommand(ctx context.Context, cfg *config.Config, cmd *testdata.JsonRpcCommand, descriptor *testdata.TestDescriptor, outcome *testdata.TestOutcome, baseClient *internalrpc.Client) {
	transportType := descriptor.TransportType
	jsonFile := descriptor.Name
	request := cmd.Request

	target := cfg.GetTarget(cfg.DaemonUnderTest, descriptor.Name)

	// Use pre-created client; create per-test client only when JWT is needed (fresh iat per request)
	client := baseClient
	if cfg.JWTSecret != "" {
		secretBytes, _ := hex.DecodeString(cfg.JWTSecret)
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"iat": time.Now().Unix(),
		})
		tokenString, _ := token.SignedString(secretBytes)
		client = internalrpc.NewClient(transportType, "Bearer "+tokenString, cfg.VerboseLevel)
	}

	outputAPIFilename, outputDirName, diffFile, daemonFile, expRspFile := compare.OutputFilePaths(cfg.OutputDir, jsonFile)

	if !cfg.VerifyWithDaemon {
		var result any
		metrics, err := client.Call(ctx, target, request, &result)
		outcome.Metrics.RoundTripTime += metrics.RoundTripTime
		outcome.Metrics.UnmarshallingTime += metrics.UnmarshallingTime
		if err != nil {
			outcome.Error = err
			return
		}
		if cfg.VerboseLevel > 2 {
			fmt.Printf("%s: [%v]\n", cfg.DaemonUnderTest, result)
		}

		compare.ProcessResponse(result, nil, cmd.Response, cfg, cmd, outputDirName, daemonFile, expRspFile, diffFile, outcome)
	} else {
		target = cfg.GetTarget(config.DaemonOnDefaultPort, descriptor.Name)

		var result any
		metrics, err := client.Call(ctx, target, request, &result)
		outcome.Metrics.RoundTripTime += metrics.RoundTripTime
		outcome.Metrics.UnmarshallingTime += metrics.UnmarshallingTime
		if err != nil {
			outcome.Error = err
			return
		}
		if cfg.VerboseLevel > 2 {
			fmt.Printf("%s: [%v]\n", cfg.DaemonUnderTest, result)
		}

		target1 := cfg.GetTarget(cfg.DaemonAsReference, descriptor.Name)
		var result1 any
		metrics1, err := client.Call(ctx, target1, request, &result1)
		outcome.Metrics.RoundTripTime += metrics1.RoundTripTime
		outcome.Metrics.UnmarshallingTime += metrics1.UnmarshallingTime
		if err != nil {
			outcome.Error = err
			return
		}
		if cfg.VerboseLevel > 2 {
			fmt.Printf("%s: [%v]\n", cfg.DaemonAsReference, result1)
		}

		daemonFile = outputAPIFilename + config.GetJSONFilenameExt(config.DaemonOnDefaultPort, target)
		expRspFile = outputAPIFilename + config.GetJSONFilenameExt(cfg.DaemonAsReference, target1)

		compare.ProcessResponse(result, result1, nil, cfg, cmd, outputDirName, daemonFile, expRspFile, diffFile, outcome)
	}
}

// mustAtoi converts a string to int, returning 0 on failure.
func mustAtoi(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// IsStartTestReached checks if we've reached the start-from-test threshold.
// Uses cfg.StartTestNum which is cached at config init time for zero-alloc lookup.
func IsStartTestReached(cfg *config.Config, testNumber int) bool {
	return cfg.StartTest == "" || testNumber >= cfg.StartTestNum
}

// ShouldRunTest determines if a specific test should actually be executed.
// This encapsulates the v1 scheduling logic.
func ShouldRunTest(cfg *config.Config, testName string, testNumberInAnyLoop int) bool {
	if cfg.TestingAPIsWith == "" && cfg.TestingAPIs == "" && (cfg.ReqTestNum == -1 || cfg.ReqTestNum == testNumberInAnyLoop) {
		return true
	}
	if cfg.TestingAPIsWith != "" && checkTestNameForNumber(testName, cfg.ReqTestNum) {
		return true
	}
	if cfg.TestingAPIs != "" && checkTestNameForNumber(testName, cfg.ReqTestNum) {
		return true
	}
	return false
}

// checkTestNameForNumber checks if a test filename like "test_01.json" matches a requested
// test number. Zero-alloc: extracts the number after the last "_" without regex.
func checkTestNameForNumber(testName string, reqTestNumber int) bool {
	if reqTestNumber == -1 {
		return true
	}
	// Find the last "_" to locate the number portion (e.g. "test_01.json" -> "01.json")
	idx := strings.LastIndex(testName, "_")
	if idx < 0 || idx+1 >= len(testName) {
		return false
	}
	// Extract digits after "_", skip leading zeros
	numStr := testName[idx+1:]
	// Strip file extension and any non-digit suffix
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
