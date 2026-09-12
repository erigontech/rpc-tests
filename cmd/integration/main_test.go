package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/tools"
)

// parseArgs runs parseFlags against a fresh flag set and the given argv,
// returning the populated config. parseFlags reads the package-level
// flag.CommandLine and os.Args, so both are swapped out for the call.
func parseArgs(t *testing.T, args ...string) (*config.Config, error) {
	t.Helper()
	originalArgs := os.Args
	originalFlags := flag.CommandLine
	t.Cleanup(func() {
		os.Args = originalArgs
		flag.CommandLine = originalFlags
	})

	flag.CommandLine = flag.NewFlagSet("rpc_int", flag.ContinueOnError)
	flag.CommandLine.SetOutput(os.NewFile(0, os.DevNull))
	os.Args = append([]string{"rpc_int"}, args...)

	cfg := config.NewConfig()
	return cfg, parseFlags(cfg)
}

func TestParseFlagsDefaults(t *testing.T) {
	cfg, err := parseArgs(t)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.ExitOnFail {
		t.Error("ExitOnFail should default to true (no -c)")
	}
	if !cfg.Parallel {
		t.Error("Parallel should default to true (no -S)")
	}
	if cfg.Net != "mainnet" {
		t.Errorf("Net: got %q, want mainnet", cfg.Net)
	}
	if cfg.TransportType != "http" {
		t.Errorf("TransportType: got %q, want http", cfg.TransportType)
	}
	if cfg.ReqTestNum != -1 {
		t.Errorf("ReqTestNum: got %d, want -1", cfg.ReqTestNum)
	}
	if cfg.LoopNumber != 1 {
		t.Errorf("LoopNumber: got %d, want 1", cfg.LoopNumber)
	}
	if cfg.DiffKind != config.JsonDiffGo {
		t.Errorf("DiffKind: got %v, want JsonDiffGo", cfg.DiffKind)
	}
	// UpdateDirs runs at the end of parseFlags.
	if cfg.JSONDir != "./integration/mainnet/" {
		t.Errorf("JSONDir: got %q", cfg.JSONDir)
	}
	if cfg.OutputDir != "./integration/mainnet/results/" {
		t.Errorf("OutputDir: got %q", cfg.OutputDir)
	}
	if cfg.ServerPort != 8545 {
		t.Errorf("ServerPort: got %d, want 8545", cfg.ServerPort)
	}
}

