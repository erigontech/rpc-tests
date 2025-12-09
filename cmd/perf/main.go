package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/bzip2"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/urfave/cli/v2"
)

const (
	DefaultTestSequence          = "50:30,1000:30,2500:20,10000:20"
	DefaultRepetitions           = 10
	DefaultVegetaPatternTarFile  = ""
	DefaultDaemonVegetaOnCore    = "-:-"
	DefaultErigonBuildDir        = ""
	DefaultSilkwormBuildDir      = ""
	DefaultErigonAddress         = "localhost"
	DefaultTestMode              = "3"
	DefaultWaitingTime           = 5
	DefaultMaxConn               = "9000"
	DefaultTestType              = "eth_getLogs"
	DefaultVegetaResponseTimeout = "300s"
	DefaultMaxBodyRsp            = "1500"

	Silkworm           = "silkworm"
	Erigon             = "rpcdaemon"
	BinaryDir          = "bin"
	SilkwormServerName = "rpcdaemon"
	ErigonServerName   = "rpcdaemon"
)

var (
	RunTestDirname            string
	VegetaPatternDirname      string
	VegetaReport              string
	VegetaTarFileName         string
	VegetaPatternSilkwormBase string
	VegetaPatternErigonBase   string
)

func init() {
	// Generate a random directory name
	timestamp := time.Now().UnixNano()
	RunTestDirname = fmt.Sprintf("/tmp/run_tests_%d", timestamp)
	VegetaPatternDirname = RunTestDirname + "/erigon_stress_test"
	VegetaReport = RunTestDirname + "/vegeta_report.hrd"
	VegetaTarFileName = RunTestDirname + "/vegeta_TAR_File"
	VegetaPatternSilkwormBase = VegetaPatternDirname + "/vegeta_geth_"
	VegetaPatternErigonBase = VegetaPatternDirname + "/vegeta_erigon_"
}

// Config holds all configuration for the performance test
type Config struct {
	VegetaPatternTarFile   string
	DaemonVegetaOnCore     string
	ErigonDir              string
	SilkwormDir            string
	Repetitions            int
	TestSequence           string
	RPCDaemonAddress       string
	TestMode               string
	TestType               string
	TestingDaemon          string
	WaitingTime            int
	VersionedTestReport    bool
	Verbose                bool
	MacConnection          bool
	CheckServerAlive       bool
	Tracing                bool
	EmptyCache             bool
	CreateTestReport       bool
	MaxConnection          string
	VegetaResponseTimeout  string
	MaxBodyRsp             string
	JSONReportFile         string
	BinaryFileFullPathname string
	BinaryFile             string
	ChainName              string
	MorePercentiles        bool
	InstantReport          bool
	HaltOnVegetaError      bool
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	return &Config{
		VegetaPatternTarFile:   DefaultVegetaPatternTarFile,
		DaemonVegetaOnCore:     DefaultDaemonVegetaOnCore,
		ErigonDir:              DefaultErigonBuildDir,
		SilkwormDir:            DefaultSilkwormBuildDir,
		Repetitions:            DefaultRepetitions,
		TestSequence:           DefaultTestSequence,
		RPCDaemonAddress:       DefaultErigonAddress,
		TestMode:               DefaultTestMode,
		TestType:               DefaultTestType,
		TestingDaemon:          "",
		WaitingTime:            DefaultWaitingTime,
		VersionedTestReport:    false,
		Verbose:                false,
		MacConnection:          false,
		CheckServerAlive:       true,
		Tracing:                false,
		EmptyCache:             false,
		CreateTestReport:       false,
		MaxConnection:          DefaultMaxConn,
		VegetaResponseTimeout:  DefaultVegetaResponseTimeout,
		MaxBodyRsp:             DefaultMaxBodyRsp,
		JSONReportFile:         "",
		BinaryFileFullPathname: "",
		BinaryFile:             "",
		ChainName:              "mainnet",
		MorePercentiles:        false,
		InstantReport:          false,
		HaltOnVegetaError:      false,
	}
}

// Validate checks the configuration for conflicts and invalid values
func (c *Config) Validate() error {
	if c.JSONReportFile != "" && c.TestMode == "3" {
		return fmt.Errorf("incompatible option json-report with test-mode=3")
	}

	if c.TestMode == "3" && c.TestingDaemon != "" {
		return fmt.Errorf("incompatible option test-mode=3 and testing-daemon")
	}

	if c.JSONReportFile != "" && c.TestingDaemon == "" {
		return fmt.Errorf("with json-report must also set testing-daemon")
	}

	if (c.ErigonDir != DefaultErigonBuildDir || c.SilkwormDir != DefaultSilkwormBuildDir) &&
		c.RPCDaemonAddress != DefaultErigonAddress {
		return fmt.Errorf("incompatible option rpc-daemon-address with erigon-dir/silk-dir")
	}

	if c.EmptyCache {
		currentUser, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to get current user: %w", err)
		}
		if currentUser.Username != "root" {
			return fmt.Errorf("empty-cache option can only be used by root")
		}
	}

	if c.CreateTestReport {
		if _, err := os.Stat(c.ErigonDir); c.ErigonDir != "" && os.IsNotExist(err) {
			return fmt.Errorf("erigon build dir not specified correctly: %s", c.ErigonDir)
		}

		if _, err := os.Stat(c.SilkwormDir); c.SilkwormDir != "" && os.IsNotExist(err) {
			return fmt.Errorf("silkworm build dir not specified correctly: %s", c.SilkwormDir)
		}
	}

	return nil
}

// TestSequenceItem represents a single test in the sequence
type TestSequenceItem struct {
	QPS      int
	Duration int
}

type TestSequence []TestSequenceItem

