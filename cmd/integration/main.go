package main

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	bzip2w "github.com/dsnet/compress/bzip2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/josephburnett/jd/v2"
)

const (
	DaemonOnOtherPort   = "other-daemon"
	DaemonOnDefaultPort = "rpcdaemon"
	None                = "none"
	ExternalProvider    = "external-provider"
	TimeInterval        = 100 * time.Millisecond
	MaxTime             = 200
	TempDirname         = "./temp_rpc_tests"
)

var (
	apiNotCompared = []string{
		"mainnet/engine_getClientVersionV1",
		"mainnet/trace_rawTransaction",
		"mainnet/engine_",
	}

	testsNotCompared        = []string{}
	testsNotComparedMessage = []string{}
	testsNotComparedError   = []string{}

	// testsOnLatest - add your list here
	testsOnLatest = []string{
		"mainnet/debug_traceBlockByNumber/test_24.json",
		"mainnet/debug_traceBlockByNumber/test_30.json",
		"mainnet/debug_traceCall/test_22.json",
		"mainnet/debug_traceCall/test_33.json",
		"mainnet/debug_traceCall/test_34.json",
		"mainnet/debug_traceCall/test_35.json",
		"mainnet/debug_traceCall/test_36.json",
		"mainnet/debug_traceCall/test_37.json",
		"mainnet/debug_traceCall/test_38.json",
		"mainnet/debug_traceCall/test_39.json",
		"mainnet/debug_traceCall/test_40.json",
		"mainnet/debug_traceCall/test_41.json",
		"mainnet/debug_traceCall/test_42.json",
		"mainnet/debug_traceCall/test_43.json",
		"mainnet/debug_traceCallMany/test_11.json",
		"mainnet/debug_traceCallMany/test_12.json",
		"mainnet/eth_blobBaseFee", // works always on the latest block
		"mainnet/eth_blockNumber", // works always on the latest block
		"mainnet/eth_call/test_20.json",
		"mainnet/eth_call/test_28.json",
		"mainnet/eth_call/test_29.json",
		"mainnet/eth_call/test_36.json",
		"mainnet/eth_call/test_37.json",
		"mainnet/eth_callBundle/test_09.json",
		"mainnet/eth_createAccessList/test_18.json",
		"mainnet/eth_createAccessList/test_19.json",
		"mainnet/eth_createAccessList/test_20.json",
		"mainnet/eth_createAccessList/test_22.json",
		"mainnet/eth_estimateGas/test_01",
		"mainnet/eth_estimateGas/test_02",
		"mainnet/eth_estimateGas/test_03",
		"mainnet/eth_estimateGas/test_04",
		"mainnet/eth_estimateGas/test_05",
		"mainnet/eth_estimateGas/test_06",
		"mainnet/eth_estimateGas/test_07",
		"mainnet/eth_estimateGas/test_08",
		"mainnet/eth_estimateGas/test_09",
		"mainnet/eth_estimateGas/test_10",
		"mainnet/eth_estimateGas/test_11",
		"mainnet/eth_estimateGas/test_12",
		"mainnet/eth_estimateGas/test_21",
		"mainnet/eth_estimateGas/test_22",
		"mainnet/eth_estimateGas/test_23",
		"mainnet/eth_estimateGas/test_27",
		"mainnet/eth_feeHistory/test_07.json",
		"mainnet/eth_feeHistory/test_22.json",
		"mainnet/eth_gasPrice", // works always on the latest block
		"mainnet/eth_getBalance/test_03.json",
		"mainnet/eth_getBalance/test_26.json",
		"mainnet/eth_getBalance/test_27.json",
		"mainnet/eth_getBlockTransactionCountByNumber/test_03.json",
		"mainnet/eth_getBlockByNumber/test_10.json",
		"mainnet/eth_getBlockByNumber/test_27.json",
		"mainnet/eth_getBlockReceipts/test_07.json",
		"mainnet/eth_getCode/test_05.json",
		"mainnet/eth_getCode/test_06.json",
		"mainnet/eth_getCode/test_07.json",
		"mainnet/eth_getLogs/test_21.json",
		"mainnet/eth_getProof/test_01.json",
		"mainnet/eth_getProof/test_02.json",
		"mainnet/eth_getProof/test_03.json",
		"mainnet/eth_getProof/test_04.json",
		"mainnet/eth_getProof/test_05.json",
		"mainnet/eth_getProof/test_06.json",
		"mainnet/eth_getProof/test_07.json",
		"mainnet/eth_getProof/test_08.json",
		"mainnet/eth_getProof/test_09.json",
		"mainnet/eth_getProof/test_10.json",
		"mainnet/eth_getProof/test_11.json",
		"mainnet/eth_getProof/test_12.json",
		"mainnet/eth_getProof/test_13.json",
		"mainnet/eth_getProof/test_14.json",
		"mainnet/eth_getProof/test_15.json",
		"mainnet/eth_getProof/test_16.json",
		"mainnet/eth_getProof/test_17.json",
		"mainnet/eth_getProof/test_18.json",
		"mainnet/eth_getProof/test_19.json",
		"mainnet/eth_getProof/test_20.json",
		"mainnet/eth_getRawTransactionByBlockNumberAndIndex/test_11.json",
		"mainnet/eth_getRawTransactionByBlockNumberAndIndex/test_12.json",
		"mainnet/eth_getRawTransactionByBlockNumberAndIndex/test_13.json",
		"mainnet/eth_getStorageAt/test_04.json",
		"mainnet/eth_getStorageAt/test_07.json",
		"mainnet/eth_getStorageAt/test_08.json",
		"mainnet/eth_getTransactionByBlockNumberAndIndex/test_02.json",
		"mainnet/eth_getTransactionByBlockNumberAndIndex/test_08.json",
		"mainnet/eth_getTransactionByBlockNumberAndIndex/test_09.json",
		"mainnet/eth_getTransactionCount/test_02.json",
		"mainnet/eth_getTransactionCount/test_07.json",
		"mainnet/eth_getTransactionCount/test_08.json",
		"mainnet/eth_getUncleCountByBlockNumber/test_03.json",
		"mainnet/eth_getUncleByBlockNumberAndIndex/test_02.json",
		"mainnet/eth_maxPriorityFeePerGas",
		"mainnet/eth_simulateV1/test_04.json",
		"mainnet/eth_simulateV1/test_05.json",
		"mainnet/eth_simulateV1/test_06.json",
		"mainnet/eth_simulateV1/test_07.json",
		"mainnet/eth_simulateV1/test_12.json",
		"mainnet/eth_simulateV1/test_13.json",
		"mainnet/eth_simulateV1/test_14.json",
		"mainnet/eth_simulateV1/test_15.json",
		"mainnet/eth_simulateV1/test_16.json",
		"mainnet/eth_simulateV1/test_25.json",
		"mainnet/eth_simulateV1/test_27.json",
		"mainnet/erigon_blockNumber/test_4.json",
		"mainnet/erigon_blockNumber/test_6.json",
		"mainnet/ots_hasCode/test_10.json",
		"mainnet/ots_searchTransactionsBefore/test_02.json",
		"mainnet/parity_listStorageKeys",
		"mainnet/trace_block/test_25.json",
		"mainnet/trace_call/test_26.json",
		"mainnet/trace_call/test_27.json",
		"mainnet/trace_call/test_28.json",
		"mainnet/trace_call/test_29.json",
		"mainnet/trace_callMany/test_15.json",
		"mainnet/trace_filter/test_25.json",
		"mainnet/trace_replayBlockTransactions/test_36.json",
	}
)

// Supported compression types
const (
	GzipCompression  = ".gz"
	Bzip2Compression = ".bz2"
	NoCompression    = ""
)

// --- Helper Functions ---

// getCompressionType determines the compression from the filename extension.
func getCompressionType(filename string) string {
	if strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tgz") {
		return GzipCompression
	}
	if strings.HasSuffix(filename, ".tar.bz2") || strings.HasSuffix(filename, ".tbz") {
		return Bzip2Compression
	}
	return NoCompression
}

