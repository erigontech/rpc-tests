package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"syscall"

	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/runner"
	"github.com/erigontech/rpc-tests/internal/tools"
	"github.com/urfave/cli/v2"
)

func parseFlags(cfg *config.Config) error {
	help := flag.Bool("h", false, "print help")
	flag.BoolVar(help, "help", false, "print help")

	continueOnFail := flag.Bool("c", false, "continue on test failure")
	flag.BoolVar(continueOnFail, "continue", false, "continue on test failure")

	daemonPort := flag.Bool("I", false, "use 51515/51516 ports to server")
	flag.BoolVar(daemonPort, "daemon-port", false, "use 51515/51516 ports to server")

	externalProvider := flag.String("e", "", "verify external provider URL")
	flag.StringVar(externalProvider, "verify-external-provider", "", "verify external provider URL")

	serial := flag.Bool("S", false, "run tests in serial")
	flag.BoolVar(serial, "serial", false, "run tests in serial")

	host := flag.String("H", "localhost", "host where RpcDaemon is located")
	flag.StringVar(host, "host", "localhost", "host where RpcDaemon is located")

	testOnLatest := flag.Bool("L", false, "run only tests on latest block")
	flag.BoolVar(testOnLatest, "tests-on-latest-block", false, "run only tests on latest block")

	port := flag.Int("p", 0, "port where RpcDaemon is located")
	flag.IntVar(port, "port", 0, "port where RpcDaemon is located")

	enginePort := flag.Int("P", 0, "engine port")
	flag.IntVar(enginePort, "engine-port", 0, "engine port")

	displayOnlyFail := flag.Bool("f", false, "display only failed tests")
	flag.BoolVar(displayOnlyFail, "display-only-fail", false, "display only failed tests")

	verbose := flag.Int("v", 0, "verbose level (0-2)")
	flag.IntVar(verbose, "verbose", 0, "verbose level (0-2)")

	testNumber := flag.Int("t", -1, "run single test number")
	flag.IntVar(testNumber, "run-test", -1, "run single test number")

	startTest := flag.String("s", "", "start from test number")
	flag.StringVar(startTest, "start-from-test", "", "start from test number")

	apiListWith := flag.String("a", "", "API list with pattern")
	flag.StringVar(apiListWith, "api-list-with", "", "API list with pattern")

	apiList := flag.String("A", "", "API list exact match")
	flag.StringVar(apiList, "api-list", "", "API list exact match")

	loops := flag.Int("l", 1, "number of loops")
	flag.IntVar(loops, "loops", 1, "number of loops")

	compareErigon := flag.Bool("d", false, "compare with Erigon RpcDaemon")
	flag.BoolVar(compareErigon, "compare-erigon-rpcdaemon", false, "compare with Erigon RpcDaemon")

	jwtFile := flag.String("k", "", "JWT secret file")
	flag.StringVar(jwtFile, "jwt", "", "JWT secret file")

	createJWT := flag.String("K", "", "create JWT secret file")
	flag.StringVar(createJWT, "create-jwt", "", "create JWT secret file")

	blockchain := flag.String("b", "mainnet", "blockchain network")
	flag.StringVar(blockchain, "blockchain", "mainnet", "blockchain network")

	transportType := flag.String("T", "http", "transport type")
	flag.StringVar(transportType, "transport-type", "http", "transport type")

	excludeAPIList := flag.String("x", "", "exclude API list")
	flag.StringVar(excludeAPIList, "exclude-api-list", "", "exclude API list")

	excludeTestList := flag.String("X", "", "exclude test list")
	flag.StringVar(excludeTestList, "exclude-test-list", "", "exclude test list")

	jsonDiff := flag.Bool("j", false, "use json-diff for compare")
	flag.BoolVar(jsonDiff, "json-diff", false, "use json-diff for compare")

	waitingTime := flag.Int("w", 0, "waiting time in milliseconds")
	flag.IntVar(waitingTime, "waiting-time", 0, "waiting time in milliseconds")

	dumpResponse := flag.Bool("o", false, "dump response")
	flag.BoolVar(dumpResponse, "dump-response", false, "dump response")

	withoutCompare := flag.Bool("i", false, "without compare results")
	flag.BoolVar(withoutCompare, "without-compare-results", false, "without compare results")

	doNotCompareError := flag.Bool("E", false, "do not compare error")
	flag.BoolVar(doNotCompareError, "do-not-compare-error", false, "do not compare error")

	cpuProfile := flag.String("cpuprofile", "", "write cpu profile to file")
	memProfile := flag.String("memprofile", "", "write memory profile to file")
	traceFile := flag.String("trace", "", "write execution trace to file")

	flag.Parse()

	if *help {
		usage()
		os.Exit(0)
	}

	cfg.ExitOnFail = !*continueOnFail
	cfg.Parallel = !*serial
	cfg.VerboseLevel = *verbose
	cfg.ReqTestNum = *testNumber
	cfg.LoopNumber = *loops
	cfg.DaemonOnHost = *host
	cfg.ServerPort = *port
	cfg.EnginePort = *enginePort
	cfg.DisplayOnlyFail = *displayOnlyFail
	cfg.TestingAPIsWith = *apiListWith
	cfg.TestingAPIs = *apiList
	cfg.Net = *blockchain
	cfg.ExcludeAPIList = *excludeAPIList
	cfg.ExcludeTestList = *excludeTestList
	cfg.StartTest = *startTest
	cfg.TransportType = *transportType
	cfg.WaitingTime = *waitingTime
	cfg.ForceDumpJSONs = *dumpResponse
	cfg.WithoutCompareResults = *withoutCompare
	cfg.DoNotCompareError = *doNotCompareError
	cfg.TestsOnLatestBlock = *testOnLatest
	cfg.CpuProfile = *cpuProfile
	cfg.MemProfile = *memProfile
	cfg.TraceFile = *traceFile

	if *jsonDiff {
		cfg.DiffKind = config.JsonDiffTool
	}

	if *daemonPort {
		cfg.DaemonUnderTest = config.DaemonOnOtherPort
	}

	if *externalProvider != "" {
		cfg.DaemonAsReference = config.ExternalProvider
		cfg.ExternalProviderURL = *externalProvider
		cfg.VerifyWithDaemon = true
	}

	if *compareErigon {
		cfg.VerifyWithDaemon = true
		cfg.DaemonAsReference = config.DaemonOnDefaultPort
	}

	if *createJWT != "" {
		if err := config.GenerateJWTSecret(*createJWT, 64); err != nil {
			return fmt.Errorf("failed to create JWT secret: %w", err)
		}
		secret, err := config.GetJWTSecret(*createJWT)
		if err != nil {
			return fmt.Errorf("failed to read JWT secret: %w", err)
		}
		cfg.JWTSecret = secret
	} else if *jwtFile != "" {
		secret, err := config.GetJWTSecret(*jwtFile)
		if err != nil {
			return fmt.Errorf("secret file not found: %s", *jwtFile)
		}
		cfg.JWTSecret = secret
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	cfg.UpdateDirs()

	if err := cfg.CleanOutputDir(); err != nil {
		return err
	}

	return nil
}

func usage() {
	fmt.Println("Usage: rpc_int [options]")
	fmt.Println("")
	fmt.Println("Launch an automated sequence of RPC integration tests on target blockchain node(s)")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  -h, --help                        print this help")
	fmt.Println("  -j, --json-diff                   use json-diff tool to make compare [default: json-diff-go]")
	fmt.Println("  -f, --display-only-fail           shows only failed tests (not Skipped) [default: print all]")
	fmt.Println("  -E, --do-not-compare-error        do not compare error")
	fmt.Println("  -v, --verbose <level>             0: no message; 1: print result; 2: print request/response [default 0]")
	fmt.Println("  -c, --continue                    runs all tests even if one test fails [default: exit at first failed test]")
	fmt.Println("  -l, --loops <number>              [default loop 1]")
	fmt.Println("  -b, --blockchain <name>           [default: mainnet]")
	fmt.Println("  -s, --start-from-test <number>    run tests starting from specified test number [default starts from 1]")
	fmt.Println("  -t, --run-test <number>           run single test using global test number")
	fmt.Println("  -d, --compare-erigon-rpcdaemon    send requests also to the reference daemon e.g.: Erigon RpcDaemon")
	fmt.Println("  -T, --transport-type <type>       http,http_comp,https,websocket,websocket_comp [default http]")
	fmt.Println("  -k, --jwt <file>                  authentication token file")
	fmt.Println("  -K, --create-jwt <file>           generate authentication token file and use it")
	fmt.Println("  -a, --api-list-with <apis>        run all tests of the specified API that contains string")
	fmt.Println("  -A, --api-list <apis>             run all tests of the specified API that match full name")
	fmt.Println("  -x, --exclude-api-list <list>     exclude API list")
	fmt.Println("  -X, --exclude-test-list <list>    exclude test list")
	fmt.Println("  -o, --dump-response               dump JSON RPC response even if responses are the same")
	fmt.Println("  -H, --host <host>                 host where the RpcDaemon is located [default: localhost]")
	fmt.Println("  -p, --port <port>                 port where the RpcDaemon is located [default: 8545]")
	fmt.Println("  -I, --daemon-port                 Use 51515/51516 ports to server")
	fmt.Println("  -e, --verify-external-provider <url> send any request also to external API endpoint as reference")
	fmt.Println("  -i, --without-compare-results     send request and waits response without compare results")
	fmt.Println("  -w, --waiting-time <ms>           wait time after test execution in milliseconds")
	fmt.Println("  -S, --serial                      all tests run in serial way [default: parallel]")
	fmt.Println("  -L, --tests-on-latest-block       runs only test on latest block")
}

func runMain() int {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	cfg := config.NewConfig()
	if err := parseFlags(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		usage()
		return -1
	}

	// CPU profiling
	if cfg.CpuProfile != "" {
		f, err := os.Create(cfg.CpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not create CPU profile: %v\n", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "could not start CPU profile: %v\n", err)
		}
		defer pprof.StopCPUProfile()
	}

	// Execution tracing
	if cfg.TraceFile != "" {
		f, err := os.Create(cfg.TraceFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not create trace file: %v\n", err)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			fmt.Fprintf(os.Stderr, "could not start trace: %v\n", err)
		}
		defer trace.Stop()
	}

	// Memory profiling
	defer func() {
		if cfg.MemProfile != "" {
			f, err := os.Create(cfg.MemProfile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not create memory profile: %v\n", err)
			}
			defer f.Close()
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintf(os.Stderr, "could not write memory profile: %v\n", err)
			}
		}
	}()

	// Clean temp dirs
	if _, err := os.Stat(config.TempDirName); err == nil {
		if err := os.RemoveAll(config.TempDirName); err != nil {
			return -1
		}
	}

	ctx, cancelCtx := context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case sig := <-sigs:
				fmt.Printf("\nReceived signal: %s. Starting graceful shutdown...\n", sig)
				cancelCtx()
			case <-ctx.Done():
				return
			}
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("\nCRITICAL: TEST SEQUENCE INTERRUPTED!")
		}
	}()

	exitCode, err := runner.Run(ctx, cancelCtx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return -1
	}
	return exitCode
}

func main() {
	if len(os.Args) > 1 && tools.IsSubcommand(os.Args[1]) {
		app := &cli.App{
			Name:     "rpc_int",
			Commands: tools.Commands(),
		}
		if err := app.Run(os.Args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Exit(runMain())
}