// ParseTestSequence parses the test sequence string into structured items
func ParseTestSequence(sequence string) ([]TestSequenceItem, error) {
	var items []TestSequenceItem

	parts := strings.Split(sequence, ",")
	for _, part := range parts {
		qpsDur := strings.Split(part, ":")
		if len(qpsDur) != 2 {
			return nil, fmt.Errorf("invalid test sequence format: %s", part)
		}

		qps, err := strconv.Atoi(qpsDur[0])
		if err != nil {
			return nil, fmt.Errorf("invalid QPS value: %s", qpsDur[0])
		}

		duration, err := strconv.Atoi(qpsDur[1])
		if err != nil {
			return nil, fmt.Errorf("invalid duration value: %s", qpsDur[1])
		}

		items = append(items, TestSequenceItem{
			QPS:      qps,
			Duration: duration,
		})
	}

	return items, nil
}

// VegetaTarget represents a single HTTP request target for Vegeta
type VegetaTarget struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Body   []byte              `json:"body,omitempty"`
	Header map[string][]string `json:"header,omitempty"`
}

// TestMetrics holds the results of a performance test
type TestMetrics struct {
	DaemonName    string
	TestNumber    int
	Repetition    int
	QPS           int
	Duration      int
	MinLatency    string
	Mean          string
	P50           string
	P90           string
	P95           string
	P99           string
	MaxLatency    string
	SuccessRatio  string
	Error         string
	VegetaMetrics *vegeta.Metrics
}

// JSONReport represents the structure of the JSON performance report
type JSONReport struct {
	Platform      PlatformInfo      `json:"platform"`
	Configuration ConfigurationInfo `json:"configuration"`
	Results       []TestResult      `json:"results"`
}

// PlatformInfo holds platform hardware and software information
type PlatformInfo struct {
	Vendor        string `json:"vendor"`
	Product       string `json:"product"`
	Board         string `json:"board"`
	CPU           string `json:"cpu"`
	Bogomips      string `json:"bogomips"`
	Kernel        string `json:"kernel"`
	GCCVersion    string `json:"gccVersion"`
	GoVersion     string `json:"goVersion"`
	SilkrpcCommit string `json:"silkrpcCommit"`
	ErigonCommit  string `json:"erigonCommit"`
}

// ConfigurationInfo holds test configuration information
type ConfigurationInfo struct {
	TestingDaemon   string `json:"testingDaemon"`
	TestingAPI      string `json:"testingApi"`
	TestSequence    string `json:"testSequence"`
	TestRepetitions int    `json:"testRepetitions"`
	VegetaFile      string `json:"vegetaFile"`
	VegetaChecksum  string `json:"vegetaChecksum"`
	Taskset         string `json:"taskset"`
}

// TestResult holds results for a single QPS/duration test
type TestResult struct {
	QPS             int              `json:"qps"`
	Duration        int              `json:"duration"`
	TestRepetitions []RepetitionInfo `json:"testRepetitions"`
}

// RepetitionInfo holds information for a single test repetition
type RepetitionInfo struct {
	VegetaBinary        string                 `json:"vegetaBinary"`
	VegetaReport        map[string]interface{} `json:"vegetaReport"`
	VegetaReportHdrPlot string                 `json:"vegetaReportHdrPlot"`
}

// Hardware provides methods to extract hardware information
type Hardware struct{}

// Vendor returns the system vendor
func (h *Hardware) Vendor() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	data, err := os.ReadFile("/sys/devices/virtual/dmi/id/sys_vendor")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// NormalizedVendor returns the system vendor as a lowercase first token
func (h *Hardware) NormalizedVendor() string {
	vendor := h.Vendor()
	parts := strings.Split(vendor, " ")
	if len(parts) > 0 {
		return strings.ToLower(parts[0])
	}
	return "unknown"
}

// Product returns the system product name
func (h *Hardware) Product() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	data, err := os.ReadFile("/sys/devices/virtual/dmi/id/product_name")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// Board returns the system board name
func (h *Hardware) Board() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	data, err := os.ReadFile("/sys/devices/virtual/dmi/id/board_name")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// NormalizedProduct returns the system product name as lowercase without whitespaces
func (h *Hardware) NormalizedProduct() string {
	product := h.Product()
	return strings.ToLower(strings.ReplaceAll(product, " ", ""))
}

// NormalizedBoard returns the board name as a lowercase name without whitespaces
func (h *Hardware) NormalizedBoard() string {
	board := h.Board()
	parts := strings.Split(board, "/")
	if len(parts) > 0 {
		return strings.ToLower(strings.ReplaceAll(parts[0], " ", ""))
	}
	return "unknown"
}

// GetCPUModel returns the CPU model information
func (h *Hardware) GetCPUModel() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}

	cmd := exec.Command("sh", "-c", "cat /proc/cpuinfo | grep 'model name' | uniq")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	parts := strings.Split(string(output), ":")
	if len(parts) > 1 {
		return strings.TrimSpace(parts[1])
	}
	return "unknown"
}

// GetBogomips returns the bogomips value
func (h *Hardware) GetBogomips() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}

	cmd := exec.Command("sh", "-c", "cat /proc/cpuinfo | grep 'bogomips' | uniq")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	parts := strings.Split(string(output), ":")
	if len(parts) > 1 {
		return strings.TrimSpace(parts[1])
	}
	return "unknown"
}