// --- Archiving Logic ---

// createArchive creates a compressed or uncompressed tar archive.
func createArchive(archivePath string, files []string) error {
	// Create the output file
	outFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func(outFile *os.File) {
		err := outFile.Close()
		if err != nil {
			fmt.Printf("Failed to close output file: %v\n", err)
		}
	}(outFile)

	// Wrap the output file with the correct compression writer (if any)
	var writer io.WriteCloser = outFile
	compressionType := getCompressionType(archivePath)

	switch compressionType {
	case GzipCompression:
		writer = gzip.NewWriter(outFile)
	case Bzip2Compression:
		config := &bzip2w.WriterConfig{Level: bzip2w.BestCompression}
		writer, err = bzip2w.NewWriter(outFile, config)
		if err != nil {
			return fmt.Errorf("failed to create bzip2 writer: %w", err)
		}
	}

	// Create the tar writer
	tarWriter := tar.NewWriter(writer)
	defer func(writer io.WriteCloser, tarWriter *tar.Writer) {
		// Explicitly close the compression writer if it was used (before closing the tar writer)
		if compressionType != NoCompression {
			if err := writer.Close(); err != nil {
				fmt.Printf("failed to close compression writer: %v\n", err)
			}
		}

		err := tarWriter.Close()
		if err != nil {
			fmt.Printf("Failed to close tar writer: %v\n", err)
		}
	}(writer, tarWriter)

	// Add files to the archive
	for _, file := range files {
		err := addFileToTar(tarWriter, file, "")
		if err != nil {
			return fmt.Errorf("failed to add file %s: %w", file, err)
		}
	}

	return nil
}

// addFileToTar recursively adds a file or directory to the tar archive.
func addFileToTar(tarWriter *tar.Writer, filePath, baseDir string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	// Determine the name inside the archive (relative path)
	var link string
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		link, err = os.Readlink(filePath)
		if err != nil {
			return err
		}
	}

	// If baseDir is not empty, use the relative path, otherwise use the basename
	nameInArchive := filePath
	if baseDir != "" && strings.HasPrefix(filePath, baseDir) {
		nameInArchive = filePath[len(baseDir)+1:]
	} else {
		nameInArchive = filepath.Base(filePath)
	}

	// Create the tar Header
	header, err := tar.FileInfoHeader(fileInfo, link)
	if err != nil {
		return err
	}
	header.Name = nameInArchive

	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	// Write file contents if it's a regular file
	if fileInfo.Mode().IsRegular() {
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tarWriter, file); err != nil {
			_ = file.Close()
			return err
		}
		_ = file.Close()
	}

	// Recurse into directories
	if fileInfo.IsDir() {
		dirEntries, err := os.ReadDir(filePath)
		if err != nil {
			return err
		}
		for _, entry := range dirEntries {
			fullPath := filepath.Join(filePath, entry.Name())
			// Keep the original baseDir if it was set, otherwise set it to the current path's parent
			newBaseDir := baseDir
			if baseDir == "" {
				// Special handling for the root call: use the current path as the new base.
				// This ensures nested files have relative paths within the archive.
				newBaseDir = filePath
			}
			if err := addFileToTar(tarWriter, fullPath, newBaseDir); err != nil {
				return err
			}
		}
	}

	return nil
}

func reopenFile(filePath string, file *os.File) (*os.File, error) {
	err := file.Close()
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return nil, err
	}
	file, err = os.Open(filePath)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func autodetectCompression(archivePath string, inFile *os.File) (string, error) {
	// Assume we have no compression and try to detect it if the tar header is invalid
	compressionType := NoCompression
	tarReader := tar.NewReader(inFile)
	_, err := tarReader.Next()
	if err != nil && !errors.Is(err, io.EOF) {
		// Reopen the file and check if it's gzip encoded
		inFile, err = reopenFile(archivePath, inFile)
		if err != nil {
			return compressionType, err
		}
		_, err = gzip.NewReader(inFile)
		if err == nil {
			compressionType = GzipCompression
		} else {
			// Reopen the file and check if it's bzip2 encoded
			inFile, err = reopenFile(archivePath, inFile)
			if err != nil {
				return compressionType, err
			}
			_, err = tar.NewReader(bzip2.NewReader(inFile)).Next()
			if err == nil {
				compressionType = Bzip2Compression
			}
		}
		err = inFile.Close()
		if err != nil {
			return compressionType, err
		}
	}
	return compressionType, nil
}

// extractArchive extracts a compressed or uncompressed tar archive.
func extractArchive(archivePath string, sanitizeExtension bool) ([]string, error) {
	// Open the archive file
	inputFile, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive: %w", err)
	}
	defer func(inFile *os.File) {
		_ = inFile.Close()
	}(inputFile)

	// Wrap the input file with the correct compression reader
	compressionType := getCompressionType(archivePath)
	if compressionType == NoCompression {
		// Possibly handle the corner case where the file is compressed but has tar extension
		compressionType, err = autodetectCompression(archivePath, inputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to autodetect compression for archive: %w", err)
		}
		if compressionType != NoCompression {
			// If any compression was detected, optionally rename and reopen the archive file
			if sanitizeExtension {
				err = os.Rename(archivePath, archivePath+compressionType)
				if err != nil {
					return nil, err
				}
				archivePath = archivePath + compressionType
			}
		}
		inputFile, err = os.Open(archivePath)
		if err != nil {
			return nil, err
		}
	}

	var reader io.Reader
	switch compressionType {
	case GzipCompression:
		if reader, err = gzip.NewReader(inputFile); err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
	case Bzip2Compression:
		reader = bzip2.NewReader(inputFile)
	case NoCompression:
		reader = inputFile
	}

	// Iterate over files in the archive and extract them
	tarReader := tar.NewReader(reader)
	tmpFilePaths := []string{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		targetPath := filepath.Dir(archivePath) + "/" + header.Name

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err = os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			// Ensure the parent directory exists before creating the file
			if err = os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return nil, fmt.Errorf("failed to create parent directory for %s: %w", targetPath, err)
			}

			// Create the file
			outputFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return nil, fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}

			// Write content
			if _, err = io.Copy(outputFile, tarReader); err != nil {
				err = outputFile.Close()
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("failed to write file content for %s: %w", targetPath, err)
			}
			tmpFilePaths = append(tmpFilePaths, targetPath)
			err = outputFile.Close()
			if err != nil {
				return nil, err
			}
		default:
			fmt.Printf("WARN: skipping unsupported file type %c: %s\n", header.Typeflag, targetPath)
		}
	}

	return tmpFilePaths, nil
}

type JsonDiffKind int

const (
	JdLibrary JsonDiffKind = iota
	JsonDiffTool
	DiffTool
)

func (k JsonDiffKind) String() string {
	return [...]string{"jd", "json-diff", "diff"}[k]
}

// ParseJsonDiffKind converts a string into a JsonDiffKind enum type
func ParseJsonDiffKind(s string) (JsonDiffKind, error) {
	switch strings.ToLower(s) {
	case "jd":
		return JdLibrary, nil
	case "json-diff":
		return JsonDiffTool, nil
	case "diff":
		return DiffTool, nil
	default:
		return JdLibrary, fmt.Errorf("invalid JsonDiffKind value: %s", s)
	}
}

type Config struct {
	ExitOnFail            bool
	DaemonUnderTest       string
	DaemonAsReference     string
	LoopNumber            int
	VerboseLevel          int
	ReqTestNumber         int
	ForceDumpJSONs        bool
	ExternalProviderURL   string
	DaemonOnHost          string
	ServerPort            int
	EnginePort            int
	TestingAPIsWith       string
	TestingAPIs           string
	VerifyWithDaemon      bool
	Net                   string
	JSONDir               string
	ResultsDir            string
	OutputDir             string
	ExcludeAPIList        string
	ExcludeTestList       string
	StartTest             string
	JWTSecret             string
	DisplayOnlyFail       bool
	TransportType         string
	Parallel              bool
	DiffKind              JsonDiffKind
	WithoutCompareResults bool
	WaitingTime           int
	DoNotCompareError     bool
	TestsOnLatestBlock    bool
	LocalServer           string
	SanitizeArchiveExt    bool
	CpuProfile            string
	MemProfile            string
	TraceFile             string
}

