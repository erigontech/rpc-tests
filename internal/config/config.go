package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	jsoniter "github.com/json-iterator/go"
)

const (
	DaemonOnDefaultPort = "rpcdaemon"
	DaemonOnOtherPort   = "other-daemon"
	ExternalProvider    = "external-provider"
	None                = "none"

	TransportHTTP          = "http"
	TransportHTTPComp      = "http_comp"
	TransportHTTPS         = "https"
	TransportWebSocket     = "websocket"
	TransportWebSocketComp = "websocket_comp"

	DefaultServerPort      = 8545
	DefaultEnginePort      = 8551
	DefaultOtherPort       = 51515
	DefaultOtherEnginePort = 51516

	TempDirName = "./temp_rpc_tests"
	ResultsDir  = "results"
)

// JSON is the json-iterator API used across the application for fast JSON operations.
var JSON = jsoniter.ConfigCompatibleWithStandardLibrary

// DiffKind represents the JSON diff strategy to use.
type DiffKind int

const (
	JdLibrary DiffKind = iota
	JsonDiffTool
	DiffTool
	JsonDiffGo
)

func (k DiffKind) String() string {
	return [...]string{"jd", "json-diff", "diff", "json-diff-go"}[k]
}

// ParseDiffKind converts a string into a DiffKind enum type.
func ParseDiffKind(s string) (DiffKind, error) {
	switch strings.ToLower(s) {
	case "jd":
		return JdLibrary, nil
	case "json-diff":
		return JsonDiffTool, nil
	case "diff":
		return DiffTool, nil
	case "json-diff-go":
		return JsonDiffGo, nil
	default:
		return JdLibrary, fmt.Errorf("invalid DiffKind value: %s", s)
	}
}

// Config holds all configuration for the test runner.
type Config struct {
	// Test execution
	ExitOnFail  bool
	Parallel    bool
	LoopNumber  int
	StartTest   string
	ReqTestNum  int
	WaitingTime int

	// Output control
	VerboseLevel          int
	DisplayOnlyFail       bool
	ForceDumpJSONs        bool
	DiffKind              DiffKind
	DoNotCompareError     bool
	WithoutCompareResults bool

	// Network and paths
	Net        string
	JSONDir    string
	ResultsDir string
	OutputDir  string

	// Daemon configuration
	DaemonUnderTest     string
	DaemonAsReference   string
	DaemonOnHost        string
	ServerPort          int
	EnginePort          int
	VerifyWithDaemon    bool
	ExternalProviderURL string
	LocalServer         string

	// Test filtering
	TestingAPIs        string // Exact match (-A)
	TestingAPIsWith    string // Pattern match (-a)
	ExcludeAPIList     string
	ExcludeTestList    string
	TestsOnLatestBlock bool

	// Authentication
	JWTSecret string

	// Transport
	TransportType string

	// Archive handling
	SanitizeArchiveExt bool

	// Profiling
	CpuProfile string
	MemProfile string
	TraceFile  string

	// Cached derived values (set by UpdateDirs)
	StartTestNum int // parsed StartTest, cached for zero-alloc lookups
}

// NewConfig creates a Config with sensible defaults matching v1 behavior.
func NewConfig() *Config {
	return &Config{
		ExitOnFail:        true,
		Parallel:          true,
		LoopNumber:        1,
		ReqTestNum:        -1,
		VerboseLevel:      0,
		Net:               "mainnet",
		DaemonOnHost:      "localhost",
		ServerPort:        0,
		EnginePort:        0,
		DaemonUnderTest:   DaemonOnDefaultPort,
		DaemonAsReference: None,
		DiffKind:          JsonDiffGo,
		TransportType:     TransportHTTP,
		ResultsDir:        ResultsDir,
	}
}

// Validate checks the configuration for conflicts and invalid values.
func (c *Config) Validate() error {
	if c.WaitingTime > 0 && c.Parallel {
		return fmt.Errorf("waiting-time is not compatible with parallel tests")
	}
	if c.DaemonUnderTest == DaemonOnOtherPort && c.VerifyWithDaemon && c.DaemonAsReference == DaemonOnDefaultPort {
		return fmt.Errorf("daemon-port is not compatible with compare-erigon-rpcdaemon")
	}
	if c.ReqTestNum != -1 && (c.ExcludeTestList != "" || c.ExcludeAPIList != "") {
		return fmt.Errorf("run-test is not compatible with exclude-api-list or exclude-test-list")
	}
	if c.TestingAPIs != "" && c.ExcludeAPIList != "" {
		return fmt.Errorf("api-list is not compatible with exclude-api-list")
	}
	if c.VerifyWithDaemon && c.WithoutCompareResults {
		return fmt.Errorf("compare-erigon-rpcdaemon is not compatible with without-compare-results")
	}

	// Validate transport types
	if c.TransportType != "" {
		types := strings.Split(c.TransportType, ",")
		for _, t := range types {
			if !IsValidTransport(t) {
				return fmt.Errorf("invalid connection type: %s", t)
			}
		}
	}

	return nil
}