// GetKernelVersion returns the kernel version
func GetKernelVersion() string {
	cmd := exec.Command("uname", "-r")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// GetGCCVersion returns the GCC version
func GetGCCVersion() string {
	cmd := exec.Command("gcc", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return "unknown"
}

// GetGoVersion returns the Go version
func GetGoVersion() string {
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// GetGitCommit returns the git commit hash for a directory
func GetGitCommit(dir string) string {
	if dir == "" {
		return ""
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// GetFileChecksum returns the checksum of a file
func GetFileChecksum(filepath string) string {
	cmd := exec.Command("sum", filepath)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	parts := strings.Split(string(output), " ")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// IsProcessRunning checks if a process with the given name is running
func IsProcessRunning(processName string) bool {
	cmd := exec.Command("pgrep", "-f", processName)
	err := cmd.Run()
	return err == nil
}

// EmptyCache drops OS caches
func EmptyCache() error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		// Sync and drop caches
		if err := exec.Command("sync").Run(); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
		cmd = exec.Command("sh", "-c", "echo 3 > /proc/sys/vm/drop_caches")
	case "darwin":
		// macOS purge
		if err := exec.Command("sync").Run(); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
		cmd = exec.Command("purge")
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cache purge failed: %w", err)
	}

	return nil
}

// FormatDuration formats a duration string with units
func FormatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.0fµs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// ParseLatency parses a latency string and returns it in a consistent format
func ParseLatency(latency string) string {
	// Replace microsecond symbol and normalise
	latency = strings.ReplaceAll(latency, "µs", "us")
	return strings.TrimSpace(latency)
}

// PerfTest manages performance test execution
type PerfTest struct {
	config     *Config
	testReport *TestReport
}

// NewPerfTest creates a new performance test instance
func NewPerfTest(config *Config, testReport *TestReport) (*PerfTest, error) {
	pt := &PerfTest{
		config:     config,
		testReport: testReport,
	}

	// Initial cleanup
	if err := pt.Cleanup(true); err != nil {
		return nil, fmt.Errorf("initial cleanup failed: %w", err)
	}

	// Copy and extract the pattern file
	if err := pt.CopyAndExtractPatternFile(); err != nil {
		return nil, fmt.Errorf("failed to setup pattern file: %w", err)
	}

	return pt, nil
}

// Cleanup removes temporary files
func (pt *PerfTest) Cleanup(initial bool) error {
	filesToRemove := []string{
		VegetaTarFileName,
		"perf.data.old",
		"perf.data",
	}

	for _, fileName := range filesToRemove {
		_, err := os.Stat(fileName)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		err = os.Remove(fileName)
		if err != nil {
			return err
		}
	}

	// Remove the pattern directory
	err := os.RemoveAll(VegetaPatternDirname)
	if err != nil {
		return err
	}

	// Remove the run test directory
	if initial {
		err := os.RemoveAll(RunTestDirname)
		if err != nil {
			return err
		}
	} else {
		// Try to remove, ignore if not empty
		_ = os.Remove(RunTestDirname)
	}

	return nil
}

// CopyAndExtractPatternFile copies and extracts the vegeta pattern tar file
func (pt *PerfTest) CopyAndExtractPatternFile() error {
	// Check if the pattern file exists
	if _, err := os.Stat(pt.config.VegetaPatternTarFile); os.IsNotExist(err) {
		return fmt.Errorf("invalid pattern file: %s", pt.config.VegetaPatternTarFile)
	}

	// Create the run test directory
	if err := os.MkdirAll(RunTestDirname, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Copy tar file
	if err := pt.copyFile(pt.config.VegetaPatternTarFile, VegetaTarFileName); err != nil {
		return fmt.Errorf("failed to copy pattern file: %w", err)
	}

	if pt.config.Tracing {
		fmt.Printf("Copy Vegeta pattern: %s -> %s\n", pt.config.VegetaPatternTarFile, VegetaTarFileName)
	}

	// Extract tar file
	if err := pt.extractTarGz(VegetaTarFileName, RunTestDirname); err != nil {
		return fmt.Errorf("failed to extract pattern file: %w", err)
	}

	if pt.config.Tracing {
		fmt.Printf("Extracting Vegeta pattern to: %s\n", RunTestDirname)
	}

	// Substitute address if not localhost
	if pt.config.RPCDaemonAddress != "localhost" {
		silkwormPattern := VegetaPatternSilkwormBase + pt.config.TestType + ".txt"
		erigonPattern := VegetaPatternErigonBase + pt.config.TestType + ".txt"

		if err := pt.replaceInFile(silkwormPattern, "localhost", pt.config.RPCDaemonAddress); err != nil {
			log.Printf("Warning: failed to replace address in silkworm pattern: %v", err)
		}

		if err := pt.replaceInFile(erigonPattern, "localhost", pt.config.RPCDaemonAddress); err != nil {
			log.Printf("Warning: failed to replace address in erigon pattern: %v", err)
		}
	}

	return nil
}

// copyFile copies a file from src to dst
func (pt *PerfTest) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func(sourceFile *os.File) {
		err := sourceFile.Close()
		if err != nil {
			log.Printf("Warning: failed to close source file: %v", err)
		}
	}(sourceFile)

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func(destFile *os.File) {
		err := destFile.Close()
		if err != nil {
			log.Printf("Warning: failed to close destination file: %v", err)
		}
	}(destFile)

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// extractTarGz extracts a tar.gz file to a destination directory
func (pt *PerfTest) extractTarGz(tarFile, destDir string) error {
	file, err := os.Open(tarFile)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Printf("Warning: failed to close tar file: %v", err)
		}
	}(file)

	/*gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func(gzr *gzip.Reader) {
		err := gzr.Close()
		if err != nil {
			log.Printf("Warning: failed to close gzip reader: %v", err)
		}
	}(gzr)*/

	tr := tar.NewReader(bzip2.NewReader(file))

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
				err := outFile.Close()
				if err != nil {
					return err
				}
				return err
			}
			err = outFile.Close()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// replaceInFile replaces old string with new string in a file
func (pt *PerfTest) replaceInFile(filepath, old, new string) error {
	input, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	output := strings.ReplaceAll(string(input), old, new)

	return os.WriteFile(filepath, []byte(output), 0644)
}

// Execute runs a single performance test
func (pt *PerfTest) Execute(ctx context.Context, testNumber, repetition int, name string, qps, duration int, format ResultFormat) error {
	// Empty cache if configured
	if pt.config.EmptyCache {
		if err := EmptyCache(); err != nil {
			log.Printf("Warning: failed to empty cache: %v", err)
		}
	}

	// Determine pattern file
	var pattern string
	if name == Silkworm {
		pattern = VegetaPatternSilkwormBase + pt.config.TestType + ".txt"
	} else {
		pattern = VegetaPatternErigonBase + pt.config.TestType + ".txt"
	}

	// Create the binary file name
	timestamp := time.Now().Format("20060102150405")
	pt.config.BinaryFile = fmt.Sprintf("%s_%s_%s_%s_%d_%d_%d.bin",
		timestamp,
		pt.config.ChainName,
		pt.config.TestingDaemon,
		pt.config.TestType,
		qps,
		duration,
		repetition+1)

	// Create the binary directory
	var dirname string
	if pt.config.VersionedTestReport {
		dirname = "./reports/" + BinaryDir + "/"
	} else {
		dirname = RunTestDirname + "/" + BinaryDir + "/"
	}

	if err := os.MkdirAll(dirname, 0755); err != nil {
		return fmt.Errorf("failed to create binary directory: %w", err)
	}

	pt.config.BinaryFileFullPathname = dirname + pt.config.BinaryFile

	// Print test result information
	maxRepetitionDigits := strconv.Itoa(format.maxRepetitionDigits)
	maxQpsDigits := strconv.Itoa(format.maxQpsDigits)
	maxDurationDigits := strconv.Itoa(format.maxDurationDigits)
	if pt.config.TestingDaemon != "" {
		fmt.Printf("[%d.%"+maxRepetitionDigits+"d] %s: executes test qps: %"+maxQpsDigits+"d time: %"+maxDurationDigits+"d -> ",
			testNumber, repetition+1, pt.config.TestingDaemon, qps, duration)
	} else {
		fmt.Printf("[%d.%"+maxRepetitionDigits+"d] daemon: executes test qps: %"+maxQpsDigits+"d time: %"+maxDurationDigits+"d -> ",
			testNumber, repetition+1, qps, duration)
	}

	// Load targets from pattern file
	targets, err := pt.loadTargets(pattern)
	if err != nil {
		return fmt.Errorf("failed to load targets: %w", err)
	}

	// Run vegeta attack
	metrics, err := pt.runVegetaAttack(ctx, targets, qps, time.Duration(duration)*time.Second, pt.config.BinaryFileFullPathname)
	if err != nil {
		return fmt.Errorf("vegeta attack failed: %w", err)
	}

	// Check if the server is still alive during the test
	if pt.config.CheckServerAlive {
		var serverName string
		if name == Silkworm {
			serverName = SilkwormServerName
		} else {
			serverName = ErigonServerName
		}

		if !IsProcessRunning(serverName) {
			fmt.Println("test failed: server is Dead")
			return fmt.Errorf("server died during test")
		}
	}

	// Process results
	return pt.processResults(testNumber, repetition, name, qps, duration, metrics)
}

// loadTargets loads Vegeta targets from a pattern file
func (pt *PerfTest) loadTargets(filepath string) ([]vegeta.Target, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Printf("Warning: failed to close pattern file: %v", err)
		}
	}(file)

	var targets []vegeta.Target
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 256*1024)
	scanner.Buffer(buffer, cap(buffer))

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

	// Print test port information
	/*if pt.config.Verbose {
		fmt.Printf("Test on port: %s\n", targets[0].URL)
	}*/

	return targets, nil
}