type TestResult struct {
	Success bool
	Error   error
	Test    *TestDescriptor
}

type TestDescriptor struct {
	Name          string
	Number        int
	TransportType string
	ResultChan    chan TestResult
}

type JsonRpcResponseMetadata struct {
	PathOptions json.RawMessage `json:"pathOptions"`
}

type JsonRpcTestMetadata struct {
	Request  interface{}              `json:"request"`
	Response *JsonRpcResponseMetadata `json:"response"`
}

type JsonRpcTest struct {
	Identifier  string               `json:"id"`
	Reference   string               `json:"reference"`
	Description string               `json:"description"`
	Metadata    *JsonRpcTestMetadata `json:"metadata"`
}

type JSONRPCCommand struct {
	Request  interface{}  `json:"request"`
	Response interface{}  `json:"response"`
	TestInfo *JsonRpcTest `json:"test"`
}

func NewConfig() *Config {
	return &Config{
		ExitOnFail:            true,
		DaemonUnderTest:       DaemonOnDefaultPort,
		DaemonAsReference:     None,
		LoopNumber:            1,
		VerboseLevel:          0,
		ReqTestNumber:         -1,
		ForceDumpJSONs:        false,
		ExternalProviderURL:   "",
		DaemonOnHost:          "localhost",
		ServerPort:            0,
		EnginePort:            0,
		TestingAPIsWith:       "",
		TestingAPIs:           "",
		VerifyWithDaemon:      false,
		Net:                   "mainnet",
		ResultsDir:            "results",
		JWTSecret:             "",
		DisplayOnlyFail:       false,
		TransportType:         "http",
		Parallel:              true,
		DiffKind:              JdLibrary,
		WithoutCompareResults: false,
		WaitingTime:           0,
		DoNotCompareError:     false,
		TestsOnLatestBlock:    false,
		SanitizeArchiveExt:    false,
	}
}

