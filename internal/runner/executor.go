package runner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	attempts := 1
	if classifyHeads && cfg.LatestRetries > 1 {
		attempts = cfg.LatestRetries
	}

	for attempt := 1; ; attempt++ {
		var before headPair
		if classifyHeads {
			before = readHeads(ctx, cfg, descriptor, client)
		}

		runCommands(ctx, cfg, commands, descriptor, &outcome, client)

		if outcome.Success && outcome.Error == nil {
			return outcome
		}
		if !classifyHeads || ctx.Err() != nil {
			return outcome
		}

		after := readHeads(ctx, cfg, descriptor, client)
		// Heads unreadable: nothing to classify with, take the failure at face value.
		if !before.known() || !after.known() {
			outcome.Notes = append(outcome.Notes, fmt.Sprintf(
				"head unreadable, failure not classified (before: %s, after: %s)", before, after))
			attachHeadEvidence(&outcome, before, after)
			return outcome
		}
		if before.equal(after) {
			// Both nodes stayed on the same head for the whole test: the mismatch is real.
			attachHeadEvidence(&outcome, before, after)
			return outcome
		}
		if attempt >= attempts {
			outcome.Notes = append(outcome.Notes, fmt.Sprintf(
				"head moved on %d consecutive attempts, reported as failure (before: %s, after: %s)",
				attempts, before, after))
			attachHeadEvidence(&outcome, before, after)
			return outcome
		}

		// Inconclusive: the nodes did not resolve "latest" to the same block. Log only the
		// head values (not the diff, which can be hundreds of MB) and retry.
		outcome.Notes = append(outcome.Notes, fmt.Sprintf(
			"inconclusive attempt %d/%d: head moved during test (before: %s, after: %s), retrying",
			attempt, attempts, before, after))
		discardAttempt(&outcome)
	}
}

// runCommands executes the fixture's commands into outcome, resetting any verdict left by
// a previous attempt. Multi-command fixtures run sequentially and stop at the first failing
// command, so stateful sequences (e.g. commit then verify) stay ordered.
func runCommands(ctx context.Context, cfg *config.Config, commands []testdata.JsonRpcCommand, descriptor *testdata.TestDescriptor, outcome *testdata.TestOutcome, client *internalrpc.Client) {
	outcome.Error = nil
	outcome.ColoredDiff = ""
	outcome.ErrorDetails = nil
	outcome.Artifacts = nil

	for i := range commands {
		outcome.Success = false
		runCommand(ctx, cfg, &commands[i], descriptor, outcome, client)
		if !outcome.Success || outcome.Error != nil {
			return
		}
	}
}

// discardAttempt throws away the verdict and the artifacts of an inconclusive attempt.
func discardAttempt(outcome *testdata.TestOutcome) {
	for _, f := range outcome.Artifacts {
		_ = os.Remove(f)
	}
	outcome.Artifacts = nil
	outcome.Success = false
	outcome.Error = nil
	outcome.ColoredDiff = ""
	outcome.ErrorDetails = nil
}

// headPair holds the head of the node under test and of the reference node.
type headPair struct {
	underTest testdata.HeadSnapshot
	reference testdata.HeadSnapshot
}

func (p headPair) known() bool {
	return p.underTest.Error == "" && p.reference.Error == ""
}

// equal reports whether both nodes are on the same head they were on in q.
// The hash is compared as well as the number, so a tip reorg is caught too.
func (p headPair) equal(q headPair) bool {
	return p.underTest.Number == q.underTest.Number && p.underTest.Hash == q.underTest.Hash &&
		p.reference.Number == q.reference.Number && p.reference.Hash == q.reference.Hash
}

func (p headPair) String() string {
	return p.underTest.String() + " " + p.reference.String()
}

// readHeads reads the current head of both nodes concurrently, so the two values refer to
// as close to the same instant as possible.
func readHeads(ctx context.Context, cfg *config.Config, descriptor *testdata.TestDescriptor, client *internalrpc.Client) headPair {
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

	if cfg.VerboseLevel > 1 {
		fmt.Printf("%s: head %s\n", descriptor.Name, pair)
	}
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

// attachHeadEvidence records the two heads on the failure details, as the evidence that
// tells a real mismatch from one caused by the nodes tracing different blocks.
func attachHeadEvidence(outcome *testdata.TestOutcome, before, after headPair) {
	if outcome.ErrorDetails == nil {
		msg := ""
		if outcome.Error != nil {
			msg = outcome.Error.Error()
		}
		outcome.ErrorDetails = &testdata.ErrorDetails{Message: msg}
	}
	outcome.ErrorDetails.Heads = &testdata.HeadEvidence{
		Before: []testdata.HeadSnapshot{before.underTest, before.reference},
		After:  []testdata.HeadSnapshot{after.underTest, after.reference},
	}
}

// enrichErrorDetails fills in Target and Request on an existing or newly created ErrorDetails.
func enrichErrorDetails(outcome *testdata.TestOutcome, target string, request []byte) {
	if outcome.ErrorDetails == nil {
		if outcome.Error != nil {
			outcome.ErrorDetails = &testdata.ErrorDetails{Message: outcome.Error.Error()}
		} else {
			return
		}
	}
	if outcome.ErrorDetails.Target == "" {
		outcome.ErrorDetails.Target = target
	}
	if outcome.ErrorDetails.Request == nil && len(request) > 0 {
		var req any
		if err := json.Unmarshal(request, &req); err == nil {
			outcome.ErrorDetails.Request = req
		}
	}
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

		compare.ProcessResponse(result, nil, cmd.Response, cfg, outputDirName, daemonFile, expRspFile, diffFile, outcome, ignoreFields)
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

		outcome.Artifacts = append(outcome.Artifacts, daemonFile, expRspFile, diffFile)
		compare.ProcessResponse(result, result1, nil, cfg, outputDirName, daemonFile, expRspFile, diffFile, outcome, ignoreFields)
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