// runVegetaAttack executes a Vegeta attack using the library
func (pt *PerfTest) runVegetaAttack(ctx context.Context, targets []vegeta.Target, qps int, duration time.Duration, outputFile string) (*vegeta.Metrics, error) {
	// Create rate
	rate := vegeta.Rate{Freq: qps, Per: time.Second}

	// Create targeter
	targeter := vegeta.NewStaticTargeter(targets...)

	// Create attacker
	timeout, _ := time.ParseDuration(pt.config.VegetaResponseTimeout)
	maxConnInt, _ := strconv.Atoi(pt.config.MaxConnection)
	maxBodyInt, _ := strconv.Atoi(pt.config.MaxBodyRsp)

	attacker := vegeta.NewAttacker(
		vegeta.Timeout(timeout),
		vegeta.Workers(uint64(maxConnInt)),
		vegeta.MaxBody(int64(maxBodyInt)),
		vegeta.KeepAlive(true),
	)

	// Create the output file for results
	out, err := os.Create(outputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer func(out *os.File) {
		err := out.Close()
		if err != nil {
			log.Printf("Warning: failed to close output file: %v", err)
		}
	}(out)

	encoder := vegeta.NewEncoder(out)

	// Execute the attack i.e. the test workload
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

// ExecuteSequence executes a sequence of performance tests
func (pt *PerfTest) ExecuteSequence(ctx context.Context, sequence []TestSequenceItem, tag string) error {
	testNumber := 1

	// Get pattern to extract port information
	var pattern string
	if tag == Silkworm {
		pattern = VegetaPatternSilkwormBase + pt.config.TestType + ".txt"
	} else {
		pattern = VegetaPatternErigonBase + pt.config.TestType + ".txt"
	}

	// Print port information
	if file, err := os.Open(pattern); err == nil {
		scanner := bufio.NewScanner(file)
		if scanner.Scan() {
			var vt VegetaTarget
			if json.Unmarshal([]byte(scanner.Text()), &vt) == nil {
				fmt.Printf("Test on port: %s\n", vt.URL)
			}
		}
		err = file.Close()
		if err != nil {
			return err
		}
	}

	maxQpsDigits, maxDurationDigits := maxQpsAndDurationDigits(sequence)
	resultFormat := ResultFormat{
		maxRepetitionDigits: countDigits(pt.config.Repetitions),
		maxQpsDigits:        maxQpsDigits,
		maxDurationDigits:   maxDurationDigits,
	}

	// Execute each test in sequence
	for _, test := range sequence {
		for rep := 0; rep < pt.config.Repetitions; rep++ {
			if test.QPS > 0 {
				err := pt.Execute(ctx, testNumber, rep, tag, test.QPS, test.Duration, resultFormat)
				if err != nil {
					return err
				}
			} else {
				// qps = 0 means we've been asked for a silence period
				time.Sleep(time.Duration(test.Duration) * time.Second)
			}

			time.Sleep(time.Duration(pt.config.WaitingTime) * time.Second)
		}
		testNumber++
		fmt.Println()
	}

	return nil
}

func countDigits(n int) int {
	if n == 0 {
		return 1
	}
	digits := 0
	for n != 0 {
		n /= 10
		digits++
	}
	return digits
}

func maxQpsAndDurationDigits(sequence TestSequence) (maxQpsDigits, maxDurationDigits int) {
	for _, item := range sequence {
		qpsDigits := countDigits(item.QPS)
		if qpsDigits > maxQpsDigits {
			maxQpsDigits = qpsDigits
		}
		durationDigits := countDigits(item.Duration)
		if durationDigits > maxDurationDigits {
			maxDurationDigits = durationDigits
		}
	}
	return
}

type ResultFormat struct {
	maxRepetitionDigits, maxQpsDigits, maxDurationDigits int
}

// processResults processes the vegeta metrics and generates reports
func (pt *PerfTest) processResults(testNumber, repetition int, name string, qps, duration int, metrics *vegeta.Metrics) error {
	// Extract latency values
	minLatency := FormatDuration(metrics.Latencies.Min)
	mean := FormatDuration(metrics.Latencies.Mean)
	p50 := FormatDuration(metrics.Latencies.P50)
	p90 := FormatDuration(metrics.Latencies.P90)
	p95 := FormatDuration(metrics.Latencies.P95)
	p99 := FormatDuration(metrics.Latencies.P99)
	maxLatency := FormatDuration(metrics.Latencies.Max)

	// Calculate success ratio
	successRatio := fmt.Sprintf("%.2f%%", metrics.Success*100)

	// Check for errors
	errorMsg := ""
	if len(metrics.Errors) > 0 {
		// Collect unique error messages
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

	// Print results
	var resultRecord string
	if pt.config.MorePercentiles {
		resultRecord = fmt.Sprintf("success=%7s lat=[p50=%8s  p90=%8s  p95=%8s  p99=%8s  max=%8s]",
			successRatio, p50, p90, p95, p99, maxLatency)
	} else {
		resultRecord = fmt.Sprintf("success=%7s lat=[max=%8s]", successRatio, maxLatency)
	}
	if errorMsg != "" {
		resultRecord += fmt.Sprintf(" error=%s", errorMsg)
	}
	fmt.Println(resultRecord)

	// Check for failures
	if errorMsg != "" && pt.config.HaltOnVegetaError {
		return fmt.Errorf("test failed: %s", errorMsg)
	}

	if successRatio != "100.00%" {
		return fmt.Errorf("test failed: ratio is not 100.00%%")
	}

	// Write to the test report if enabled
	if pt.config.CreateTestReport {
		testMetrics := &TestMetrics{
			DaemonName:    name,
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

		if err := pt.testReport.WriteTestReport(testMetrics); err != nil {
			return fmt.Errorf("failed to write test report: %w", err)
		}
	}

	// Print instant report if enabled
	if pt.config.InstantReport {
		pt.printInstantReport(metrics)
	}

	return nil
}

// printInstantReport prints detailed metrics to the console
func (pt *PerfTest) printInstantReport(metrics *vegeta.Metrics) {
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

// TestReport manages CSV and JSON report generation
type TestReport struct {
	config         *Config
	csvFile        *os.File
	csvWriter      *csv.Writer
	jsonReport     *JSONReport
	hardware       *Hardware
	currentTestIdx int
}

// NewTestReport creates a new test report instance
func NewTestReport(config *Config) *TestReport {
	return &TestReport{
		config:         config,
		hardware:       &Hardware{},
		currentTestIdx: -1,
	}
}

// Open initialises the test report and writes headers
func (tr *TestReport) Open() error {
	if err := tr.createCSVFile(); err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}

	// Collect system information
	checksum := GetFileChecksum(tr.config.VegetaPatternTarFile)
	gccVersion := GetGCCVersion()
	goVersion := GetGoVersion()
	kernelVersion := GetKernelVersion()
	cpuModel := tr.hardware.GetCPUModel()
	bogomips := tr.hardware.GetBogomips()

	var silkrpcCommit, erigonCommit string
	if tr.config.TestMode == "1" || tr.config.TestMode == "3" {
		silkrpcCommit = GetGitCommit(tr.config.SilkwormDir)
	}
	if tr.config.TestMode == "2" || tr.config.TestMode == "3" {
		erigonCommit = GetGitCommit(tr.config.ErigonDir)
	}

	// Write headers
	if err := tr.writeTestHeader(cpuModel, bogomips, kernelVersion, checksum,
		gccVersion, goVersion, silkrpcCommit, erigonCommit); err != nil {
		return fmt.Errorf("failed to write test header: %w", err)
	}

	// Initialise the JSON report if needed
	if tr.config.JSONReportFile != "" {
		tr.initializeJSONReport(cpuModel, bogomips, kernelVersion, checksum,
			gccVersion, goVersion, silkrpcCommit, erigonCommit)
	}

	return nil
}

// createCSVFile creates the CSV report file with appropriate naming
func (tr *TestReport) createCSVFile() error {
	// Determine folder extension
	extension := tr.hardware.NormalizedProduct()
	if extension == "systemproductname" {
		extension = tr.hardware.NormalizedBoard()
	}

	// Create the folder path
	csvFolder := tr.hardware.NormalizedVendor() + "_" + extension
	var csvFolderPath string
	if tr.config.VersionedTestReport {
		csvFolderPath = filepath.Join("./reports", tr.config.ChainName, csvFolder)
	} else {
		csvFolderPath = filepath.Join(RunTestDirname, tr.config.ChainName, csvFolder)
	}

	if err := os.MkdirAll(csvFolderPath, 0755); err != nil {
		return fmt.Errorf("failed to create CSV folder: %w", err)
	}

	// Generate CSV filename
	timestamp := time.Now().Format("20060102150405")
	var csvFilename string
	if tr.config.TestingDaemon != "" {
		csvFilename = fmt.Sprintf("%s_%s_%s_perf.csv",
			tr.config.TestType, timestamp, tr.config.TestingDaemon)
	} else {
		csvFilename = fmt.Sprintf("%s_%s_perf.csv",
			tr.config.TestType, timestamp)
	}

	csvFilepath := filepath.Join(csvFolderPath, csvFilename)

	// Create and open the CSV report file
	file, err := os.Create(csvFilepath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}

	tr.csvFile = file
	tr.csvWriter = csv.NewWriter(file)

	fmt.Printf("Perf report file: %s\n\n", csvFilepath)

	return nil
}

// writeTestHeader writes the test configuration header to CSV
func (tr *TestReport) writeTestHeader(cpuModel, bogomips, kernelVersion, checksum,
	gccVersion, goVersion, silkrpcCommit, erigonCommit string) error {

	// Write platform information
	emptyRow := make([]string, 14)

	err := tr.csvWriter.Write(append(emptyRow[:12], "vendor", tr.hardware.Vendor()))
	if err != nil {
		return err
	}

	product := tr.hardware.Product()
	if product != "System Product Name" {
		err := tr.csvWriter.Write(append(emptyRow[:12], "product", product))
		if err != nil {
			return err
		}
	} else {
		err := tr.csvWriter.Write(append(emptyRow[:12], "board", tr.hardware.Board()))
		if err != nil {
			return err
		}
	}

	err = tr.csvWriter.Write(append(emptyRow[:12], "cpu", cpuModel))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "bogomips", bogomips))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "kernel", kernelVersion))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "taskset", tr.config.DaemonVegetaOnCore))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "vegetaFile", tr.config.VegetaPatternTarFile))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "vegetaChecksum", checksum))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "gccVersion", gccVersion))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "goVersion", goVersion))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "silkrpcVersion", silkrpcCommit))
	if err != nil {
		return err
	}
	err = tr.csvWriter.Write(append(emptyRow[:12], "erigonVersion", erigonCommit))
	if err != nil {
		return err
	}

	// Empty rows
	for range 2 {
		err := tr.csvWriter.Write([]string{})
		if err != nil {
			return err
		}
	}

	// Write column headers
	headers := []string{
		"Daemon", "TestNo", "Repetition", "Qps", "Time(secs)",
		"Min", "Mean", "50", "90", "95", "99", "Max", "Ratio", "Error",
	}
	err = tr.csvWriter.Write(headers)
	if err != nil {
		return err
	}
	tr.csvWriter.Flush()

	return tr.csvWriter.Error()
}

