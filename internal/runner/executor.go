package runner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/erigontech/rpc-tests/internal/compare"
	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/filter"
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

	if len(commands) == 0 {
		outcome.Error = errors.New("expected at least one JSON RPC command in " + jsonFilename)
		return outcome
	}

	// A test on "latest" is classified on failure rather than failed outright: each node
	// resolves the tag on its own, so a head moving under the test makes the two responses
	// incomparable. Only relevant when there is a second node to compare against.
	classifyHeads := descriptor.Latest && cfg.VerifyWithDaemon && !cfg.WithoutCompareResults && cfg.LatestRetries > 0

	for attempt := 1; ; attempt++ {
		var before, after headPair
		var haveAfter bool
		var notComparable func() bool

		if classifyHeads {
			before = readHeads(ctx, cfg, client)
			// The last attempt runs ungated, so a genuine failure still gets its full diff;
			// the earlier ones are dropped as soon as the heads say they cannot be compared.
			if attempt < cfg.LatestRetries {
				notComparable = func() bool {
					after, haveAfter = readHeads(ctx, cfg, client), true
					return before.known() && after.known() && before != after
				}
			}
		}

		runCommands(ctx, cfg, commands, descriptor, &outcome, client, notComparable)

		switch {
		case outcome.Inconclusive:
			// The nodes did not resolve "latest" to the same block: nothing was compared or
			// written, only the two heads are logged, and the test is retried.
			outcome.Notes = append(outcome.Notes, fmt.Sprintf(
				"inconclusive attempt %d/%d: head moved during test (%s -> %s), retrying",
				attempt, cfg.LatestRetries, before, after))
			continue
		case outcome.Success && outcome.Error == nil:
			return outcome
		case !classifyHeads || ctx.Err() != nil:
			return outcome
		}

		// The failure is final: judge it against the heads and attach them as evidence.
		if !haveAfter {
			after = readHeads(ctx, cfg, client)
		}
		switch {
		case !before.known() || !after.known():
			outcome.Notes = append(outcome.Notes, "head unreadable, failure not classified")
		case before != after:
			outcome.Notes = append(outcome.Notes, fmt.Sprintf(
				"head moved during test on all %d attempts, reported as failure", attempt))
		}
		ensureErrorDetails(&outcome).Heads = &testdata.HeadEvidence{
			Before: []testdata.HeadSnapshot{before.underTest, before.reference},
			After:  []testdata.HeadSnapshot{after.underTest, after.reference},
		}
		return outcome
	}
}

// runCommands executes the fixture's commands into outcome, resetting any verdict left by
// a previous attempt. Multi-command fixtures run sequentially and stop at the first failing
// command, so stateful sequences (e.g. commit then verify) stay ordered.
func runCommands(ctx context.Context, cfg *config.Config, commands []testdata.JsonRpcCommand, descriptor *testdata.TestDescriptor, outcome *testdata.TestOutcome, client *internalrpc.Client, notComparable func() bool) {
	outcome.Error = nil
	outcome.ColoredDiff = ""
	outcome.ErrorDetails = nil
	outcome.Inconclusive = false

	for i := range commands {
		outcome.Success = false
		runCommand(ctx, cfg, &commands[i], descriptor, outcome, client, notComparable)
		if !outcome.Success || outcome.Error != nil {
			return
		}
	}
}

// headPair holds the head of the node under test and of the reference node. It is comparable,
// so two pairs are compared with ==: on number and hash, which catches a tip reorg too.
type headPair struct {
	underTest testdata.HeadSnapshot
	reference testdata.HeadSnapshot
}

func (p headPair) known() bool {
	return p.underTest.Error == "" && p.reference.Error == ""
}

func (p headPair) String() string {
	return formatHeads([]testdata.HeadSnapshot{p.underTest, p.reference})
}

// readHeads reads the current head of both nodes concurrently, so the two values refer to
// as close to the same instant as possible.
func readHeads(ctx context.Context, cfg *config.Config, client *internalrpc.Client) headPair {
	// Heads always come from the eth endpoint, even for engine_ tests.
	underTest := cfg.GetTarget(config.DaemonOnDefaultPort, "eth_getBlockByNumber")
	reference := cfg.GetTarget(cfg.DaemonAsReference, "eth_getBlockByNumber")

	var pair headPair
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pair.reference = readHead(ctx, client, reference)
	}()
	pair.underTest = readHead(ctx, client, underTest)
	wg.Wait()
	return pair
}