// IsValidTransport checks if a transport type string is valid.
func IsValidTransport(t string) bool {
	switch t {
	case TransportHTTP, TransportHTTPComp, TransportHTTPS, TransportWebSocket, TransportWebSocketComp:
		return true
	default:
		return false
	}
}

// UpdateDirs sets derived directory paths and cached values based on current configuration.
func (c *Config) UpdateDirs() {
	c.JSONDir = "./integration/" + c.Net + "/"
	c.OutputDir = c.JSONDir + c.ResultsDir + "/"
	if c.ServerPort == 0 {
		c.ServerPort = DefaultServerPort
	}
	if c.EnginePort == 0 {
		c.EnginePort = DefaultEnginePort
	}
	c.LocalServer = "http://" + c.DaemonOnHost + ":" + strconv.Itoa(c.ServerPort)

	// Cache parsed StartTest for zero-alloc lookups in the scheduling loop
	if c.StartTest != "" {
		c.StartTestNum, _ = strconv.Atoi(c.StartTest)
	}
}

// GetTarget returns the target URL for an RPC method given a daemon target type.
func (c *Config) GetTarget(targetType, method string) string {
	isEngine := strings.HasPrefix(method, "engine_")

	if targetType == ExternalProvider {
		return c.ExternalProviderURL
	}

	if c.VerifyWithDaemon && targetType == DaemonOnOtherPort && isEngine {
		return c.DaemonOnHost + ":" + strconv.Itoa(DefaultOtherEnginePort)
	}
	if c.VerifyWithDaemon && targetType == DaemonOnOtherPort {
		return c.DaemonOnHost + ":" + strconv.Itoa(DefaultOtherPort)
	}
	if targetType == DaemonOnOtherPort && isEngine {
		return c.DaemonOnHost + ":" + strconv.Itoa(DefaultOtherEnginePort)
	}
	if targetType == DaemonOnOtherPort {
		return c.DaemonOnHost + ":" + strconv.Itoa(DefaultOtherPort)
	}

	if isEngine {
		port := c.EnginePort
		if port == 0 {
			port = DefaultEnginePort
		}
		return c.DaemonOnHost + ":" + strconv.Itoa(port)
	}

	port := c.ServerPort
	if port == 0 {
		port = DefaultServerPort
	}
	return c.DaemonOnHost + ":" + strconv.Itoa(port)
}

// GetJSONFilenameExt returns the JSON filename extension based on daemon type and target.
func GetJSONFilenameExt(targetType, target string) string {
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

// ServerEndpoints returns a human-readable description of the server endpoints.
func (c *Config) ServerEndpoints() string {
	if c.VerifyWithDaemon {
		if c.DaemonAsReference == ExternalProvider {
			return "both servers (rpcdaemon with " + c.ExternalProviderURL + ")"
		}
		return "both servers (rpcdaemon with " + c.DaemonUnderTest + ")"
	}
	target := c.GetTarget(c.DaemonUnderTest, "eth_call")
	target1 := c.GetTarget(c.DaemonUnderTest, "engine_")
	return target + "/" + target1
}

// TransportTypes returns the list of transport types as a slice.
func (c *Config) TransportTypes() []string {
	return strings.Split(c.TransportType, ",")
}

// CleanOutputDir removes and recreates the output directory.
func (c *Config) CleanOutputDir() error {
	if _, err := os.Stat(c.OutputDir); err == nil {
		if err := os.RemoveAll(c.OutputDir); err != nil {
			return err
		}
	}
	return os.MkdirAll(c.OutputDir, 0755)
}

// ResultsAbsDir returns the absolute path to the results directory.
func (c *Config) ResultsAbsDir() (string, error) {
	return filepath.Abs(c.ResultsDir)
}

// GetJWTSecret reads a JWT secret from a file.
func GetJWTSecret(filename string) (string, error) {
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

// GenerateJWTSecret creates a new JWT secret file with random hex data.
func GenerateJWTSecret(filename string, length int) error {
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