// initializeJSONReport initializes the JSON report structure
func (tr *TestReport) initializeJSONReport(cpuModel, bogomips, kernelVersion, checksum,
	gccVersion, goVersion, silkrpcCommit, erigonCommit string) {

	tr.jsonReport = &JSONReport{
		Platform: PlatformInfo{
			Vendor:        strings.TrimSpace(tr.hardware.Vendor()),
			Product:       strings.TrimSpace(tr.hardware.Product()),
			Board:         strings.TrimSpace(tr.hardware.Board()),
			CPU:           strings.TrimSpace(cpuModel),
			Bogomips:      strings.TrimSpace(bogomips),
			Kernel:        strings.TrimSpace(kernelVersion),
			GCCVersion:    strings.TrimSpace(gccVersion),
			GoVersion:     strings.TrimSpace(goVersion),
			SilkrpcCommit: strings.TrimSpace(silkrpcCommit),
			ErigonCommit:  strings.TrimSpace(erigonCommit),
		},
		Configuration: ConfigurationInfo{
			TestingDaemon:   tr.config.TestingDaemon,
			TestingAPI:      tr.config.TestType,
			TestSequence:    tr.config.TestSequence,
			TestRepetitions: tr.config.Repetitions,
			VegetaFile:      tr.config.VegetaPatternTarFile,
			VegetaChecksum:  checksum,
			Taskset:         tr.config.DaemonVegetaOnCore,
		},
		Results: []TestResult{},
	}
}

