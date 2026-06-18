package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/erigontech/rpc-tests/internal/compare"
	"github.com/erigontech/rpc-tests/internal/config"
	"github.com/erigontech/rpc-tests/internal/filter"
	internalrpc "github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

// Run executes the full test suite matching v1 runMain behavior.
func Run(ctx context.Context, cancelCtx context.CancelFunc, cfg *config.Config) (int, error) {
	startTime := time.Now()

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return -1, err
	}

	// Print server endpoints
	if cfg.Parallel {
		fmt.Printf("Run tests in parallel on %s\n", cfg.ServerEndpoints())
	} else {
		fmt.Printf("Run tests in serial on %s\n", cfg.ServerEndpoints())
	}

	if strings.Contains(cfg.TransportType, "_comp") {
		fmt.Println("Run tests using compression")
	}

	if err := cfg.CleanOutputDir(); err != nil {
		return -1, err
	}

	resultsAbsDir, err := cfg.ResultsAbsDir()
	if err != nil {
		return -1, err
	}
	fmt.Printf("Result directory: %s\n", resultsAbsDir)

	// Create filter
	f := filter.New(filter.FilterConfig{
		Net:                cfg.Net,
		ReqTestNum:         cfg.ReqTestNum,
		TestingAPIs:        cfg.TestingAPIs,
		TestingAPIsWith:    cfg.TestingAPIsWith,
		ExcludeAPIList:     cfg.ExcludeAPIList,
		ExcludeTestList:    cfg.ExcludeTestList,
		TestsOnLatestBlock: cfg.TestsOnLatestBlock,
		DoNotCompareError:  cfg.DoNotCompareError,
		CommitmentHistory:  cfg.CommitmentHistory,
	})

	// Discover tests
	discovery, err := testdata.DiscoverTests(cfg.JSONDir, cfg.ResultsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading directory %s: %v\n", cfg.JSONDir, err)
		return -1, err
	}

	numWorkers := 1
	if cfg.Parallel {
		numWorkers = runtime.NumCPU()
	}

	// Pre-create one RPC client per transport type (Client is goroutine-safe)
	clients := make(map[string]*internalrpc.Client)
	for _, tt := range cfg.TransportTypes() {
		clients[tt] = internalrpc.NewClient(tt, "", cfg.VerboseLevel)
	}

	availableTestedAPIs := discovery.TotalAPIs
	stats := &Stats{}

	var reportEntries []reportEntry
	var reportMu sync.Mutex

	transportTypes := cfg.TransportTypes()

	// Each loop iteration runs as a complete batch: all tests are scheduled,
	// workers drain the channel, results are collected, then the next iteration starts.
	for loopNum := range cfg.LoopNumber {
		if ctx.Err() != nil {
			break
		}

		if cfg.LoopNumber != 1 {
			fmt.Printf("\nTest iteration: %d\n", loopNum+1)
		}

		// Phase 1: collect all tests for this iteration (handles skip reporting as side-effect).
		allTests := collectTestDescriptors(ctx, discovery, f, cfg, transportTypes, stats, &reportEntries, &reportMu)

		// Phase 2: execute — batched with per-batch sync, or all at once.
		w := bufio.NewWriterSize(os.Stdout, 64*1024)
		if cfg.TestsOnLatestBlock && cfg.LatestBatchSize > 0 {
			batchSize := cfg.LatestBatchSize
			total := len(allTests)
			numBatches := (total + batchSize - 1) / batchSize
			attempt := 1
			for i := 0; i < numBatches; {
				if ctx.Err() != nil || maxFailuresReached(cfg, stats) {
					break
				}
				start := i * batchSize
				batch := allTests[start:min(start+batchSize, total)]
				fmt.Fprintf(w, "Attempt %d — Latest batch %d/%d (%d tests)\n", attempt, i+1, numBatches, len(batch))
				w.Flush()
				if cfg.VerifyWithDaemon {
					if err := syncLatestBlock(cfg); err != nil {
						return -1, err
					}
				}
				failuresBefore := stats.FailedTests
				runTestSlice(ctx, cancelCtx, batch, cfg, clients, numWorkers, stats, w, &reportEntries, &reportMu)
				if stats.FailedTests > failuresBefore {
					fmt.Fprintf(w, "Batch %d/%d had failures, restarting from batch 1\n", i+1, numBatches)
					w.Flush()
					i = 0
					attempt++
				} else {
					i++
				}
			}
		} else {
			// N=0: optional single sync then run all.
			if cfg.VerifyWithDaemon && cfg.TestsOnLatestBlock {
				if err := syncLatestBlock(cfg); err != nil {
					return -1, err
				}
			}
			runTestSlice(ctx, cancelCtx, allTests, cfg, clients, numWorkers, stats, w, &reportEntries, &reportMu)
		}
		w.Flush()
	}

	if stats.ScheduledTests == 0 && cfg.TestingAPIsWith != "" {
		fmt.Printf("WARN: API filter %s selected no tests\n", cfg.TestingAPIsWith)
	}

	if cfg.ExitOnFail && stats.FailedTests > 0 {
		fmt.Println("WARN: test sequence interrupted by failure (ExitOnFail)")
	}

	// Clean empty subfolders
	if entries, err := os.ReadDir(cfg.OutputDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			subfolder := fmt.Sprintf("%s/%s", cfg.OutputDir, entry.Name())
			if subEntries, err := os.ReadDir(subfolder); err == nil && len(subEntries) == 0 {
				_ = os.Remove(subfolder)
			}
		}
	}

	// Clean temp dir
	_ = os.RemoveAll(config.TempDirName)

	// Print summary
	elapsed := time.Since(startTime)
	stats.PrintSummary(startTime, elapsed, cfg.LoopNumber, availableTestedAPIs, discovery.TotalTests)

	reportMu.Lock()
	entries := reportEntries
	reportMu.Unlock()

	// Generate JSON report when verbose == 1 (mirrors Python behaviour)
	if cfg.VerboseLevel == 1 {
		reportFile := filepath.Join(cfg.OutputDir, "test_report.json")
		if err := generateReport(reportFile, startTime, elapsed, stats, discovery.TotalTests, availableTestedAPIs, cfg.LoopNumber, entries); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate report: %v\n", err)
		} else {
			fmt.Printf("\nJSON report generated: %s\n", reportFile)
		}
	}

	// Generate CSV summary report when -R / --report-file is specified
	if cfg.ReportFile != "" {
		if err := generateCSVReport(cfg.ReportFile, entries); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate report: %v\n", err)
		} else {
			fmt.Printf("\nReport generated: %s\n", cfg.ReportFile)
		}
	}

	if maxFailuresReached(cfg, stats) {
		fmt.Printf("\nABORTED: too many failures (%d), test sequence stopped early\n", cfg.MaxFailures)
	}

	if stats.FailedTests > 0 {
		return 1, nil
	}
	return 0, nil
}