func readHead(ctx context.Context, client *internalrpc.Client, target string) testdata.HeadSnapshot {
	snapshot := testdata.HeadSnapshot{Target: target}
	head, _, err := internalrpc.GetLatestHead(ctx, client, target)
	if err != nil {
		snapshot.Error = err.Error()
		return snapshot
	}
	snapshot.Number, snapshot.Hash = head.Number, head.Hash
	return snapshot
}

// ensureErrorDetails returns the failure details of the outcome, creating them if the failure
// carried none yet.
func ensureErrorDetails(outcome *testdata.TestOutcome) *testdata.ErrorDetails {
	if outcome.ErrorDetails == nil {
		message := ""
		if outcome.Error != nil {
			message = outcome.Error.Error()
		}
		outcome.ErrorDetails = &testdata.ErrorDetails{Message: message}
	}
	return outcome.ErrorDetails
}

// enrichErrorDetails fills in Target and Request on an existing or newly created ErrorDetails.
func enrichErrorDetails(outcome *testdata.TestOutcome, target string, request []byte) {
	if outcome.ErrorDetails == nil && outcome.Error == nil {
		return
	}
	details := ensureErrorDetails(outcome)
	if details.Target == "" {
		details.Target = target
	}
	if outcome.ErrorDetails.Request == nil && len(request) > 0 {
		var req any
		if err := json.Unmarshal(request, &req); err == nil {
			outcome.ErrorDetails.Request = req
		}
	}
}

// runCommand executes a single JSON-RPC command against the target.
func runCommand(ctx context.Context, cfg *config.Config, cmd *testdata.JsonRpcCommand, descriptor *testdata.TestDescriptor, outcome *testdata.TestOutcome, baseClient *internalrpc.Client, notComparable func() bool) {
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

	var ignoreFields []string
	if cmd.Metadata != nil {
		ignoreFields = cmd.Metadata.IgnoreFields
	}

	if !cfg.VerifyWithDaemon {
		var result any
		metrics, err := client.Call(ctx, target, request, &result)
		outcome.Metrics.RoundTripTime += metrics.RoundTripTime
		outcome.Metrics.UnmarshallingTime += metrics.UnmarshallingTime
		if err != nil {
			outcome.Error = err
			enrichErrorDetails(outcome, target, request)
			return
		}
		if cfg.VerboseLevel > 2 {
			fmt.Printf("%s: [%v]\n", cfg.DaemonUnderTest, result)
		}

		compare.ProcessResponse(result, nil, cmd.Response, cfg, outputDirName, daemonFile, expRspFile, diffFile, outcome, ignoreFields, nil)
		if !outcome.Success {
			enrichErrorDetails(outcome, target, request)
		}
	} else {
		target = cfg.GetTarget(config.DaemonOnDefaultPort, descriptor.Name)
		target1 := cfg.GetTarget(cfg.DaemonAsReference, descriptor.Name)

		// Both requests are dispatched concurrently: a request resolving "latest" is resolved
		// by each node when it arrives, so a sequential dispatch would let the head advance by
		// the whole duration of the first response.
		var result, result1 any
		var metrics, metrics1 internalrpc.Metrics
		var err, err1 error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			metrics1, err1 = client.Call(ctx, target1, request, &result1)
		}()
		metrics, err = client.Call(ctx, target, request, &result)
		wg.Wait()

		outcome.Metrics.RoundTripTime += metrics.RoundTripTime + metrics1.RoundTripTime
		outcome.Metrics.UnmarshallingTime += metrics.UnmarshallingTime + metrics1.UnmarshallingTime

		if err != nil {
			outcome.Error = err
			enrichErrorDetails(outcome, target, request)
			return
		}
		if err1 != nil {
			outcome.Error = err1
			enrichErrorDetails(outcome, target1, request)
			return
		}
		if cfg.VerboseLevel > 2 {
			fmt.Printf("%s: [%v]\n", cfg.DaemonUnderTest, result)
			fmt.Printf("%s: [%v]\n", cfg.DaemonAsReference, result1)
		}

		daemonFile = outputAPIFilename + config.GetJSONFilenameExt(config.DaemonOnDefaultPort, target)
		expRspFile = outputAPIFilename + config.GetJSONFilenameExt(cfg.DaemonAsReference, target1)

		compare.ProcessResponse(result, result1, nil, cfg, outputDirName, daemonFile, expRspFile, diffFile, outcome, ignoreFields, notComparable)
		if !outcome.Success {
			enrichErrorDetails(outcome, target, request)
		}
	}
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
	if cfg.TestingAPIsWith != "" && filter.CheckTestNameForNumber(testName, cfg.ReqTestNum) {
		return true
	}
	if cfg.TestingAPIs != "" && filter.CheckTestNameForNumber(testName, cfg.ReqTestNum) {
		return true
	}
	return false
}