// WriteTestReport writes a test result to the report
func (tr *TestReport) WriteTestReport(metrics *TestMetrics) error {
	// Write to CSV
	row := []string{
		metrics.DaemonName,
		strconv.Itoa(metrics.TestNumber),
		strconv.Itoa(metrics.Repetition),
		strconv.Itoa(metrics.QPS),
		strconv.Itoa(metrics.Duration),
		metrics.MinLatency,
		metrics.Mean,
		metrics.P50,
		metrics.P90,
		metrics.P95,
		metrics.P99,
		metrics.MaxLatency,
		metrics.SuccessRatio,
		metrics.Error,
	}

	if err := tr.csvWriter.Write(row); err != nil {
		return fmt.Errorf("failed to write CSV row: %w", err)
	}
	tr.csvWriter.Flush()

	// Write to JSON if enabled
	if tr.config.JSONReportFile != "" {
		if err := tr.writeTestReportToJSON(metrics); err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
	}

	return nil
}

// writeTestReportToJSON writes a test result to the JSON report
func (tr *TestReport) writeTestReportToJSON(metrics *TestMetrics) error {
	// Check if we need to create a new test result entry
	if metrics.Repetition == 0 {
		tr.currentTestIdx++
		tr.jsonReport.Results = append(tr.jsonReport.Results, TestResult{
			QPS:             metrics.QPS,
			Duration:        metrics.Duration,
			TestRepetitions: []RepetitionInfo{},
		})
	}

	// Generate JSON report from the binary file
	jsonReportData, err := tr.generateJSONReport(tr.config.BinaryFileFullPathname)
	if err != nil {
		return fmt.Errorf("failed to generate JSON report: %w", err)
	}

	// Generate HDR plot
	hdrPlot, err := tr.generateHdrPlot(tr.config.BinaryFileFullPathname)
	if err != nil {
		return fmt.Errorf("failed to generate HDR plot: %w", err)
	}

	// Add repetition info
	repetitionInfo := RepetitionInfo{
		VegetaBinary:        tr.config.BinaryFile,
		VegetaReport:        jsonReportData,
		VegetaReportHdrPlot: hdrPlot,
	}

	if tr.currentTestIdx >= 0 && tr.currentTestIdx < len(tr.jsonReport.Results) {
		tr.jsonReport.Results[tr.currentTestIdx].TestRepetitions = append(
			tr.jsonReport.Results[tr.currentTestIdx].TestRepetitions,
			repetitionInfo,
		)
	}

	return nil
}

