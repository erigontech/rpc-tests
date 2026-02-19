package perf

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// VegetaTarget represents a single HTTP request target for Vegeta.
type VegetaTarget struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Body   []byte              `json:"body,omitempty"`
	Header map[string][]string `json:"header,omitempty"`
}

// PerfTest manages performance test execution.
type PerfTest struct {
	Config  *Config
	Report  *TestReport
	RunDirs *RunDirs
}

// NewPerfTest creates a new performance test instance.
func NewPerfTest(config *Config, report *TestReport, dirs *RunDirs) (*PerfTest, error) {
	pt := &PerfTest{
		Config:  config,
		Report:  report,
		RunDirs: dirs,
	}

	if err := pt.Cleanup(true); err != nil {
		return nil, fmt.Errorf("initial cleanup failed: %w", err)
	}

	if err := pt.CopyAndExtractPatternFile(); err != nil {
		return nil, fmt.Errorf("failed to setup pattern file: %w", err)
	}

	return pt, nil
}

// Cleanup removes temporary files.
func (pt *PerfTest) Cleanup(initial bool) error {
	filesToRemove := []string{
		pt.RunDirs.TarFileName,
		"perf.data.old",
		"perf.data",
	}

	for _, fileName := range filesToRemove {
		_, err := os.Stat(fileName)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Remove(fileName); err != nil {
			return err
		}
	}

	if err := os.RemoveAll(pt.RunDirs.PatternDir); err != nil {
		return err
	}

	if initial {
		if err := os.RemoveAll(pt.RunDirs.RunTestDir); err != nil {
			return err
		}
	} else {
		_ = os.Remove(pt.RunDirs.RunTestDir)
	}

	return nil
}