func (c *Config) parseFlags() error {
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

	diffKind := flag.String("j", c.DiffKind.String(), "diff for JSON values, one of: jd, json-diff, diff")
	flag.StringVar(diffKind, "json-diff", c.DiffKind.String(), "diff for JSON values, one of: jd, json-diff, diff")

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

	// Validation and conflicts
	if *waitingTime > 0 && c.Parallel {
		return fmt.Errorf("waiting-time is not compatible with parallel tests")
	}

	if *daemonPort && *compareErigon {
		return fmt.Errorf("daemon-port is not compatible with compare-erigon-rpcdaemon")
	}

	if *testNumber != -1 && (*excludeTestList != "" || *excludeAPIList != "") {
		return fmt.Errorf("run-test is not compatible with exclude-api-list or exclude-test-list")
	}

	if *apiList != "" && *excludeAPIList != "" {
		return fmt.Errorf("api-list is not compatible with exclude-api-list")
	}

	if *compareErigon && *withoutCompare {
		return fmt.Errorf("compare-erigon-rpcdaemon is not compatible with without-compare-results")
	}

	// Apply configuration
	c.ExitOnFail = !*continueOnFail
	c.VerboseLevel = *verbose
	c.ReqTestNumber = *testNumber
	c.LoopNumber = *loops
	c.DaemonOnHost = *host
	c.ServerPort = *port
	c.EnginePort = *enginePort
	c.DisplayOnlyFail = *displayOnlyFail
	c.TestingAPIsWith = *apiListWith
	c.TestingAPIs = *apiList
	c.Net = *blockchain
	c.ExcludeAPIList = *excludeAPIList
	c.ExcludeTestList = *excludeTestList
	c.StartTest = *startTest
	c.TransportType = *transportType
	c.WaitingTime = *waitingTime
	c.ForceDumpJSONs = *dumpResponse
	c.WithoutCompareResults = *withoutCompare
	c.DoNotCompareError = *doNotCompareError
	c.TestsOnLatestBlock = *testOnLatest
	c.Parallel = !*serial
	c.CpuProfile = *cpuProfile
	c.MemProfile = *memProfile
	c.TraceFile = *traceFile

	kind, err := ParseJsonDiffKind(*diffKind)
	if err != nil {
		return err
	}
	c.DiffKind = kind

	if *daemonPort {
		c.DaemonUnderTest = DaemonOnOtherPort
	}

	if *externalProvider != "" {
		c.DaemonAsReference = ExternalProvider
		c.ExternalProviderURL = *externalProvider
		c.VerifyWithDaemon = true
	}

	if *compareErigon {
		c.VerifyWithDaemon = true
		c.DaemonAsReference = DaemonOnDefaultPort
	}

	if *createJWT != "" {
		if err := generateJWTSecret(*createJWT, 64); err != nil {
			return fmt.Errorf("failed to create JWT secret: %v", err)
		}
		secret, err := getJWTSecret(*createJWT)
		if err != nil {
			return fmt.Errorf("failed to read JWT secret: %v", err)
		}
		c.JWTSecret = secret
	} else if *jwtFile != "" {
		secret, err := getJWTSecret(*jwtFile)
		if err != nil {
			return fmt.Errorf("secret file not found: %s", *jwtFile)
		}
		c.JWTSecret = secret
	}

	// Validate transport type
	if *transportType != "" {
		types := strings.Split(*transportType, ",")
		for _, t := range types {
			if t != "websocket" && t != "http" && t != "http_comp" && t != "https" && t != "websocket_comp" {
				return fmt.Errorf("invalid connection type: %s", t)
			}
		}
	}

	c.UpdateDirs()

	// Remove output directory if exists
	if _, err := os.Stat(c.OutputDir); err == nil {
		err := os.RemoveAll(c.OutputDir)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) UpdateDirs() {
	c.JSONDir = "./integration/" + c.Net + "/"
	c.OutputDir = c.JSONDir + c.ResultsDir + "/"
	if c.ServerPort == 0 {
		c.ServerPort = 8545
	}
	if c.EnginePort == 0 {
		c.EnginePort = 8551
	}
	c.LocalServer = "http://" + c.DaemonOnHost + ":" + strconv.Itoa(c.ServerPort)
}

func usage() {
	fmt.Println("Usage: rpc_int [options]")
	fmt.Println("")
	fmt.Println("Launch an automated sequence of RPC integration tests on target blockchain node(s)")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  -h, --help                        print this help")
	fmt.Println("  -j, --json-diff                   use json-diff to make compare [default use json-diff]")
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

func getTarget(targetType, method string, config *Config) string {
	if targetType == ExternalProvider {
		return config.ExternalProviderURL
	}

	if config.VerifyWithDaemon && targetType == DaemonOnOtherPort && strings.Contains(method, "engine_") {
		return config.DaemonOnHost + ":51516"
	}

	if config.VerifyWithDaemon && targetType == DaemonOnOtherPort {
		return config.DaemonOnHost + ":51515"
	}

	if targetType == DaemonOnOtherPort && strings.Contains(method, "engine_") {
		return config.DaemonOnHost + ":51516"
	}

	if targetType == DaemonOnOtherPort {
		return config.DaemonOnHost + ":51515"
	}

	if strings.Contains(method, "engine_") {
		port := config.EnginePort
		if port == 0 {
			port = 8551
		}
		return config.DaemonOnHost + ":" + strconv.Itoa(port)
	}

	port := config.ServerPort
	if port == 0 {
		port = 8545
	}
	return config.DaemonOnHost + ":" + strconv.Itoa(port)
}

func getJSONFilenameExt(targetType, target string) string {
	parts := strings.Split(target, ":")
	port := ""
	if len(parts) > 1 {
		port = parts[1]
	}

	if targetType == DaemonOnOtherPort {
		return "_" + port + "-daemon.json"
	}
	if targetType == ExternalProvider {
		return "-external_provider_url.json"
	}
	return "_" + port + "-rpcdaemon.json"
}

func getJWTSecret(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	contents := string(data)
	if len(contents) >= 2 && contents[:2] == "0x" {
		return contents[2:], nil
	}
	return strings.TrimSpace(contents), nil
}

func generateJWTSecret(filename string, length int) error {
	if length <= 0 {
		length = 64
	}
	randomBytes := make([]byte, length/2)
	if _, err := rand.Read(randomBytes); err != nil {
		return err
	}
	randomHex := "0x" + hex.EncodeToString(randomBytes)
	if err := os.WriteFile(filename, []byte(randomHex), 0600); err != nil {
		return err
	}
	fmt.Printf("Secret File '%s' created with success!\n", filename)
	return nil
}

func toLowerCase(inputFile, outputFile string) error {
	inputContent, err := os.ReadFile(inputFile)
	if err != nil {
		return err
	}

	outputContent := []byte(strings.ToLower(string(inputContent)))

	err = os.WriteFile(outputFile, outputContent, 0644)
	if err != nil {
		return err
	}
	return nil
}

func replaceMessage(inputFile, outputFile, matchedString string) error {
	inData, err := os.ReadFile(inputFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(inData), "\n")
	var output []string
	for _, line := range lines {
		if !strings.Contains(line, matchedString) {
			output = append(output, line)
		} else {
			output = append(output, "     \"message\": \"\"")
		}
	}

	return os.WriteFile(outputFile, []byte(strings.Join(output, "\n")), 0644)
}

func extractNumber(filename string) int {
	re := regexp.MustCompile(`\d+`)
	match := re.FindString(filename)
	if match != "" {
		num, _ := strconv.Atoi(match)
		return num
	}
	return 0
}

func checkTestNameForNumber(testName string, reqTestNumber int) bool {
	if reqTestNumber == -1 {
		return true
	}
	pattern := "_0*" + strconv.Itoa(reqTestNumber) + "($|[^0-9])"
	matched, _ := regexp.MatchString(pattern, testName)
	return matched
}

func isSkipped(currAPI, testName string, globalTestNumber int, config *Config) bool {
	apiFullName := config.Net + "/" + currAPI
	apiFullTestName := config.Net + "/" + testName

	if (config.ReqTestNumber == -1 || config.TestingAPIs != "" || config.TestingAPIsWith != "") &&
		!(config.ReqTestNumber != -1 && (config.TestingAPIs != "" || config.TestingAPIsWith != "")) &&
		config.ExcludeAPIList == "" && config.ExcludeTestList == "" {
		for _, currTestName := range apiNotCompared {
			if strings.Contains(apiFullName, currTestName) {
				return true
			}
		}
		for _, currTest := range testsNotCompared {
			if strings.Contains(apiFullTestName, currTest) {
				return true
			}
		}
	}

	if config.ExcludeAPIList != "" {
		excludeAPIs := strings.Split(config.ExcludeAPIList, ",")
		for _, excludeAPI := range excludeAPIs {
			if strings.Contains(apiFullName, excludeAPI) || strings.Contains(apiFullTestName, excludeAPI) {
				return true
			}
		}
	}

	if config.ExcludeTestList != "" {
		excludeTests := strings.Split(config.ExcludeTestList, ",")
		for _, excludeTest := range excludeTests {
			if excludeTest == strconv.Itoa(globalTestNumber) {
				return true
			}
		}
	}

	return false
}

func verifyInLatestList(testName string, config *Config) bool {
	apiFullTestName := config.Net + "/" + testName
	if config.TestsOnLatestBlock {
		for _, currTest := range testsOnLatest {
			if strings.Contains(apiFullTestName, currTest) {
				return true
			}
		}
	}
	return false
}

func apiUnderTest(currAPI, testName string, config *Config) bool {
	if config.TestingAPIsWith == "" && config.TestingAPIs == "" && !config.TestsOnLatestBlock {
		return true
	}

	if config.TestingAPIsWith != "" {
		tests := strings.Split(config.TestingAPIsWith, ",")
		for _, test := range tests {
			if strings.Contains(currAPI, test) {
				if config.TestsOnLatestBlock && verifyInLatestList(testName, config) {
					return true
				}
				if config.TestsOnLatestBlock {
					return false
				}
				return true
			}
		}
		return false
	}

	if config.TestingAPIs != "" {
		tests := strings.Split(config.TestingAPIs, ",")
		for _, test := range tests {
			if test == currAPI {
				if config.TestsOnLatestBlock && verifyInLatestList(testName, config) {
					return true
				}
				if config.TestsOnLatestBlock {
					return false
				}
				return true
			}
		}
		return false
	}

	if config.TestsOnLatestBlock {
		return verifyInLatestList(testName, config)
	}

	return false
}

func isNotComparedMessage(testName, net string) bool {
	testFullName := net + "/" + testName
	for _, currTestName := range testsNotComparedMessage {
		if currTestName == testFullName {
			return true
		}
	}
	return false
}

func isNotComparedError(testName, net string) bool {
	testFullName := net + "/" + testName
	for _, currTestName := range testsNotComparedError {
		if currTestName == testFullName {
			return true
		}
	}
	return false
}

func dumpJSONs(dumpJSON bool, daemonFile, expRspFile, outputDir string, response, expectedResponse interface{}) error {
	if !dumpJSON {
		return nil
	}

	for attempt := 0; attempt < 10; attempt++ {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			fmt.Printf("Exception on makedirs: %s %v\n", outputDir, err)
			continue
		}

		if daemonFile != "" {
			if _, err := os.Stat(daemonFile); err == nil {
				err := os.Remove(daemonFile)
				if err != nil {
					return err
				}
			}
			data, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				fmt.Printf("Error marshaling daemon response: %v\n", err)
				continue
			}
			if err := os.WriteFile(daemonFile, data, 0644); err != nil {
				fmt.Printf("Exception on file write daemon: %v attempt %d\n", err, attempt)
				continue
			}
		}

		if expRspFile != "" {
			if _, err := os.Stat(expRspFile); err == nil {
				err := os.Remove(expRspFile)
				if err != nil {
					return err
				}
			}
			data, err := json.MarshalIndent(expectedResponse, "", "  ")
			if err != nil {
				fmt.Printf("Error marshaling expected response: %v\n", err)
				continue
			}
			if err := os.WriteFile(expRspFile, data, 0644); err != nil {
				fmt.Printf("Exception on file write expected: %v attempt %d\n", err, attempt)
				continue
			}
		}
		break
	}
	return nil
}

const (
	identifierTag = "id"
	jsonRpcTag    = "jsonrpc"
	resultTag     = "result"
	errorTag      = "error"
)

var (
	errJsonRpcUnexpectedFormat           = errors.New("invalid JSON-RPC response format: neither object nor array")
	errJsonRpcMissingVersion             = errors.New("invalid JSON-RPC response: missing 'jsonrpc' field")
	errJsonRpcMissingId                  = errors.New("invalid JSON-RPC response: missing 'id' field")
	errJsonRpcNoncompliantVersion        = errors.New("noncompliant JSON-RPC 2.0 version")
	errJsonRpcMissingResultOrError       = errors.New("JSON-RPC 2.0 response contains neither 'result' nor 'error'")
	errJsonRpcContainsBothResultAndError = errors.New("JSON-RPC 2.0 response contains both 'result' and 'error'")
)