// generateJSONReport generates a JSON report from the binary file
func (tr *TestReport) generateJSONReport(binaryFile string) (map[string]interface{}, error) {
	// Read the binary file
	file, err := os.Open(binaryFile)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Printf("Warning: failed to close file: %v", err)
		}
	}(file)

	// Decode results
	dec := vegeta.NewDecoder(file)

	// Create metrics
	var metrics vegeta.Metrics
	for {
		var result vegeta.Result
		if err := dec.Decode(&result); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		metrics.Add(&result)
	}
	metrics.Close()

	// Convert metrics to map
	report := map[string]interface{}{
		"requests":   metrics.Requests,
		"duration":   metrics.Duration.Seconds(),
		"rate":       metrics.Rate,
		"throughput": metrics.Throughput,
		"success":    metrics.Success,
		"latencies": map[string]interface{}{
			"min":  metrics.Latencies.Min.Seconds(),
			"mean": metrics.Latencies.Mean.Seconds(),
			"p50":  metrics.Latencies.P50.Seconds(),
			"p90":  metrics.Latencies.P90.Seconds(),
			"p95":  metrics.Latencies.P95.Seconds(),
			"p99":  metrics.Latencies.P99.Seconds(),
			"max":  metrics.Latencies.Max.Seconds(),
		},
		"status_codes": metrics.StatusCodes,
		"errors":       metrics.Errors,
	}

	return report, nil
}

// generateHdrPlot generates HDR histogram plot data
func (tr *TestReport) generateHdrPlot(binaryFile string) (string, error) {
	// Read the binary file
	file, err := os.Open(binaryFile)
	if err != nil {
		return "", err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Printf("Warning: failed to close file: %v", err)
		}
	}(file)

	// Decode results
	dec := vegeta.NewDecoder(file)

	// Create metrics
	var metrics vegeta.Metrics
	for {
		var result vegeta.Result
		if err := dec.Decode(&result); err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		metrics.Add(&result)
	}
	metrics.Close()

	// Generate HDR histogram
	var buf bytes.Buffer
	histogram := metrics.Histogram
	if histogram != nil {
		// Print histogram data
		for i, bucket := range histogram.Buckets {
			_, err := fmt.Fprintf(&buf, "%.6f %d\n", float64(bucket), histogram.Counts[i])
			if err != nil {
				return "", err
			}
		}
	}

	return buf.String(), nil
}

// Close finalises and closes the test report
func (tr *TestReport) Close() error {
	// Flush and close the CSV file
	if tr.csvWriter != nil {
		tr.csvWriter.Flush()
		if err := tr.csvWriter.Error(); err != nil {
			log.Printf("CSV writer error: %v", err)
		}
	}

	if tr.csvFile != nil {
		if err := tr.csvFile.Close(); err != nil {
			return fmt.Errorf("failed to close CSV file: %w", err)
		}
	}

	// Write the JSON report if enabled
	if tr.config.JSONReportFile != "" && tr.jsonReport != nil {
		fmt.Printf("Create json file: %s\n", tr.config.JSONReportFile)

		jsonData, err := json.MarshalIndent(tr.jsonReport, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON report: %w", err)
		}

		if err := os.WriteFile(tr.config.JSONReportFile, jsonData, 0644); err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
	}

	return nil
}

