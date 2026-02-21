package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
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

	// Handle latest block sync for verify mode
	if cfg.VerifyWithDaemon && cfg.TestsOnLatestBlock {
		server1 := fmt.Sprintf("%s:%d", cfg.DaemonOnHost, cfg.ServerPort)
		latestBlock, err := internalrpc.GetConsistentLatestBlock(
			cfg.VerboseLevel, server1, cfg.ExternalProviderURL, 10, 1*time.Second)
		if err != nil {
			fmt.Println("sync on latest block number failed ", err)
			return -1, err
		}
		if cfg.VerboseLevel > 0 {
			fmt.Printf("Latest block number for %s, %s: %d\n", server1, cfg.ExternalProviderURL, latestBlock)
		}
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
	globalTestNumber := 0
	stats := &Stats{}

	// Each loop iteration runs as a complete batch: all tests are scheduled,
	// workers drain the channel, results are collected, then the next iteration starts.
	for loopNum := range cfg.LoopNumber {
		if ctx.Err() != nil {
			break
		}

		if cfg.LoopNumber != 1 {
			fmt.Printf("\nTest iteration: %d\n", loopNum+1)
		}

		testsChan := make(chan *testdata.TestDescriptor, 2000)
		resultsChan := make(chan testdata.TestResult, 2000)

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
			w := bufio.NewWriterSize(os.Stdout, 64*1024)
			defer w.Flush()
			pending := make(map[int]testdata.TestResult)
			nextIndex := 0
			for {
				select {
				case result, ok := <-resultsChan:
					if !ok {
						return
					}
					pending[result.Test.Index] = result
					// Flush all consecutive results starting from nextIndex
					for {
						r, exists := pending[nextIndex]
						if !exists {
							break
						}
						delete(pending, nextIndex)
						nextIndex++
						printResult(w, &r, stats, cfg, cancelCtx)
						if cfg.ExitOnFail && stats.FailedTests > 0 {
							return
						}
					}
				case <-ctx.Done():
					return
				}
			}
		}()

		// Schedule all tests for this iteration
		scheduledIndex := 0
		transportTypes := cfg.TransportTypes()
	transportLoop:
		for _, transportType := range transportTypes {
			testNumberInAnyLoop := 1
			globalTestNumber = 0

			for _, tc := range discovery.Tests {
				if ctx.Err() != nil {
					break transportLoop
				}

				globalTestNumber = tc.Number
				currAPI := tc.APIName
				jsonTestFullName := tc.Name
				testName := strings.TrimPrefix(jsonTestFullName, currAPI+"/")
				if idx := strings.LastIndex(jsonTestFullName, "/"); idx >= 0 {
					testName = jsonTestFullName[idx+1:]
				}

				if f.APIUnderTest(currAPI, jsonTestFullName) {
					if f.IsSkipped(currAPI, jsonTestFullName, testNumberInAnyLoop) {
						if IsStartTestReached(cfg, testNumberInAnyLoop) {
							if !cfg.DisplayOnlyFail && cfg.ReqTestNum == -1 {
								file := fmt.Sprintf("%-60s", jsonTestFullName)
								tt := fmt.Sprintf("%-15s", transportType)
								fmt.Printf("%04d. %s::%s   skipped\n", testNumberInAnyLoop, tt, file)
							}
							stats.SkippedTests++
						}
					} else {
						shouldRun := ShouldRunTest(cfg, testName, testNumberInAnyLoop)

						if shouldRun && IsStartTestReached(cfg, testNumberInAnyLoop) {
							testDesc := &testdata.TestDescriptor{
								Name:          jsonTestFullName,
								Number:        testNumberInAnyLoop,
								TransportType: transportType,
								Index:         scheduledIndex,
							}
							scheduledIndex++
							select {
							case <-ctx.Done():
								break transportLoop
							case testsChan <- testDesc:
							}
							stats.ScheduledTests++

							if cfg.WaitingTime > 0 {
								time.Sleep(time.Duration(cfg.WaitingTime) * time.Millisecond)
							}
						}
					}
				}

				testNumberInAnyLoop++
			}
		}

		// Wait for this iteration to fully complete before starting the next
		close(testsChan)
		wg.Wait()
		close(resultsChan)
		resultsWg.Wait()
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
	stats.PrintSummary(elapsed, cfg.LoopNumber, availableTestedAPIs, globalTestNumber)

	if stats.FailedTests > 0 {
		return 1, nil
	}
	return 0, nil
}

func printResult(w *bufio.Writer, result *testdata.TestResult, stats *Stats, cfg *config.Config, cancelCtx context.CancelFunc) {
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
	} else {
		stats.AddFailure()
		if result.Outcome.Error != nil {
			fmt.Fprintf(w, "failed: %s\n", result.Outcome.Error.Error())
			if errors.Is(result.Outcome.Error, compare.ErrDiffMismatch) && result.Outcome.ColoredDiff != "" {
				fmt.Fprint(w, result.Outcome.ColoredDiff)
			}
		} else {
			fmt.Fprintf(w, "failed: no error\n")
		}
		if cfg.ExitOnFail {
			w.Flush()
			cancelCtx()
		}
	}
	w.Flush()
}