// validateJsonRpcObject checks that the received response is a valid JSON-RPC object, according to 2.0 spec.
// This implies that the response must be a JSON object containing:
// - one mandatory "jsonrpc" field which must be equal to "2.0"
// - one mandatory "id" field which must match the value of the same field in the request
// - either one "result" field in case of success or one "error" field otherwise, mutually exclusive
// The strict parameter relaxes the compliance requirements by allowing both 'result' and 'error' to be present
// TODO: strict parameter is required for corner cases in streaming mode when 'result' is emitted up-front
// https://www.jsonrpc.org/specification
func validateJsonRpcObject(response map[string]any, strict bool) error {
	// Ensure that the response is a valid JSON-RPC object.
	jsonrpc, ok := response[jsonRpcTag]
	if !ok {
		return errJsonRpcMissingVersion
	}
	jsonrpcVersion, ok := jsonrpc.(string)
	if jsonrpcVersion != "2.0" {
		return errJsonRpcNoncompliantVersion
	}
	_, ok = response[identifierTag]
	if !ok {
		return errJsonRpcMissingId
	}
	_, hasResult := response[resultTag]
	_, hasError := response[errorTag]
	if !hasResult && !hasError {
		return errJsonRpcMissingResultOrError
	}
	if strict && hasResult && hasError {
		return errJsonRpcContainsBothResultAndError
	}
	return nil
}