func main() {
	app := &cli.App{
		Name:  "rpc_perf",
		Usage: "Launch an automated sequence of RPC performance tests on on target blockchain node(s)",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "not-verify-server-alive",
				Aliases: []string{"Z"},
				Usage:   "Don't verify server is still active",
			},
			&cli.BoolFlag{
				Name:    "tmp-test-report",
				Aliases: []string{"R"},
				Usage:   "Generate report in tmp directory",
			},
			&cli.BoolFlag{
				Name:    "test-report",
				Aliases: []string{"u"},
				Usage:   "Generate report in reports area ready for Git repo",
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "Enable verbose output",
			},
			&cli.BoolFlag{
				Name:    "tracing",
				Aliases: []string{"x"},
				Usage:   "Enable verbose and tracing output",
			},
			&cli.BoolFlag{
				Name:    "empty-cache",
				Aliases: []string{"e"},
				Usage:   "Empty OS cache before each test",
			},
			&cli.StringFlag{
				Name:    "max-connections",
				Aliases: []string{"C"},
				Value:   DefaultMaxConn,
				Usage:   "Maximum number of connections",
			},
			&cli.StringFlag{
				Name:    "testing-daemon",
				Aliases: []string{"D"},
				Usage:   "Name of testing daemon",
			},
			&cli.StringFlag{
				Name:    "blockchain",
				Aliases: []string{"b"},
				Value:   "mainnet",
				Usage:   "Blockchain network name",
			},
			&cli.StringFlag{
				Name:    "test-type",
				Aliases: []string{"y"},
				Value:   DefaultTestType,
				Usage:   "Test type (e.g., eth_call, eth_getLogs)",
			},
			&cli.StringFlag{
				Name:    "test-mode",
				Aliases: []string{"m"},
				Value:   DefaultTestMode,
				Usage:   "Test mode: silkworm(1), erigon(2), both(3)",
			},
			&cli.StringFlag{
				Name:    "pattern-file",
				Aliases: []string{"p"},
				Value:   DefaultVegetaPatternTarFile,
				Usage:   "Path to the Vegeta attack pattern file",
			},
			&cli.IntFlag{
				Name:    "repetitions",
				Aliases: []string{"r"},
				Value:   DefaultRepetitions,
				Usage:   "Number of repetitions for each test in sequence",
			},
			&cli.StringFlag{
				Name:    "test-sequence",
				Aliases: []string{"t"},
				Value:   DefaultTestSequence,
				Usage:   "Test sequence as qps:duration,... (e.g., 200:30,400:10)",
			},
			&cli.IntFlag{
				Name:    "wait-after-test-sequence",
				Aliases: []string{"w"},
				Value:   DefaultWaitingTime,
				Usage:   "Wait time between test iterations in seconds",
			},
			&cli.StringFlag{
				Name:    "rpc-daemon-address",
				Aliases: []string{"d"},
				Value:   DefaultErigonAddress,
				Usage:   "RPC daemon address (e.g., 192.2.3.1)",
			},
			&cli.StringFlag{
				Name:    "erigon-dir",
				Aliases: []string{"g"},
				Value:   DefaultErigonBuildDir,
				Usage:   "Path to Erigon folder",
			},
			&cli.StringFlag{
				Name:    "silk-dir",
				Aliases: []string{"s"},
				Value:   DefaultSilkwormBuildDir,
				Usage:   "Path to Silkworm folder",
			},
			&cli.StringFlag{
				Name:    "run-vegeta-on-core",
				Aliases: []string{"c"},
				Value:   DefaultDaemonVegetaOnCore,
				Usage:   "Taskset format for Vegeta (e.g., 0-1:2-3)",
			},
			&cli.StringFlag{
				Name:    "response-timeout",
				Aliases: []string{"T"},
				Value:   DefaultVegetaResponseTimeout,
				Usage:   "Vegeta response timeout",
			},
			&cli.StringFlag{
				Name:    "max-body-rsp",
				Aliases: []string{"M"},
				Value:   DefaultMaxBodyRsp,
				Usage:   "Max bytes to read from response bodies",
			},
			&cli.StringFlag{
				Name:    "json-report",
				Aliases: []string{"j"},
				Usage:   "Generate JSON report at specified path",
			},
			&cli.BoolFlag{
				Name:    "more-percentiles",
				Aliases: []string{"P"},
				Usage:   "Print more percentiles in console report",
			},
			&cli.BoolFlag{
				Name:    "halt-on-vegeta-error",
				Aliases: []string{"H"},
				Usage:   "Consider test failed if Vegeta reports any error",
			},
			&cli.BoolFlag{
				Name:    "instant-report",
				Aliases: []string{"I"},
				Usage:   "Print instant Vegeta report for each test",
			},
		},
		Action: runPerfTests,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func runPerfTests(c *cli.Context) error {
	fmt.Println("Performance Test started")

	// Create configuration from CLI flags
	config := NewConfig()

	config.CheckServerAlive = !c.Bool("not-verify-server-alive")
	config.CreateTestReport = c.Bool("tmp-test-report") || c.Bool("test-report")
	config.VersionedTestReport = c.Bool("test-report")
	config.Verbose = c.Bool("verbose") || c.Bool("tracing")
	config.Tracing = c.Bool("tracing")
	config.EmptyCache = c.Bool("empty-cache")

	config.MaxConnection = c.String("max-connections")
	config.TestingDaemon = c.String("testing-daemon")
	config.ChainName = c.String("blockchain")
	config.TestType = c.String("test-type")
	config.TestMode = c.String("test-mode")
	config.VegetaPatternTarFile = c.String("pattern-file")
	config.Repetitions = c.Int("repetitions")
	config.TestSequence = c.String("test-sequence")
	config.WaitingTime = c.Int("wait-after-test-sequence")
	config.RPCDaemonAddress = c.String("rpc-daemon-address")
	config.ErigonDir = c.String("erigon-dir")
	config.SilkwormDir = c.String("silk-dir")
	config.DaemonVegetaOnCore = c.String("run-vegeta-on-core")
	config.VegetaResponseTimeout = c.String("response-timeout")
	config.MaxBodyRsp = c.String("max-body-rsp")
	config.JSONReportFile = c.String("json-report")
	config.MorePercentiles = c.Bool("more-percentiles")
	config.HaltOnVegetaError = c.Bool("halt-on-vegeta-error")
	config.InstantReport = c.Bool("instant-report")

	// Validate configuration
	if err := config.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Parse test sequence
	sequence, err := ParseTestSequence(config.TestSequence)
	if err != nil {
		return fmt.Errorf("failed to parse test sequence: %w", err)
	}

	// Create the test report
	testReport := NewTestReport(config)

	// Create the performance test
	perfTest, err := NewPerfTest(config, testReport)
	if err != nil {
		return fmt.Errorf("failed to initialize performance test: %w", err)
	}
	defer func(perfTest *PerfTest, initial bool) {
		err := perfTest.Cleanup(initial)
		if err != nil {
			log.Printf("Failed to cleanup: %v", err)
		}
	}(perfTest, false)

	// Print test configuration
	fmt.Printf("Test repetitions: %d on sequence: %s for pattern: %s\n",
		config.Repetitions, config.TestSequence, config.VegetaPatternTarFile)

	// Open the test report if needed
	if config.CreateTestReport {
		if err := testReport.Open(); err != nil {
			return fmt.Errorf("failed to open test report: %w", err)
		}
		defer func(testReport *TestReport) {
			err := testReport.Close()
			if err != nil {
				log.Printf("Failed to close test report: %v", err)
			}
		}(testReport)
	}

	// Create context
	ctx := context.Background()

	// Run tests based on test mode
	if config.TestMode == "1" || config.TestMode == "3" {
		fmt.Println("Testing Silkworm...")
		if err := perfTest.ExecuteSequence(ctx, sequence, Silkworm); err != nil {
			fmt.Printf("Performance Test failed, error: %v\n", err)
			return err
		}

		if config.TestMode == "3" {
			fmt.Println("--------------------------------------------------------------------------------------------")
		}
	}

	if config.TestMode == "2" || config.TestMode == "3" {
		fmt.Println("Testing Erigon...")
		if err := perfTest.ExecuteSequence(ctx, sequence, Erigon); err != nil {
			fmt.Printf("Performance Test failed, error: %v\n", err)
			return err
		}
	}

	fmt.Println("Performance Test completed successfully.")
	return nil
}
