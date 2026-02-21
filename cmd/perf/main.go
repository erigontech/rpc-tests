package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/erigontech/rpc-tests/internal/perf"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "rpc_perf",
		Usage: "Launch an automated sequence of RPC performance tests on target blockchain node(s)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "disable-http-compression", Aliases: []string{"O"}, Usage: "Disable Http compression"},
			&cli.BoolFlag{Name: "not-verify-server-alive", Aliases: []string{"Z"}, Usage: "Don't verify server is still active"},
			&cli.BoolFlag{Name: "tmp-test-report", Aliases: []string{"R"}, Usage: "Generate report in tmp directory"},
			&cli.BoolFlag{Name: "test-report", Aliases: []string{"u"}, Usage: "Generate report in reports area ready for Git repo"},
			&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Enable verbose output"},
			&cli.BoolFlag{Name: "tracing", Aliases: []string{"x"}, Usage: "Enable verbose and tracing output"},
			&cli.BoolFlag{Name: "empty-cache", Aliases: []string{"e"}, Usage: "Empty OS cache before each test"},
			&cli.StringFlag{Name: "max-connections", Aliases: []string{"C"}, Value: perf.DefaultMaxConn, Usage: "Maximum number of connections"},
			&cli.StringFlag{Name: "testing-client", Aliases: []string{"D"}, Value: perf.DefaultClientName, Usage: "Name of testing client"},
			&cli.StringFlag{Name: "blockchain", Aliases: []string{"b"}, Value: "mainnet", Usage: "Blockchain network name"},
			&cli.StringFlag{Name: "test-type", Aliases: []string{"y"}, Value: perf.DefaultTestType, Usage: "Test type (e.g., eth_call, eth_getLogs)"},
			&cli.StringFlag{Name: "pattern-file", Aliases: []string{"p"}, Value: perf.DefaultVegetaPatternTarFile, Usage: "Path to the Vegeta attack pattern file"},
			&cli.IntFlag{Name: "repetitions", Aliases: []string{"r"}, Value: perf.DefaultRepetitions, Usage: "Number of repetitions for each test in sequence"},
			&cli.StringFlag{Name: "test-sequence", Aliases: []string{"t"}, Value: perf.DefaultTestSequence, Usage: "Test sequence as qps:duration,..."},
			&cli.IntFlag{Name: "wait-after-test-sequence", Aliases: []string{"w"}, Value: perf.DefaultWaitingTime, Usage: "Wait time between test iterations in seconds"},
			&cli.StringFlag{Name: "rpc-client-address", Aliases: []string{"d"}, Value: perf.DefaultServerAddress, Usage: "Client address"},
			&cli.StringFlag{Name: "client-build-dir", Aliases: []string{"g"}, Value: perf.DefaultClientBuildDir, Usage: "Path to Client build folder"},
			&cli.StringFlag{Name: "run-vegeta-on-core", Aliases: []string{"c"}, Value: perf.DefaultClientVegetaOnCore, Usage: "Taskset format for Vegeta"},
			&cli.StringFlag{Name: "response-timeout", Aliases: []string{"T"}, Value: perf.DefaultVegetaResponseTimeout, Usage: "Vegeta response timeout"},
			&cli.StringFlag{Name: "max-body-rsp", Aliases: []string{"M"}, Value: perf.DefaultMaxBodyRsp, Usage: "Max bytes to read from response bodies"},
			&cli.StringFlag{Name: "json-report", Aliases: []string{"j"}, Usage: "Generate JSON report at specified path"},
			&cli.BoolFlag{Name: "more-percentiles", Aliases: []string{"P"}, Usage: "Print more percentiles in console report"},
			&cli.BoolFlag{Name: "halt-on-vegeta-error", Aliases: []string{"H"}, Usage: "Consider test failed if Vegeta reports any error"},
			&cli.BoolFlag{Name: "instant-report", Aliases: []string{"I"}, Usage: "Print instant Vegeta report for each test"},
		},
		Action: runPerfTests,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func runPerfTests(c *cli.Context) error {
	fmt.Println("Performance Test started")

	cfg := perf.NewConfig()

	cfg.DisableHttpCompression = c.Bool("disable-http-compression")
	cfg.CheckServerAlive = !c.Bool("not-verify-server-alive")
	cfg.CreateTestReport = c.Bool("tmp-test-report") || c.Bool("test-report")
	cfg.VersionedTestReport = c.Bool("test-report")
	cfg.Verbose = c.Bool("verbose") || c.Bool("tracing")
	cfg.Tracing = c.Bool("tracing")
	cfg.EmptyCache = c.Bool("empty-cache")

	cfg.MaxConnection = c.String("max-connections")
	cfg.TestingClient = c.String("testing-client")
	cfg.ChainName = c.String("blockchain")
	cfg.TestType = c.String("test-type")
	cfg.VegetaPatternTarFile = c.String("pattern-file")
	cfg.Repetitions = c.Int("repetitions")
	cfg.TestSequence = c.String("test-sequence")
	cfg.WaitingTime = c.Int("wait-after-test-sequence")
	cfg.ClientAddress = c.String("rpc-client-address")
	cfg.ClientBuildDir = c.String("client-build-dir")
	cfg.ClientVegetaOnCore = c.String("run-vegeta-on-core")
	cfg.VegetaResponseTimeout = c.String("response-timeout")
	cfg.MaxBodyRsp = c.String("max-body-rsp")
	cfg.JSONReportFile = c.String("json-report")
	cfg.MorePercentiles = c.Bool("more-percentiles")
	cfg.HaltOnVegetaError = c.Bool("halt-on-vegeta-error")
	cfg.InstantReport = c.Bool("instant-report")

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	sequence, err := perf.ParseTestSequence(cfg.TestSequence)
	if err != nil {
		return fmt.Errorf("failed to parse test sequence: %w", err)
	}

	dirs := perf.NewRunDirs()
	testReport := perf.NewTestReport(cfg, dirs)

	perfTest, err := perf.NewPerfTest(cfg, testReport, dirs)
	if err != nil {
		return fmt.Errorf("failed to initialize performance test: %w", err)
	}
	defer func() {
		if err := perfTest.Cleanup(false); err != nil {
			log.Printf("Failed to cleanup: %v", err)
		}
	}()

	fmt.Printf("Test repetitions: %d on sequence: %s for pattern: %s\n",
		cfg.Repetitions, cfg.TestSequence, cfg.VegetaPatternTarFile)

	if cfg.CreateTestReport {
		if err := testReport.Open(); err != nil {
			return fmt.Errorf("failed to open test report: %w", err)
		}
		defer func() {
			if err := testReport.Close(); err != nil {
				log.Printf("Failed to close test report: %v", err)
			}
		}()
	}

	ctx := context.Background()

	if err := perfTest.ExecuteSequence(ctx, sequence, cfg.TestingClient); err != nil {
		fmt.Printf("Performance Test failed, error: %v\n", err)
		return err
	}

	fmt.Println("Performance Test completed successfully.")
	return nil
}