func TestParseFlagsShortForms(t *testing.T) {
	cfg, err := parseArgs(t,
		"-c", "-f", "-S", "-v", "2",
		"-b", "sepolia",
		"-H", "node1", "-p", "9545", "-P", "9551",
		"-A", "eth_call,eth_getLogs",
		"-x", "debug_traceCall",
		"-X", "3,4",
		"-l", "5",
		"-w", "10",
		"-o", "-i", "-E", "-C",
		"-M", "7",
		"-s", "12",
		"-R", "summary.csv",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ExitOnFail", cfg.ExitOnFail, false},
		{"DisplayOnlyFail", cfg.DisplayOnlyFail, true},
		{"Parallel", cfg.Parallel, false},
		{"VerboseLevel", cfg.VerboseLevel, 2},
		{"Net", cfg.Net, "sepolia"},
		{"DaemonOnHost", cfg.DaemonOnHost, "node1"},
		{"ServerPort", cfg.ServerPort, 9545},
		{"EnginePort", cfg.EnginePort, 9551},
		{"TestingAPIs", cfg.TestingAPIs, "eth_call,eth_getLogs"},
		{"ExcludeAPIList", cfg.ExcludeAPIList, "debug_traceCall"},
		{"ExcludeTestList", cfg.ExcludeTestList, "3,4"},
		{"LoopNumber", cfg.LoopNumber, 5},
		{"WaitingTime", cfg.WaitingTime, 10},
		{"ForceDumpJSONs", cfg.ForceDumpJSONs, true},
		{"WithoutCompareResults", cfg.WithoutCompareResults, true},
		{"DoNotCompareError", cfg.DoNotCompareError, true},
		{"CommitmentHistory", cfg.CommitmentHistory, true},
		{"MaxFailures", cfg.MaxFailures, 7},
		{"StartTest", cfg.StartTest, "12"},
		{"StartTestNum", cfg.StartTestNum, 12},
		{"ReportFile", cfg.ReportFile, "summary.csv"},
		{"JSONDir", cfg.JSONDir, "./integration/sepolia/"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestParseFlagsLongForms checks that every short flag has a working long alias.
func TestParseFlagsLongForms(t *testing.T) {
	cfg, err := parseArgs(t,
		"--continue", "--display-only-fail", "--serial",
		"--verbose", "1",
		"--blockchain", "gnosis",
		"--host", "node2", "--port", "7545",
		"--api-list-with", "eth_",
		"--loops", "2",
		"--transport-type", "websocket",
		"--max-failures", "3",
		"--report-file", "out.csv",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ExitOnFail || !cfg.DisplayOnlyFail || cfg.Parallel {
		t.Errorf("boolean long forms not applied: %+v", cfg)
	}
	if cfg.Net != "gnosis" || cfg.DaemonOnHost != "node2" || cfg.ServerPort != 7545 {
		t.Errorf("target long forms not applied: net=%q host=%q port=%d",
			cfg.Net, cfg.DaemonOnHost, cfg.ServerPort)
	}
	if cfg.TestingAPIsWith != "eth_" {
		t.Errorf("TestingAPIsWith: got %q", cfg.TestingAPIsWith)
	}
	if cfg.TransportType != "websocket" {
		t.Errorf("TransportType: got %q", cfg.TransportType)
	}
}

func TestParseFlagsSingleTestNumber(t *testing.T) {
	cfg, err := parseArgs(t, "-t", "246")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReqTestNum != 246 {
		t.Errorf("ReqTestNum: got %d, want 246", cfg.ReqTestNum)
	}
}

func TestParseFlagsLatestBlockOptions(t *testing.T) {
	cfg, err := parseArgs(t, "-L", "-N", "25")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.TestsOnLatestBlock {
		t.Error("TestsOnLatestBlock should be set by -L")
	}
	if cfg.LatestBatchSize != 25 {
		t.Errorf("LatestBatchSize: got %d, want 25", cfg.LatestBatchSize)
	}
}

func TestParseFlagsDaemonPort(t *testing.T) {
	cfg, err := parseArgs(t, "-I")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DaemonUnderTest != config.DaemonOnOtherPort {
		t.Errorf("DaemonUnderTest: got %v, want DaemonOnOtherPort", cfg.DaemonUnderTest)
	}
}

func TestParseFlagsExternalProvider(t *testing.T) {
	cfg, err := parseArgs(t, "-e", "https://provider.example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ExternalProviderURL != "https://provider.example" {
		t.Errorf("ExternalProviderURL: got %q", cfg.ExternalProviderURL)
	}
	if cfg.DaemonAsReference != config.ExternalProvider {
		t.Errorf("DaemonAsReference: got %v, want ExternalProvider", cfg.DaemonAsReference)
	}
	if !cfg.VerifyWithDaemon {
		t.Error("VerifyWithDaemon should be enabled by -e")
	}
}

func TestParseFlagsCompareErigon(t *testing.T) {
	cfg, err := parseArgs(t, "-d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.VerifyWithDaemon {
		t.Error("VerifyWithDaemon should be enabled by -d")
	}
	if cfg.DaemonAsReference != config.DaemonOnDefaultPort {
		t.Errorf("DaemonAsReference: got %v, want DaemonOnDefaultPort", cfg.DaemonAsReference)
	}
}

func TestParseFlagsDiffKind(t *testing.T) {
	tests := []struct {
		value string
		want  config.DiffKind
	}{
		{"json-diff-go", config.JsonDiffGo},
		{"json-diff", config.JsonDiffTool},
		{"diff", config.DiffTool},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cfg, err := parseArgs(t, "-j", tt.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.DiffKind != tt.want {
				t.Errorf("DiffKind: got %v, want %v", cfg.DiffKind, tt.want)
			}
		})
	}
}

func TestParseFlagsInvalidDiffKind(t *testing.T) {
	_, err := parseArgs(t, "-j", "no-such-tool")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid DiffKind") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseFlagsInvalidTransport(t *testing.T) {
	_, err := parseArgs(t, "-T", "carrier-pigeon")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid connection type") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestParseFlagsRejectsConflictingOptions checks that Validate is reached from
// parseFlags, using one of the mutually exclusive combinations it rejects.
func TestParseFlagsRejectsConflictingOptions(t *testing.T) {
	_, err := parseArgs(t, "-t", "5", "-X", "7")
	if err == nil {
		t.Error("expected -t and -X to be rejected as mutually exclusive")
	}
}

func TestParseFlagsJWTFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jwt.hex")
	secret := strings.Repeat("ab", 32)
	if err := os.WriteFile(path, []byte("0x"+secret), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := parseArgs(t, "-k", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JWTSecret != secret {
		t.Errorf("JWTSecret: got %q, want %q", cfg.JWTSecret, secret)
	}
}

func TestParseFlagsMissingJWTFile(t *testing.T) {
	_, err := parseArgs(t, "-k", filepath.Join(t.TempDir(), "absent.hex"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "secret file not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseFlagsCreateJWTFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-jwt.hex")

	cfg, err := parseArgs(t, "-K", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("the secret file was not created: %v", statErr)
	}
	if len(cfg.JWTSecret) != 64 {
		t.Errorf("JWTSecret length: got %d, want 64 hex characters", len(cfg.JWTSecret))
	}
}

func TestParseFlagsCreateJWTFileUnwritablePath(t *testing.T) {
	_, err := parseArgs(t, "-K", filepath.Join(t.TempDir(), "missing-dir", "jwt.hex"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "failed to create JWT secret") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseFlagsProfilingOptions(t *testing.T) {
	cfg, err := parseArgs(t,
		"-cpuprofile", "cpu.out",
		"-memprofile", "mem.out",
		"-trace", "trace.out",
		"-sync-retries", "42",
		"-latest-retries", "3",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CpuProfile != "cpu.out" || cfg.MemProfile != "mem.out" || cfg.TraceFile != "trace.out" {
		t.Errorf("profile paths not applied: %+v", cfg)
	}
	if cfg.SyncRetries != 42 {
		t.Errorf("SyncRetries: got %d, want 42", cfg.SyncRetries)
	}
	if cfg.LatestRetries != 3 {
		t.Errorf("LatestRetries: got %d, want 3", cfg.LatestRetries)
	}
}

// TestSubcommandDispatchDoesNotClashWithFlags guards the dispatch rule at the
// top of main: os.Args[1] is treated as a subcommand only when it names one.
func TestSubcommandDispatchDoesNotClashWithFlags(t *testing.T) {
	for _, arg := range []string{"-c", "-f", "--help", "-A", "-t"} {
		if tools.IsSubcommand(arg) {
			t.Errorf("%q must not be treated as a subcommand", arg)
		}
	}
	for _, name := range []string{"graphql", "replay-tx", "scan-block-receipts"} {
		if !tools.IsSubcommand(name) {
			t.Errorf("%q should be recognised as a subcommand", name)
		}
	}
}

func TestUsageMentionsKeyFlags(t *testing.T) {
	output := captureStdout(t, usage)
	for _, want := range []string{"rpc_int", "-c", "-f", "-A", "-b", "-T"} {
		if !strings.Contains(output, want) {
			t.Errorf("usage output is missing %q", want)
		}
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			sb.Write(buf[:n])
			if readErr != nil {
				break
			}
		}
		done <- sb.String()
	}()

	fn()
	os.Stdout = original
	w.Close()
	output := <-done
	r.Close()
	return output
}