// CopyAndExtractPatternFile copies and extracts the vegeta pattern tar file.
func (pt *PerfTest) CopyAndExtractPatternFile() error {
	if _, err := os.Stat(pt.Config.VegetaPatternTarFile); os.IsNotExist(err) {
		return fmt.Errorf("invalid pattern file: %s", pt.Config.VegetaPatternTarFile)
	}

	if err := os.MkdirAll(pt.RunDirs.RunTestDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	if err := copyFile(pt.Config.VegetaPatternTarFile, pt.RunDirs.TarFileName); err != nil {
		return fmt.Errorf("failed to copy pattern file: %w", err)
	}

	if pt.Config.Tracing {
		fmt.Printf("Copy Vegeta pattern: %s -> %s\n", pt.Config.VegetaPatternTarFile, pt.RunDirs.TarFileName)
	}

	if err := extractTarGz(pt.RunDirs.TarFileName, pt.RunDirs.RunTestDir); err != nil {
		return fmt.Errorf("failed to extract pattern file: %w", err)
	}

	if pt.Config.Tracing {
		fmt.Printf("Extracting Vegeta pattern to: %s\n", pt.RunDirs.RunTestDir)
	}

	if pt.Config.ClientAddress != "localhost" {
		patternFile := pt.RunDirs.PatternBase + pt.Config.TestType + ".txt"
		if err := replaceInFile(patternFile, "localhost", pt.Config.ClientAddress); err != nil {
			log.Printf("Warning: failed to replace address in pattern: %v", err)
		}
	}

	return nil
}

// Execute runs a single performance test.
func (pt *PerfTest) Execute(ctx context.Context, testNumber, repetition int, name string, qps, duration int, format ResultFormat) error {
	if pt.Config.EmptyCache {
		if err := EmptyOSCache(); err != nil {
			log.Printf("Warning: failed to empty cache: %v", err)
		}
	}

	pattern := pt.RunDirs.PatternBase + pt.Config.TestType + ".txt"

	timestamp := time.Now().Format("20060102150405")
	pt.Config.BinaryFile = fmt.Sprintf("%s_%s_%s_%s_%d_%d_%d.bin",
		timestamp,
		pt.Config.ChainName,
		pt.Config.TestingClient,
		pt.Config.TestType,
		qps,
		duration,
		repetition+1)

	var dirname string
	if pt.Config.VersionedTestReport {
		dirname = "./perf/reports/" + BinaryDir + "/"
	} else {
		dirname = pt.RunDirs.RunTestDir + "/" + BinaryDir + "/"
	}

	if err := os.MkdirAll(dirname, 0755); err != nil {
		return fmt.Errorf("failed to create binary directory: %w", err)
	}

	pt.Config.BinaryFileFullPathname = dirname + pt.Config.BinaryFile

	maxRepDigits := strconv.Itoa(format.MaxRepetitionDigits)
	maxQpsDigits := strconv.Itoa(format.MaxQpsDigits)
	maxDurDigits := strconv.Itoa(format.MaxDurationDigits)
	fmt.Printf("[%d.%"+maxRepDigits+"d] %s: executes test qps: %"+maxQpsDigits+"d time: %"+maxDurDigits+"d -> ",
		testNumber, repetition+1, pt.Config.TestingClient, qps, duration)

	targets, err := pt.loadTargets(pattern)
	if err != nil {
		return fmt.Errorf("failed to load targets: %w", err)
	}

	metrics, err := pt.runVegetaAttack(ctx, targets, qps, time.Duration(duration)*time.Second, pt.Config.BinaryFileFullPathname)
	if err != nil {
		return fmt.Errorf("vegeta attack failed: %w", err)
	}

	if pt.Config.CheckServerAlive {
		if !IsProcessRunning(pt.Config.TestingClient) {
			fmt.Println("test failed: server is Dead")
			return fmt.Errorf("server died during test")
		}
	}

	return pt.processResults(testNumber, repetition, name, qps, duration, metrics)
}

// ExecuteSequence executes a sequence of performance tests.
func (pt *PerfTest) ExecuteSequence(ctx context.Context, sequence TestSequence, tag string) error {
	testNumber := 1

	pattern := pt.RunDirs.PatternBase + pt.Config.TestType + ".txt"

	if file, err := os.Open(pattern); err == nil {
		scanner := bufio.NewScanner(file)
		if scanner.Scan() {
			var vt VegetaTarget
			if json.Unmarshal([]byte(scanner.Text()), &vt) == nil {
				fmt.Printf("Test on port: %s\n", vt.URL)
			}
		}
		file.Close()
	}

	maxQpsDigits, maxDurationDigits := MaxQpsAndDurationDigits(sequence)
	resultFormat := ResultFormat{
		MaxRepetitionDigits: CountDigits(pt.Config.Repetitions),
		MaxQpsDigits:        maxQpsDigits,
		MaxDurationDigits:   maxDurationDigits,
	}

	for _, test := range sequence {
		for rep := range pt.Config.Repetitions {
			if test.QPS > 0 {
				if err := pt.Execute(ctx, testNumber, rep, tag, test.QPS, test.Duration, resultFormat); err != nil {
					return err
				}
			} else {
				time.Sleep(time.Duration(test.Duration) * time.Second)
			}

			time.Sleep(time.Duration(pt.Config.WaitingTime) * time.Second)
		}
		testNumber++
		fmt.Println()
	}

	return nil
}

// loadTargets loads Vegeta targets from a pattern file.
func (pt *PerfTest) loadTargets(filepath string) ([]vegeta.Target, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	const maxCapacity = 1024 * 1024
	var targets []vegeta.Target
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, maxCapacity)
	scanner.Buffer(buffer, maxCapacity)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var vt VegetaTarget
		if err := json.Unmarshal([]byte(line), &vt); err != nil {
			return nil, fmt.Errorf("failed to parse target: %w", err)
		}

		target := vegeta.Target{
			Method: vt.Method,
			URL:    vt.URL,
			Body:   vt.Body,
			Header: make(http.Header),
		}

		for k, v := range vt.Header {
			for _, vv := range v {
				target.Header.Set(k, vv)
			}
		}

		targets = append(targets, target)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets found in pattern file")
	}

	return targets, nil
}