func maxFailuresReached(cfg *config.Config, stats *Stats) bool {
	return cfg.MaxFailures > 0 && stats.FailedTests >= cfg.MaxFailures
}

// syncLatestBlock waits until both nodes agree on the latest block number.
func syncLatestBlock(cfg *config.Config) error {
	server1 := fmt.Sprintf("%s:%d", cfg.DaemonOnHost, cfg.ServerPort)
	latestBlock, err := internalrpc.GetConsistentLatestBlock(
		cfg.VerboseLevel, server1, cfg.ExternalProviderURL, 10, 1*time.Second)
	if err != nil {
		fmt.Println("sync on latest block number failed ", err)
		return err
	}
	if cfg.VerboseLevel > 0 {
		fmt.Printf("Latest block number for %s, %s: %d\n", server1, cfg.ExternalProviderURL, latestBlock)
	}
	return nil
}

// collectTestDescriptors runs the filter/skip logic over the discovered tests and returns
// descriptors ready for execution. Skip reporting and stats are handled as side-effects.
func collectTestDescriptors(
	ctx context.Context,
	discovery *testdata.DiscoveryResult,
	f *filter.TestFilter,
	cfg *config.Config,
	transportTypes []string,
	stats *Stats,
	reportEntries *[]reportEntry,
	reportMu *sync.Mutex,
) []*testdata.TestDescriptor {
	var all []*testdata.TestDescriptor

outer:
	for _, transportType := range transportTypes {
		testNumberInAnyLoop := 1
		for _, tc := range discovery.Tests {
			if ctx.Err() != nil {
				break outer
			}
			currAPI := tc.APIName
			jsonTestFullName := tc.Name
			testName := jsonTestFullName
			if idx := strings.LastIndex(jsonTestFullName, "/"); idx >= 0 {
				testName = jsonTestFullName[idx+1:]
			}

			if f.APIUnderTest(currAPI, jsonTestFullName, tc.Latest, tc.CommitmentHistory) {
				if f.IsSkipped(currAPI, jsonTestFullName, testNumberInAnyLoop) {
					if IsStartTestReached(cfg, testNumberInAnyLoop) {
						if !cfg.DisplayOnlyFail && cfg.ReqTestNum == -1 {
							file := fmt.Sprintf("%-60s", jsonTestFullName)
							tt := fmt.Sprintf("%-15s", transportType)
							fmt.Printf("%04d. %s::%s   skipped\n", testNumberInAnyLoop, tt, file)
						}
						stats.SkippedTests++
						if cfg.VerboseLevel == 1 || cfg.ReportFile != "" {
							reportMu.Lock()
							*reportEntries = append(*reportEntries, reportEntry{
								TestNumber:    testNumberInAnyLoop,
								TransportType: transportType,
								TestName:      jsonTestFullName,
								Result:        "SKIPPED",
								ErrorMessage:  "",
							})
							reportMu.Unlock()
						}
					}
				} else if ShouldRunTest(cfg, testName, testNumberInAnyLoop) && IsStartTestReached(cfg, testNumberInAnyLoop) {
					all = append(all, &testdata.TestDescriptor{
						Name:          jsonTestFullName,
						Number:        testNumberInAnyLoop,
						TransportType: transportType,
					})
					stats.ScheduledTests++
				}
			}
			testNumberInAnyLoop++
		}
	}
	return all
}