// validateJsonRpcResponse checks that the received response is a valid JSON-RPC message, according to 2.0 spec.
// This implies that the response must be either a valid JSON-RPC object, i.e. a JSON object containing at least
// "jsonrpc" and "id" fields or a JSON array where each element (if any) is in turn a valid JSON-RPC object.
func validateJsonRpcResponse(response any) error {
	_, isArray := response.([]any)
	responseAsMap, isMap := response.(map[string]any)
	if !isArray && !isMap {
		return errJsonRpcUnexpectedFormat
	}
	if isMap {
		// Ensure that the response is a valid JSON-RPC object.
		err := validateJsonRpcObject(responseAsMap, false)
		if err != nil {
			return err
		}
	}
	if isArray {
		for _, element := range response.([]any) {
			elementAsMap, isElementMap := element.(map[string]any)
			if !isElementMap {
				return errJsonRpcUnexpectedFormat
			}
			err := validateJsonRpcObject(elementAsMap, false)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func executeRequest(ctx context.Context, config *Config, transportType, jwtAuth, requestDumps, target string) (any, error) {
	if transportType == "http" || transportType == "http_comp" || transportType == "https" {
		headers := map[string]string{
			"Content-Type": "application/json",
		}

		if transportType != "http_comp" {
			headers["Accept-Encoding"] = "Identity"
		}

		if jwtAuth != "" {
			headers["Authorization"] = jwtAuth
		}

		targetURL := target
		if transportType == "https" {
			targetURL = "https://" + target
		} else {
			targetURL = "http://" + target
		}

		client := &http.Client{
			Timeout: 300 * time.Second,
		}

		req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBufferString(requestDumps))
		if err != nil {
			if config.VerboseLevel > 0 {
				fmt.Printf("\nhttp request creation fail: %s %v\n", targetURL, err)
			}
			return nil, err
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(start)
		if config.VerboseLevel > 1 {
			fmt.Printf("http round-trip time: %v\n", elapsed)
		}
		if err != nil {
			if config.VerboseLevel > 0 {
				fmt.Printf("\nhttp connection fail: %s %v\n", targetURL, err)
			}
			return nil, err
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				fmt.Printf("\nfailed to close response body: %v\n", err)
			}
		}(resp.Body)

		if resp.StatusCode != http.StatusOK {
			if config.VerboseLevel > 1 {
				fmt.Printf("\npost result status_code: %d\n", resp.StatusCode)
			}
			return nil, fmt.Errorf("http status %v", resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			if config.VerboseLevel > 0 {
				fmt.Printf("\nfailed to read response body: %v\n", err)
			}
			return nil, err
		}

		if config.VerboseLevel > 1 {
			fmt.Printf("\nhttp response body: %s\n", string(body))
		}

		var result any
		if err = json.Unmarshal(body, &result); err != nil {
			if config.VerboseLevel > 0 {
				fmt.Printf("\nfailed to parse JSON: %v\n", err)
			}
			return nil, err
		}
		err = validateJsonRpcResponse(result)
		if err != nil {
			return nil, err
		}

		if config.VerboseLevel > 1 {
			fmt.Printf("Node: %s\nRequest: %s\nResponse: %v\n", target, requestDumps, result)
		}

		return result, nil
	} else {
		// WebSocket
		wsTarget := "ws://" + target
		dialer := websocket.Dialer{
			HandshakeTimeout: 300 * time.Second,
		}

		headers := http.Header{}
		if jwtAuth != "" {
			headers.Set("Authorization", jwtAuth)
		}

		conn, _, err := dialer.Dial(wsTarget, headers)
		if err != nil {
			if config.VerboseLevel > 0 {
				fmt.Printf("\nwebsocket connection fail: %v\n", err)
			}
			return nil, err
		}
		defer func(conn *websocket.Conn) {
			err := conn.Close()
			if err != nil {
				fmt.Printf("\nfailed to close websocket connection: %v\n", err)
			}
		}(conn)

		if err = conn.WriteMessage(websocket.TextMessage, []byte(requestDumps)); err != nil {
			if config.VerboseLevel > 0 {
				fmt.Printf("\nwebsocket write fail: %v\n", err)
			}
			return nil, err
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if config.VerboseLevel > 0 {
				fmt.Printf("\nwebsocket read fail: %v\n", err)
			}
			return nil, err
		}

		var result any
		if err = json.Unmarshal(message, &result); err != nil {
			if config.VerboseLevel > 0 {
				fmt.Printf("\nfailed to parse JSON: %v\n", err)
			}
			return nil, err
		}
		err = validateJsonRpcResponse(result)
		if err != nil {
			return nil, err
		}

		if config.VerboseLevel > 1 {
			fmt.Printf("Node: %s\nRequest: %s\nResponse: %v\n", target, requestDumps, result)
		}

		return result, nil
	}
}

func runCompare(jsonDiff bool, errorFile, tempFile1, tempFile2, diffFile string) bool {
	var cmd *exec.Cmd
	alreadyFailed := false

	if jsonDiff {
		// Check if json-diff is available
		checkCmd := exec.Command("json-diff", "--help")
		if err := checkCmd.Run(); err != nil {
			jsonDiff = false
		}
	}

	if jsonDiff {
		cmd = exec.Command("sh", "-c", fmt.Sprintf("json-diff -s %s %s > %s 2> %s", tempFile2, tempFile1, diffFile, errorFile))
		alreadyFailed = false
	} else {
		cmd = exec.Command("sh", "-c", fmt.Sprintf("diff %s %s > %s 2> %s", tempFile2, tempFile1, diffFile, errorFile))
		alreadyFailed = true
	}

	if err := cmd.Start(); err != nil {
		return false
	}

	done := make(chan error)
	go func() {
		done <- cmd.Wait()
	}()

	timeout := time.After(time.Duration(MaxTime) * TimeInterval)
	ticker := time.NewTicker(TimeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if the process is still running
			continue
		case err := <-done:
			// Process completed
			if err != nil {
				// Non-zero exit, which is expected for diff when files differ
			}

			// Check error file size
			fileInfo, err := os.Stat(errorFile)
			if err == nil && fileInfo.Size() != 0 {
				if alreadyFailed {
					return false
				}
				// Try with diff instead
				alreadyFailed = true
				cmd = exec.Command("sh", "-c", fmt.Sprintf("diff %s %s > %s 2> %s", tempFile2, tempFile1, diffFile, errorFile))
				if err := cmd.Start(); err != nil {
					return false
				}
				go func() {
					done <- cmd.Wait()
				}()
				continue
			}
			return true
		case <-timeout:
			// Timeout reached, kill the process
			if cmd.Process != nil {
				err := cmd.Process.Kill()
				if err != nil {
					return false
				}
			}
			if alreadyFailed {
				return false
			}
			// Try with diff instead
			alreadyFailed = true
			cmd = exec.Command("sh", "-c", fmt.Sprintf("diff %s %s > %s 2> %s", tempFile2, tempFile1, diffFile, errorFile))
			if err := cmd.Start(); err != nil {
				return false
			}
			go func() {
				done <- cmd.Wait()
			}()
			timeout = time.After(time.Duration(MaxTime) * TimeInterval)
		}
	}
}

func copyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer func(source *os.File) {
		err := source.Close()
		if err != nil {
			fmt.Printf("failed to close source file: %v\n", err)
		}
	}(source)

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer func(destination *os.File) {
		err := destination.Close()
		if err != nil {
			fmt.Printf("failed to close destination file: %v\n", err)
		}
	}(destination)

	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}

var (
	errDiffTimeout  = errors.New("diff timeout")
	errDiffMismatch = errors.New("diff mismatch")
)

func isArchive(jsonFilename string) bool {
	// Treat all files except .json as potential archive files
	return !strings.HasSuffix(jsonFilename, ".json")
}

func extractJsonCommands(jsonFilename string) ([]JSONRPCCommand, error) {
	var jsonrpcCommands []JSONRPCCommand
	data, err := os.ReadFile(jsonFilename)
	if err != nil {
		return jsonrpcCommands, errors.New("cannot read file " + jsonFilename)
	}
	if err := json.Unmarshal(data, &jsonrpcCommands); err != nil {
		return jsonrpcCommands, errors.New("cannot parse JSON " + jsonFilename)
	}
	return jsonrpcCommands, nil
}

func (c *JSONRPCCommand) compareJSONFiles(kind JsonDiffKind, errorFileName, fileName1, fileName2, diffFileName string) (bool, error) {
	switch kind {
	case JdLibrary:
		jsonNode1, err := jd.ReadJsonFile(fileName1)
		if err != nil {
			return false, err
		}
		jsonNode2, err := jd.ReadJsonFile(fileName2)
		if err != nil {
			return false, err
		}
		var diff jd.Diff
		// Check if the test contains any response metadata with custom options for JSON diff
		if c.TestInfo != nil && c.TestInfo.Metadata != nil && c.TestInfo.Metadata.Response != nil {
			if c.TestInfo.Metadata.Response.PathOptions != nil {
				pathOptions := c.TestInfo.Metadata.Response.PathOptions
				options, err := jd.ReadOptionsString(string(pathOptions))
				if err != nil {
					return false, err
				}
				diff = jsonNode1.Diff(jsonNode2, options...)
			}
		} else {
			diff = jsonNode1.Diff(jsonNode2)
		}
		diffString := diff.Render()
		err = os.WriteFile(diffFileName, []byte(diffString), 0644)
		if err != nil {
			return false, err
		}
		return true, nil
	case JsonDiffTool:
		if success := runCompare(true, errorFileName, fileName1, fileName2, diffFileName); !success {
			return false, fmt.Errorf("failed to compare %s and %s using json-diff command", fileName1, fileName2)
		}
		return true, nil
	case DiffTool:
		if success := runCompare(false, errorFileName, fileName1, fileName2, diffFileName); !success {
			return false, fmt.Errorf("failed to compare %s and %s using diff command", fileName1, fileName2)
		}
		return true, nil
	default:
		return false, fmt.Errorf("unknown JSON diff kind: %d", kind)
	}
}

func (c *JSONRPCCommand) compareJSON(config *Config, response interface{}, jsonFile, daemonFile, expRspFile, diffFile string, testNumber int) (bool, error) {
	baseName := filepath.Join(TempDirname, fmt.Sprintf("test_%d", testNumber))
	err := os.MkdirAll(baseName, 0755)
	if err != nil {
		return false, err
	}

	tempFile1 := filepath.Join(baseName, "daemon_lower_case.txt")
	tempFile2 := filepath.Join(baseName, "rpc_lower_case.txt")
	errorFile := filepath.Join(baseName, "ERROR.txt")

	// Check if response contains error
	responseMap, isMap := response.(map[string]interface{})
	hasError := isMap && responseMap["error"] != nil

	if hasError {
		err := toLowerCase(daemonFile, tempFile1)
		if err != nil {
			return false, err
		}
		err = toLowerCase(expRspFile, tempFile2)
		if err != nil {
			return false, err
		}
	} else {
		_, err := copyFile(daemonFile, tempFile1)
		if err != nil {
			return false, err
		}
		_, err = copyFile(expRspFile, tempFile2)
		if err != nil {
			return false, err
		}
	}

	if isNotComparedMessage(jsonFile, config.Net) {
		err := replaceMessage(expRspFile, tempFile1, "message")
		if err != nil {
			return false, err
		}
		err = replaceMessage(daemonFile, tempFile2, "message")
		if err != nil {
			return false, err
		}
	} else if isNotComparedError(jsonFile, config.Net) {
		err := replaceMessage(expRspFile, tempFile1, "error")
		if err != nil {
			return false, err
		}
		err = replaceMessage(daemonFile, tempFile2, "error")
		if err != nil {
			return false, err
		}
	}

	diffResult, err := c.compareJSONFiles(config.DiffKind, errorFile, tempFile1, tempFile2, diffFile)
	diffFileSize := int64(0)

	if diffResult {
		fileInfo, err := os.Stat(diffFile)
		if err != nil {
			return false, err
		}
		diffFileSize = fileInfo.Size()
	}

	// Cleanup temp files
	err = os.Remove(tempFile1)
	if err != nil {
		return false, err
	}
	err = os.Remove(tempFile2)
	if err != nil {
		return false, err
	}
	err = os.RemoveAll(baseName)
	if err != nil {
		return false, err
	}

	if diffFileSize != 0 || !diffResult {
		if !diffResult {
			err = errDiffTimeout
		} else {
			err = errDiffMismatch
		}
		return false, err
	}

	return true, nil
}

func (c *JSONRPCCommand) processResponse(response, result1 any, responseInFile interface{}, config *Config, outputDir, daemonFile, expRspFile, diffFile string, descriptor *TestDescriptor) (bool, error) {
	jsonFile := descriptor.Name
	testNumber := descriptor.Number

	var expectedResponse interface{}
	if result1 != nil {
		expectedResponse = result1
	} else {
		expectedResponse = responseInFile
	}

	if config.WithoutCompareResults {
		err := dumpJSONs(config.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	// Deep comparison between the received response and the expected response
	respJSON, _ := json.Marshal(response)
	expJSON, _ := json.Marshal(expectedResponse)

	// Fast path: if actual/expected are identical byte-wise, no need to compare them
	if bytes.Equal(respJSON, expJSON) {
		err := dumpJSONs(config.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	// Check various conditions where we don't care about differences
	responseMap, respIsMap := response.(map[string]interface{})
	expectedMap, expIsMap := expectedResponse.(map[string]interface{})

	if respIsMap && expIsMap {
		_, responseHasResult := responseMap["result"]
		expectedResult, expectedHasResult := expectedMap["result"]
		_, responseHasError := responseMap["error"]
		expectedError, expectedHasError := expectedMap["error"]
		if responseHasResult && expectedHasResult && expectedResult == nil && result1 == nil {
			err := dumpJSONs(config.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse)
			if err != nil {
				return false, err
			}
			return true, nil
		}
		if responseHasError && expectedHasError && expectedError == nil {
			err := dumpJSONs(config.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse)
			if err != nil {
				return false, err
			}
			return true, nil
		}
		// TODO: improve len(expectedMap) == 2 which means: just "jsonrpc" and "id" are expected
		if !expectedHasResult && !expectedHasError && len(expectedMap) == 2 {
			err := dumpJSONs(config.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse)
			if err != nil {
				return false, err
			}
			return true, nil
		}
		if responseHasError && expectedHasError && config.DoNotCompareError {
			err := dumpJSONs(config.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse)
			if err != nil {
				return false, err
			}
			return true, nil
		}
	}

	err := dumpJSONs(true, daemonFile, expRspFile, outputDir, response, expectedResponse)
	if err != nil {
		return false, err
	}

	same, err := c.compareJSON(config, response, jsonFile, daemonFile, expRspFile, diffFile, testNumber)
	if err != nil {
		return same, err
	}
	if same {
		err := os.Remove(daemonFile)
		if err != nil {
			return false, err
		}
		err = os.Remove(expRspFile)
		if err != nil {
			return false, err
		}
		err = os.Remove(diffFile)
		if err != nil {
			return false, err
		}
	}

	err = dumpJSONs(config.ForceDumpJSONs, daemonFile, expRspFile, outputDir, response, expectedResponse)
	if err != nil {
		return false, err
	}
	return same, nil
}

func (c *JSONRPCCommand) run(ctx context.Context, config *Config, descriptor *TestDescriptor) (bool, error) {
	transportType := descriptor.TransportType
	jsonFile := descriptor.Name
	request := c.Request

	method := ""
	requestBytes, _ := json.Marshal(request)
	var requestMap map[string]interface{}
	if err := json.Unmarshal(requestBytes, &requestMap); err == nil {
		if m, ok := requestMap["method"].(string); ok {
			method = m
		}
	} else {
		// Try an array of requests
		var requestArray []map[string]interface{}
		if err := json.Unmarshal(requestBytes, &requestArray); err == nil && len(requestArray) > 0 {
			if m, ok := requestArray[0]["method"].(string); ok {
				method = m
			}
		}
	}

	requestDumps, _ := json.Marshal(request)
	target := getTarget(config.DaemonUnderTest, method, config)
	target1 := ""

	var jwtAuth string
	if config.JWTSecret != "" {
		secretBytes, _ := hex.DecodeString(config.JWTSecret)
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"iat": time.Now().Unix(),
		})
		tokenString, _ := token.SignedString(secretBytes)
		jwtAuth = "Bearer " + tokenString
	}

	outputAPIFilename := filepath.Join(config.OutputDir, strings.TrimSuffix(jsonFile, filepath.Ext(jsonFile)))
	outputDirName := filepath.Dir(outputAPIFilename)
	diffFile := outputAPIFilename + "-diff.json"

	if !config.VerifyWithDaemon {
		result, err := executeRequest(ctx, config, transportType, jwtAuth, string(requestDumps), target)
		if err != nil {
			return false, err
		}
		if config.VerboseLevel > 2 {
			fmt.Printf("%s: [%v]\n", config.DaemonUnderTest, result)
		}
		if result == nil {
			return false, errors.New("response is n il (maybe node at " + target + " is down?)")
		}

		responseInFile := c.Response
		daemonFile := outputAPIFilename + "-response.json"
		expRspFile := outputAPIFilename + "-expResponse.json"

		return c.processResponse(result, nil, responseInFile, config,
			outputDirName, daemonFile, expRspFile, diffFile, descriptor)
	} else {
		target = getTarget(DaemonOnDefaultPort, method, config)
		result, err := executeRequest(ctx, config, transportType, jwtAuth, string(requestDumps), target)
		if err != nil {
			return false, err
		}
		if config.VerboseLevel > 2 {
			fmt.Printf("%s: [%v]\n", config.DaemonUnderTest, result)
		}
		if result == nil {
			return false, errors.New("response is nil (maybe node at " + target + " is down?)")
		}
		target1 = getTarget(config.DaemonAsReference, method, config)
		result1, err := executeRequest(ctx, config, transportType, jwtAuth, string(requestDumps), target1)
		if err != nil {
			return false, err
		}
		if config.VerboseLevel > 2 {
			fmt.Printf("%s: [%v]\n", config.DaemonAsReference, result1)
		}
		if result1 == nil {
			return false, errors.New("response is nil (maybe node at " + target1 + " is down?)")
		}

		daemonFile := outputAPIFilename + getJSONFilenameExt(DaemonOnDefaultPort, target)
		expRspFile := outputAPIFilename + getJSONFilenameExt(config.DaemonAsReference, target1)

		return c.processResponse(result, result1, nil, config,
			outputDirName, daemonFile, expRspFile, diffFile, descriptor)
	}
}

func runTest(ctx context.Context, descriptor *TestDescriptor, config *Config) (bool, error) {
	jsonFilename := filepath.Join(config.JSONDir, descriptor.Name)

	var jsonrpcCommands []JSONRPCCommand
	var err error
	if isArchive(jsonFilename) {
		tempFilePaths, err := extractArchive(jsonFilename, config.SanitizeArchiveExt)
		if err != nil {
			return false, errors.New("cannot extract archive file " + jsonFilename)
		}
		removeTempFiles := func() {
			for _, path := range tempFilePaths {
				err := os.Remove(path)
				if err != nil {
					fmt.Printf("failed to remove temp file %s: %v\n", path, err)
				}
			}
		}
		for _, tempFilePath := range tempFilePaths {
			jsonrpcCommands, err = extractJsonCommands(tempFilePath)
			if err != nil {
				removeTempFiles()
				return false, err
			}
		}
		removeTempFiles()
	} else {
		jsonrpcCommands, err = extractJsonCommands(jsonFilename)
		if err != nil {
			return false, err
		}
	}

	for _, jsonrpcCmd := range jsonrpcCommands {
		return jsonrpcCmd.run(ctx, config, descriptor) // TODO: support multiple tests
	}

	fmt.Printf("WARN: no commands found in test %s\n", jsonFilename)

	return true, nil
}

func mustAtoi(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

func runMain() int {
	// Create a channel to receive OS signals and register for clean termination signals.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Parse command line arguments
	config := NewConfig()
	if err := config.parseFlags(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		usage()
		return -1
	}

	// Handle embedded CPU/memory profiling and execution tracing
	if config.CpuProfile != "" {
		f, err := os.Create(config.CpuProfile)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "could not create CPU profile: %v\n", err)
		}
		defer func(f *os.File) {
			err := f.Close()
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "could not close CPU profile: %v\n", err)
			}
		}(f)
		if err := pprof.StartCPUProfile(f); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "could not start CPU profile: %v\n", err)
		}
		defer pprof.StopCPUProfile()
	}

	if config.TraceFile != "" {
		f, err := os.Create(config.TraceFile)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "could not create trace file: %v\n", err)
		}
		defer func(f *os.File) {
			err := f.Close()
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "could not close trace file: %v\n", err)
			}
		}(f)
		if err := trace.Start(f); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "could not start trace: %v\n", err)
		}
		defer trace.Stop()
	}

	defer func() {
		if config.MemProfile != "" {
			f, err := os.Create(config.MemProfile)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "could not create memory profile: %v\n", err)
			}
			defer func(f *os.File) {
				err := f.Close()
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "could not close memory profile: %v\n", err)
				}
			}(f)
			runtime.GC() // get up-to-date statistics
			if err := pprof.WriteHeapProfile(f); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "could not write memory profile: %v\n", err)
			}
		}
	}()

	// Clean temp dirs if exists // TODO: use OS temp dir?
	if _, err := os.Stat(TempDirname); err == nil {
		err := os.RemoveAll(TempDirname)
		if err != nil {
			return -1
		}
	}

	startTime := time.Now()
	err := os.MkdirAll(config.OutputDir, 0755)
	if err != nil {
		return -1
	}

	scheduledTests := 0
	executedTests := 0
	failedTests := 0
	successTests := 0
	skippedTests := 0

	var serverEndpoints string
	if config.VerifyWithDaemon {
		if config.DaemonAsReference == ExternalProvider {
			serverEndpoints = "both servers (rpcdaemon with " + config.ExternalProviderURL + ")"
		} else {
			serverEndpoints = "both servers (rpcdaemon with " + config.DaemonUnderTest + ")"
		}
	} else {
		target := getTarget(config.DaemonUnderTest, "eth_call", config)
		target1 := getTarget(config.DaemonUnderTest, "engine_", config)
		serverEndpoints = target + "/" + target1
	}

	if config.Parallel {
		fmt.Printf("Run tests in parallel on %s\n", serverEndpoints)
	} else {
		fmt.Printf("Run tests in serial on %s\n", serverEndpoints)
	}

	if strings.Contains(config.TransportType, "_comp") {
		fmt.Println("Run tests using compression")
	}

	resultsAbsoluteDir, err := filepath.Abs(config.ResultsDir)
	if err != nil {
		return -1
	}
	fmt.Printf("Result directory: %s\n", resultsAbsoluteDir)

	globalTestNumber := 0
	availableTestedAPIs := 0
	testRep := 0

	// Worker pool for parallel execution
	var wg sync.WaitGroup
	testsChan := make(chan *TestDescriptor, 10000)
	resultsChan := make(chan chan TestResult, 10000)

	numWorkers := 1
	if config.Parallel {
		numWorkers = 10 // Adjust based on your needs
	}

	ctx, cancelCtx := context.WithCancel(context.Background())

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case test := <-testsChan:
					if test == nil {
						return
					}
					success, err := runTest(ctx, test, config)
					test.ResultChan <- TestResult{Success: success, Error: err, Test: test}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Results collector
	var resultsWg sync.WaitGroup
	resultsWg.Add(1)
	go func() {
		defer resultsWg.Done()
		for {
			select {
			case testResultCh := <-resultsChan:
				if testResultCh == nil {
					return
				}
				select {
				case result := <-testResultCh:
					file := fmt.Sprintf("%-60s", result.Test.Name)
					tt := fmt.Sprintf("%-15s", result.Test.TransportType)
					fmt.Printf("%04d. %s::%s   ", result.Test.Number, tt, file)

					if result.Success {
						successTests++
						if config.VerboseLevel > 0 {
							fmt.Println("OK")
						} else {
							fmt.Print("OK\r")
						}
					} else {
						failedTests++
						fmt.Printf("failed: %s\n", result.Error.Error())
						if config.ExitOnFail {
							// Signal other tasks to stop and exit
							cancelCtx()
							return
						}
					}
					executedTests++
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

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

	for testRep = 0; testRep < config.LoopNumber; testRep++ {
		select {
		case <-ctx.Done():
			break
		default:
		}

		if config.LoopNumber != 1 {
			fmt.Printf("\nTest iteration: %d\n", testRep+1)
		}

		transportTypes := strings.Split(config.TransportType, ",")
		for _, transportType := range transportTypes {
			select {
			case <-ctx.Done():
				break
			default:
			}

			testNumberInAnyLoop := 1

			dirs, err := os.ReadDir(config.JSONDir)
			if err != nil {
				_, err := fmt.Fprintf(os.Stderr, "Error reading directory %s: %v\n", config.JSONDir, err)
				if err != nil {
					return -1
				}
				continue
			}

			// Sort directories
			sort.Slice(dirs, func(i, j int) bool {
				return dirs[i].Name() < dirs[j].Name()
			})

			globalTestNumber = 0
			availableTestedAPIs = 0

			for _, currAPIEntry := range dirs {
				select {
				case <-ctx.Done():
					break
				default:
				}

				currAPI := currAPIEntry.Name()

				// Skip results folder and hidden folders
				if currAPI == config.ResultsDir || strings.HasPrefix(currAPI, ".") {
					continue
				}

				testDir := filepath.Join(config.JSONDir, currAPI)
				info, err := os.Stat(testDir)
				if err != nil || !info.IsDir() {
					continue
				}

				availableTestedAPIs++

				testEntries, err := os.ReadDir(testDir)
				if err != nil {
					continue
				}

				// Sort test files by number
				sort.Slice(testEntries, func(i, j int) bool {
					return extractNumber(testEntries[i].Name()) < extractNumber(testEntries[j].Name())
				})

				testNumber := 1
				for _, testEntry := range testEntries {
					select {
					case <-ctx.Done():
						break
					default:
					}

					testName := testEntry.Name()

					if !strings.HasPrefix(testName, "test_") {
						continue
					}

					ext := filepath.Ext(testName)
					if ext != ".zip" && ext != ".gzip" && ext != ".json" && ext != ".tar" {
						continue
					}

					jsonTestFullName := filepath.Join(currAPI, testName)

					if apiUnderTest(currAPI, jsonTestFullName, config) {
						if isSkipped(currAPI, jsonTestFullName, testNumberInAnyLoop, config) {
							if config.StartTest == "" || testNumberInAnyLoop >= mustAtoi(config.StartTest) {
								if !config.DisplayOnlyFail && config.ReqTestNumber == -1 {
									file := fmt.Sprintf("%-60s", jsonTestFullName)
									tt := fmt.Sprintf("%-15s", transportType)
									fmt.Printf("%04d. %s::%s   skipped\n", testNumberInAnyLoop, tt, file)
								}
								skippedTests++
							}
						} else {
							shouldRun := false
							if config.TestingAPIsWith == "" && config.TestingAPIs == "" && (config.ReqTestNumber == -1 || config.ReqTestNumber == testNumberInAnyLoop) {
								shouldRun = true
							} else if config.TestingAPIsWith != "" && checkTestNameForNumber(testName, config.ReqTestNumber) {
								shouldRun = true
							} else if config.TestingAPIs != "" && checkTestNameForNumber(testName, config.ReqTestNumber) {
								shouldRun = true
							}

							if shouldRun && (config.StartTest == "" || testNumberInAnyLoop >= mustAtoi(config.StartTest)) {
								testDesc := &TestDescriptor{
									Name:          jsonTestFullName,
									Number:        testNumberInAnyLoop,
									TransportType: transportType,
									ResultChan:    make(chan TestResult, 1),
								}
								select {
								case <-ctx.Done():
									return -1
								case resultsChan <- testDesc.ResultChan:
								}
								select {
								case <-ctx.Done():
									return -1
								case testsChan <- testDesc:
								}
								scheduledTests++

								if config.WaitingTime > 0 {
									time.Sleep(time.Duration(config.WaitingTime) * time.Millisecond)
								}
							}
						}
					}

					globalTestNumber++
					testNumberInAnyLoop++
					testNumber++
				}
			}
		}

		if config.ExitOnFail && failedTests > 0 {
			fmt.Println("WARN: test sequence interrupted by failure (ExitOnFail)")
			break
		}
	}

	if scheduledTests == 0 && config.TestingAPIsWith != "" {
		fmt.Printf("WARN: API filter %s selected no tests\n", config.TestingAPIsWith)
	}

	// Close channels and wait for completion
	close(testsChan)
	wg.Wait()
	close(resultsChan)
	resultsWg.Wait()

	// Clean empty subfolders in the output dir
	if entries, err := os.ReadDir(config.OutputDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			outputSubfolder := filepath.Join(config.OutputDir, entry.Name())
			if subEntries, err := os.ReadDir(outputSubfolder); err == nil && len(subEntries) == 0 {
				err := os.Remove(outputSubfolder)
				if err != nil {
					fmt.Printf("WARN: clean failed %v\n", err)
				}
			}
		}
	}

	// Clean temp dir
	err = os.RemoveAll(TempDirname)
	if err != nil {
		return -1
	}

	// Print results
	elapsed := time.Since(startTime)
	fmt.Println("\n                                                                                                                  ")
	fmt.Printf("Test session duration:        %v\n", elapsed)
	fmt.Printf("Test session iterations:      %d\n", testRep)
	fmt.Printf("Test suite total APIs:        %d\n", availableTestedAPIs)
	fmt.Printf("Test suite total tests:       %d\n", globalTestNumber)
	fmt.Printf("Number of skipped tests:      %d\n", skippedTests)
	fmt.Printf("Number of selected tests:     %d\n", scheduledTests)
	fmt.Printf("Number of executed tests:     %d\n", executedTests)
	fmt.Printf("Number of success tests:      %d\n", successTests)
	fmt.Printf("Number of failed tests:       %d\n", failedTests)

	if failedTests > 0 {
		return 1
	}
	return 0
}

func main() {
	exitCode := runMain()
	os.Exit(exitCode)
}