// runVegetaAttack executes a Vegeta attack using the library.
func (pt *PerfTest) runVegetaAttack(ctx context.Context, targets []vegeta.Target, qps int, duration time.Duration, outputFile string) (*vegeta.Metrics, error) {
	rate := vegeta.Rate{Freq: qps, Per: time.Second}
	targeter := vegeta.NewStaticTargeter(targets...)

	timeout, _ := time.ParseDuration(pt.Config.VegetaResponseTimeout)
	maxConnInt, _ := strconv.Atoi(pt.Config.MaxConnection)
	maxBodyInt, _ := strconv.Atoi(pt.Config.MaxBodyRsp)

	tr := &http.Transport{
		DisableCompression:  pt.Config.DisableHttpCompression,
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConnsPerHost: maxConnInt,
	}

	customClient := &http.Client{
		Transport: tr,
	}

        //
        // High workers() counts can saturate server resources
        //
	attacker := vegeta.NewAttacker(
		vegeta.Client(customClient),
		vegeta.Timeout(timeout),
                vegeta.Workers(vegeta.DefaultWorkers),
		vegeta.MaxBody(int64(maxBodyInt)),
		vegeta.KeepAlive(true),
	)

	out, err := os.Create(outputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	encoder := vegeta.NewEncoder(out)

	var metrics vegeta.Metrics
	resultCh := attacker.Attack(targeter, rate, duration, "vegeta-attack")
	for {
		select {
		case result := <-resultCh:
			if result == nil {
				metrics.Close()
				return &metrics, nil
			}
			metrics.Add(result)
			if err := encoder.Encode(result); err != nil {
				log.Printf("Warning: failed to encode result: %v", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// processResults processes the vegeta metrics and generates reports.
func (pt *PerfTest) processResults(testNumber, repetition int, name string, qps, duration int, metrics *vegeta.Metrics) error {
	minLatency := FormatDuration(metrics.Latencies.Min)
	mean := FormatDuration(metrics.Latencies.Mean)
	p50 := FormatDuration(metrics.Latencies.P50)
	p90 := FormatDuration(metrics.Latencies.P90)
	p95 := FormatDuration(metrics.Latencies.P95)
	p99 := FormatDuration(metrics.Latencies.P99)
	maxLatency := FormatDuration(metrics.Latencies.Max)

	successRatio := fmt.Sprintf("%.2f%%", metrics.Success*100)

	errorMsg := ""
	if len(metrics.Errors) > 0 {
		errorMap := make(map[string]int)
		for _, err := range metrics.Errors {
			errorMap[err]++
		}

		const MaxErrorsToDisplay = 1
		errorsToDisplay := 0
		for errStr, count := range errorMap {
			if errorsToDisplay >= MaxErrorsToDisplay {
				break
			}
			if errorMsg != "" {
				errorMsg += "; "
			}
			errorMsg += fmt.Sprintf("%s (x%d)", errStr, count)
			errorsToDisplay++
		}
		if errorsToDisplay < len(errorMap) {
			errorMsg += fmt.Sprintf(" (+%d more)", len(errorMap)-errorsToDisplay)
		}
	}

	var resultRecord string
	if pt.Config.MorePercentiles {
		resultRecord = fmt.Sprintf("success=%7s lat=[p50=%8s  p90=%8s  p95=%8s  p99=%8s  max=%8s]",
			successRatio, p50, p90, p95, p99, maxLatency)
	} else {
		resultRecord = fmt.Sprintf("success=%7s lat=[max=%8s]", successRatio, maxLatency)
	}
	if errorMsg != "" {
		resultRecord += fmt.Sprintf(" error=%s", errorMsg)
	}
	fmt.Println(resultRecord)

	if errorMsg != "" && pt.Config.HaltOnVegetaError {
		return fmt.Errorf("test failed: %s", errorMsg)
	}

	if successRatio != "100.00%" {
		return fmt.Errorf("test failed: ratio is not 100.00%%")
	}

	if pt.Config.CreateTestReport {
		testMetrics := &PerfMetrics{
			ClientName:    name,
			TestNumber:    testNumber,
			Repetition:    repetition,
			QPS:           qps,
			Duration:      duration,
			MinLatency:    minLatency,
			Mean:          mean,
			P50:           p50,
			P90:           p90,
			P95:           p95,
			P99:           p99,
			MaxLatency:    maxLatency,
			SuccessRatio:  successRatio,
			Error:         errorMsg,
			VegetaMetrics: metrics,
		}

		if err := pt.Report.WriteTestReport(testMetrics); err != nil {
			return fmt.Errorf("failed to write test report: %w", err)
		}
	}

	if pt.Config.InstantReport {
		printInstantReport(metrics)
	}

	return nil
}

// printInstantReport prints detailed metrics to the console.
func printInstantReport(metrics *vegeta.Metrics) {
	fmt.Println("\n=== Detailed Metrics ===")
	fmt.Printf("Requests:      %d\n", metrics.Requests)
	fmt.Printf("Duration:      %v\n", metrics.Duration)
	fmt.Printf("Rate:          %.2f req/s\n", metrics.Rate)
	fmt.Printf("Throughput:    %.2f req/s\n", metrics.Throughput)
	fmt.Printf("Success:       %.2f%%\n", metrics.Success*100)

	fmt.Println("\nLatencies:")
	fmt.Printf("  Min:         %v\n", metrics.Latencies.Min)
	fmt.Printf("  Mean:        %v\n", metrics.Latencies.Mean)
	fmt.Printf("  P50:         %v\n", metrics.Latencies.P50)
	fmt.Printf("  P90:         %v\n", metrics.Latencies.P90)
	fmt.Printf("  P95:         %v\n", metrics.Latencies.P95)
	fmt.Printf("  P99:         %v\n", metrics.Latencies.P99)
	fmt.Printf("  Max:         %v\n", metrics.Latencies.Max)

	fmt.Println("\nStatus Codes:")
	for code, count := range metrics.StatusCodes {
		fmt.Printf("  %s: %d\n", code, count)
	}

	if len(metrics.Errors) > 0 {
		fmt.Println("\nErrors:")
		errorMap := make(map[string]int)
		for _, err := range metrics.Errors {
			errorMap[err]++
		}
		for errStr, count := range errorMap {
			fmt.Printf("  %s: %d\n", errStr, count)
		}
	}

	fmt.Print("========================\n\n")
}

// Compression type constants.
const (
	GzipCompression  = ".gz"
	Bzip2Compression = ".bz2"
	NoCompression    = ""
)

func getCompressionType(filename string) string {
	if strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tgz") {
		return GzipCompression
	}
	if strings.HasSuffix(filename, ".tar.bz2") || strings.HasSuffix(filename, ".tbz") {
		return Bzip2Compression
	}
	return NoCompression
}

func autodetectCompression(inFile *os.File) (string, error) {
	compressionType := NoCompression
	tarReader := tar.NewReader(inFile)
	_, err := tarReader.Next()
	if err != nil && !errors.Is(err, io.EOF) {
		if _, err = inFile.Seek(0, io.SeekStart); err != nil {
			return compressionType, err
		}
		if _, err = gzip.NewReader(inFile); err == nil {
			compressionType = GzipCompression
		} else {
			if _, err = inFile.Seek(0, io.SeekStart); err != nil {
				return compressionType, err
			}
			if _, err = tar.NewReader(bzip2.NewReader(inFile)).Next(); err == nil {
				compressionType = Bzip2Compression
			}
		}
	}
	return compressionType, nil
}

func extractTarGz(tarFile, destDir string) error {
	file, err := os.Open(tarFile)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	compressionType := getCompressionType(tarFile)
	if compressionType == NoCompression {
		compressionType, err = autodetectCompression(file)
		if err != nil {
			return fmt.Errorf("failed to autodetect compression for archive: %w", err)
		}
		file.Close()
		file, err = os.Open(tarFile)
		if err != nil {
			return err
		}
		defer file.Close()
	}

	var reader io.Reader
	switch compressionType {
	case GzipCompression:
		if reader, err = gzip.NewReader(file); err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
	case Bzip2Compression:
		reader = bzip2.NewReader(file)
	case NoCompression:
		reader = file
	}

	tr := tar.NewReader(reader)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			outFile, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func replaceInFile(filepath, old, new string) error {
	input, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}
	output := strings.ReplaceAll(string(input), old, new)
	return os.WriteFile(filepath, []byte(output), 0644)
}
