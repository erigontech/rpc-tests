package perf

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// PerfMetrics holds the results of a performance test.
type PerfMetrics struct {
	ClientName    string
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

// JSONReport represents the structure of the JSON performance report.
type JSONReport struct {
	Platform      PlatformInfo      `json:"platform"`
	Configuration ConfigurationInfo `json:"configuration"`
	Results       []JSONTestResult  `json:"results"`
}

// PlatformInfo holds platform hardware and software information.
type PlatformInfo struct {
	Vendor       string `json:"vendor"`
	Product      string `json:"product"`
	Board        string `json:"board"`
	CPU          string `json:"cpu"`
	Bogomips     string `json:"bogomips"`
	Kernel       string `json:"kernel"`
	GCCVersion   string `json:"gccVersion"`
	GoVersion    string `json:"goVersion"`
	ClientCommit string `json:"clientCommit"`
}

// ConfigurationInfo holds test configuration information.
type ConfigurationInfo struct {
	TestingClient   string `json:"testingClient"`
	TestingAPI      string `json:"testingApi"`
	TestSequence    string `json:"testSequence"`
	TestRepetitions int    `json:"testRepetitions"`
	VegetaFile      string `json:"vegetaFile"`
	VegetaChecksum  string `json:"vegetaChecksum"`
	Taskset         string `json:"taskset"`
}

// JSONTestResult holds results for a single QPS/duration test.
type JSONTestResult struct {
	QPS             int              `json:"qps"`
	Duration        int              `json:"duration"`
	TestRepetitions []RepetitionInfo `json:"testRepetitions"`
}

// RepetitionInfo holds information for a single test repetition.
type RepetitionInfo struct {
	VegetaBinary        string                 `json:"vegetaBinary"`
	VegetaReport        map[string]interface{} `json:"vegetaReport"`
	VegetaReportHdrPlot string                 `json:"vegetaReportHdrPlot"`
}

// TestReport manages CSV and JSON report generation.
type TestReport struct {
	Config         *Config
	RunDirs        *RunDirs
	csvFile        *os.File
	csvWriter      *csv.Writer
	jsonReport     *JSONReport
	hardware       *Hardware
	currentTestIdx int
}

// NewTestReport creates a new test report instance.
func NewTestReport(config *Config, dirs *RunDirs) *TestReport {
	return &TestReport{
		Config:         config,
		RunDirs:        dirs,
		hardware:       &Hardware{},
		currentTestIdx: -1,
	}
}

// Open initialises the test report and writes headers.
func (tr *TestReport) Open() error {
	if err := tr.createCSVFile(); err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}

	checksum := GetFileChecksum(tr.Config.VegetaPatternTarFile)
	gccVersion := GetGCCVersion()
	goVersion := GetGoVersion()
	kernelVersion := GetKernelVersion()
	cpuModel := tr.hardware.GetCPUModel()
	bogomips := tr.hardware.GetBogomips()

	var clientCommit string
	if tr.Config.ClientBuildDir != "" {
		clientCommit = GetGitCommit(tr.Config.ClientBuildDir)
	} else {
		clientCommit = "none"
	}

	if err := tr.writeTestHeader(cpuModel, bogomips, kernelVersion, checksum,
		gccVersion, goVersion, clientCommit); err != nil {
		return fmt.Errorf("failed to write test header: %w", err)
	}

	if tr.Config.JSONReportFile != "" {
		tr.initializeJSONReport(cpuModel, bogomips, kernelVersion, checksum,
			gccVersion, goVersion, clientCommit)
	}

	return nil
}

// createCSVFile creates the CSV report file with appropriate naming.
func (tr *TestReport) createCSVFile() error {
	extension := tr.hardware.NormalizedProduct()
	if extension == "systemproductname" {
		extension = tr.hardware.NormalizedBoard()
	}

	csvFolder := tr.hardware.NormalizedVendor() + "_" + extension
	var csvFolderPath string
	if tr.Config.VersionedTestReport {
		csvFolderPath = filepath.Join("./perf/reports", tr.Config.ChainName, csvFolder)
	} else {
		csvFolderPath = filepath.Join(tr.RunDirs.RunTestDir, tr.Config.ChainName, csvFolder)
	}

	if err := os.MkdirAll(csvFolderPath, 0755); err != nil {
		return fmt.Errorf("failed to create CSV folder: %w", err)
	}

	timestamp := time.Now().Format("20060102150405")
	var csvFilename string
	if tr.Config.TestingClient != "" {
		csvFilename = fmt.Sprintf("%s_%s_%s_perf.csv",
			tr.Config.TestType, timestamp, tr.Config.TestingClient)
	} else {
		csvFilename = fmt.Sprintf("%s_%s_perf.csv",
			tr.Config.TestType, timestamp)
	}

	csvFilepath := filepath.Join(csvFolderPath, csvFilename)

	file, err := os.Create(csvFilepath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}

	tr.csvFile = file
	tr.csvWriter = csv.NewWriter(file)

	fmt.Printf("Perf report file: %s\n\n", csvFilepath)

	return nil
}