// runTestSlice executes a slice of tests through the worker pool and blocks until all complete.
// It re-indexes tests from 0 for ordered output within the slice.
func runTestSlice(
	ctx context.Context,
	cancelCtx context.CancelFunc,
	tests []*testdata.TestDescriptor,
	cfg *config.Config,
	clients map[string]*internalrpc.Client,
	numWorkers int,
	stats *Stats,
	w *bufio.Writer,
	reportEntries *[]reportEntry,
	reportMu *sync.Mutex,
) {
	if len(tests) == 0 {
		return
	}
	for i, td := range tests {
		td.Index = i
	}

	bufSize := min(len(tests), 2000)
	testsChan := make(chan *testdata.TestDescriptor, bufSize)
	resultsChan := make(chan testdata.TestResult, bufSize)

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case test := <-testsChan:
					if test == nil {
						return
					}
					testOutcome := RunTest(ctx, test, cfg, clients[test.TransportType])
					resultsChan <- testdata.TestResult{Outcome: testOutcome, Test: test}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	var resultsWg sync.WaitGroup
	resultsWg.Add(1)
	go func() {
		defer resultsWg.Done()
		pending := make(map[int]testdata.TestResult)
		nextIndex := 0
		for {
			select {
			case result, ok := <-resultsChan:
				if !ok {
					return
				}
				pending[result.Test.Index] = result
				for {
					r, exists := pending[nextIndex]
					if !exists {
						break
					}
					delete(pending, nextIndex)
					nextIndex++
					printResult(w, &r, stats, cfg, cancelCtx, reportEntries, reportMu)
					if cfg.ExitOnFail && stats.FailedTests > 0 {
						return
					}
					if maxFailuresReached(cfg, stats) {
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

schedLoop:
	for _, td := range tests {
		select {
		case <-ctx.Done():
			break schedLoop
		case testsChan <- td:
		}
		if cfg.WaitingTime > 0 {
			time.Sleep(time.Duration(cfg.WaitingTime) * time.Millisecond)
		}
	}

	close(testsChan)
	wg.Wait()
	close(resultsChan)
	resultsWg.Wait()
	fmt.Fprintln(w)
}

func printResult(w *bufio.Writer, result *testdata.TestResult, stats *Stats, cfg *config.Config, cancelCtx context.CancelFunc, reportEntries *[]reportEntry, reportMu *sync.Mutex) {
	file := fmt.Sprintf("%-60s", result.Test.Name)
	tt := fmt.Sprintf("%-15s", result.Test.TransportType)
	fmt.Fprintf(w, "%04d. %s::%s   ", result.Test.Number, tt, file)

	if result.Outcome.Success {
		stats.AddSuccess(result.Outcome.Metrics)
		if cfg.VerboseLevel > 0 {
			fmt.Fprintln(w, "OK")
		} else {
			fmt.Fprint(w, "OK\r")
		}
		if cfg.VerboseLevel == 1 || cfg.ReportFile != "" {
			reportMu.Lock()
			*reportEntries = append(*reportEntries, reportEntry{
				TestNumber:    result.Test.Number,
				TransportType: result.Test.TransportType,
				TestName:      result.Test.Name,
				Result:        "OK",
				ErrorMessage:  "",
			})
			reportMu.Unlock()
		}
	} else {
		stats.AddFailure()
		errMsg := "no error"
		if result.Outcome.Error != nil {
			errMsg = result.Outcome.Error.Error()
			fmt.Fprintf(w, "failed: %s\n", errMsg)
			if errors.Is(result.Outcome.Error, compare.ErrDiffMismatch) && result.Outcome.ColoredDiff != "" {
				fmt.Fprint(w, result.Outcome.ColoredDiff)
			}
		} else {
			fmt.Fprintf(w, "failed: %s\n", errMsg)
		}
		if cfg.VerboseLevel == 1 || cfg.ReportFile != "" {
			var errField any = errMsg
			if result.Outcome.ErrorDetails != nil {
				errField = result.Outcome.ErrorDetails
			}
			reportMu.Lock()
			*reportEntries = append(*reportEntries, reportEntry{
				TestNumber:    result.Test.Number,
				TransportType: result.Test.TransportType,
				TestName:      result.Test.Name,
				Result:        "FAILED",
				ErrorMessage:  errField,
			})
			reportMu.Unlock()
		}
		if maxFailuresReached(cfg, stats) {
			fmt.Fprintf(w, "\nABORTED: too many failures (%d), test sequence stopped early\n", cfg.MaxFailures)
			w.Flush()
			cancelCtx()
		} else if cfg.ExitOnFail {
			w.Flush()
			cancelCtx()
		}
	}
	w.Flush()
}