// writeTestHeader writes the test configuration header to CSV.
func (tr *TestReport) writeTestHeader(cpuModel, bogomips, kernelVersion, checksum, gccVersion, goVersion, clientCommit string) error {
	emptyRow := make([]string, 14)

	if err := tr.csvWriter.Write(append(emptyRow[:12], "vendor", tr.hardware.Vendor())); err != nil {
		return err
	}

	product := tr.hardware.Product()
	if product != "System Product Name" {
		if err := tr.csvWriter.Write(append(emptyRow[:12], "product", product)); err != nil {
			return err
		}
	} else {
		if err := tr.csvWriter.Write(append(emptyRow[:12], "board", tr.hardware.Board())); err != nil {
			return err
		}
	}

	rows := [][2]string{
		{"cpu", cpuModel},
		{"bogomips", bogomips},
		{"kernel", kernelVersion},
		{"taskset", tr.Config.ClientVegetaOnCore},
		{"vegetaFile", tr.Config.VegetaPatternTarFile},
		{"vegetaChecksum", checksum},
		{"gccVersion", gccVersion},
		{"goVersion", goVersion},
		{"clientVersion", clientCommit},
	}
	for _, r := range rows {
		if err := tr.csvWriter.Write(append(emptyRow[:12], r[0], r[1])); err != nil {
			return err
		}
	}

	for range 2 {
		if err := tr.csvWriter.Write([]string{}); err != nil {
			return err
		}
	}

	headers := []string{
		"ClientName", "TestNo", "Repetition", "Qps", "Time(secs)",
		"Min", "Mean", "50", "90", "95", "99", "Max", "Ratio", "Error",
	}
	if err := tr.csvWriter.Write(headers); err != nil {
		return err
	}
	tr.csvWriter.Flush()

	return tr.csvWriter.Error()
}

// initializeJSONReport initializes the JSON report structure.
func (tr *TestReport) initializeJSONReport(cpuModel, bogomips, kernelVersion, checksum,
	gccVersion, goVersion, clientCommit string) {

	tr.jsonReport = &JSONReport{
		Platform: PlatformInfo{
			Vendor:       strings.TrimSpace(tr.hardware.Vendor()),
			Product:      strings.TrimSpace(tr.hardware.Product()),
			Board:        strings.TrimSpace(tr.hardware.Board()),
			CPU:          strings.TrimSpace(cpuModel),
			Bogomips:     strings.TrimSpace(bogomips),
			Kernel:       strings.TrimSpace(kernelVersion),
			GCCVersion:   strings.TrimSpace(gccVersion),
			GoVersion:    strings.TrimSpace(goVersion),
			ClientCommit: strings.TrimSpace(clientCommit),
		},
		Configuration: ConfigurationInfo{
			TestingClient:   tr.Config.TestingClient,
			TestingAPI:      tr.Config.TestType,
			TestSequence:    tr.Config.TestSequence,
			TestRepetitions: tr.Config.Repetitions,
			VegetaFile:      tr.Config.VegetaPatternTarFile,
			VegetaChecksum:  checksum,
			Taskset:         tr.Config.ClientVegetaOnCore,
		},
		Results: []JSONTestResult{},
	}
}

// WriteTestReport writes a test result to the report.
func (tr *TestReport) WriteTestReport(metrics *PerfMetrics) error {
	row := []string{
		metrics.ClientName,
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

	if tr.Config.JSONReportFile != "" {
		if err := tr.writeTestReportToJSON(metrics); err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
	}

	return nil
}

// writeTestReportToJSON writes a test result to the JSON report.
func (tr *TestReport) writeTestReportToJSON(metrics *PerfMetrics) error {
	if metrics.Repetition == 0 {
		tr.currentTestIdx++
		tr.jsonReport.Results = append(tr.jsonReport.Results, JSONTestResult{
			QPS:             metrics.QPS,
			Duration:        metrics.Duration,
			TestRepetitions: []RepetitionInfo{},
		})
	}

	jsonReportData, err := generateJSONReport(tr.Config.BinaryFileFullPathname)
	if err != nil {
		return fmt.Errorf("failed to generate JSON report: %w", err)
	}

	hdrPlot, err := generateHdrPlot(tr.Config.BinaryFileFullPathname)
	if err != nil {
		return fmt.Errorf("failed to generate HDR plot: %w", err)
	}

	repetitionInfo := RepetitionInfo{
		VegetaBinary:        tr.Config.BinaryFile,
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

// generateJSONReport generates a JSON report from a vegeta binary file.
func generateJSONReport(binaryFile string) (map[string]interface{}, error) {
	file, err := os.Open(binaryFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	dec := vegeta.NewDecoder(file)
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

// generateHdrPlot generates HDR histogram plot data from a vegeta binary file.
func generateHdrPlot(binaryFile string) (string, error) {
	file, err := os.Open(binaryFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	dec := vegeta.NewDecoder(file)
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

	var buf bytes.Buffer
	histogram := metrics.Histogram
	if histogram != nil {
		for i, bucket := range histogram.Buckets {
			if _, err := fmt.Fprintf(&buf, "%.6f %d\n", float64(bucket), histogram.Counts[i]); err != nil {
				return "", err
			}
		}
	}

	return buf.String(), nil
}

// Close finalises and closes the test report.
func (tr *TestReport) Close() error {
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

	if tr.Config.JSONReportFile != "" && tr.jsonReport != nil {
		fmt.Printf("Create json file: %s\n", tr.Config.JSONReportFile)

		jsonData, err := json.MarshalIndent(tr.jsonReport, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON report: %w", err)
		}

		if err := os.WriteFile(tr.Config.JSONReportFile, jsonData, 0644); err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
	}

	return nil
}
