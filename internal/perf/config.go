package perf

import (
	"fmt"
	"os"
	"os/user"
	"time"
)

const (
	DefaultTestSequence          = "50:30,1000:30,2500:20,10000:20"
	DefaultRepetitions           = 10
	DefaultVegetaPatternTarFile  = ""
	DefaultClientVegetaOnCore    = "-:-"
	DefaultServerAddress         = "localhost"
	DefaultWaitingTime           = 5
	DefaultMaxConn               = "9000"
	DefaultTestType              = "eth_getLogs"
	DefaultVegetaResponseTimeout = "300s"
	DefaultMaxBodyRsp            = "1500"
	DefaultClientName            = "rpcdaemon"
	DefaultClientBuildDir        = ""

	BinaryDir = "bin"
)

// Config holds all configuration for the performance test.
type Config struct {
	VegetaPatternTarFile   string
	ClientVegetaOnCore     string
	ClientBuildDir         string
	Repetitions            int
	TestSequence           string
	ClientAddress          string
	TestType               string
	TestingClient          string
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
	DisableHttpCompression bool
}

// NewConfig creates a new Config with default values.
func NewConfig() *Config {
	return &Config{
		VegetaPatternTarFile:   DefaultVegetaPatternTarFile,
		ClientVegetaOnCore:     DefaultClientVegetaOnCore,
		ClientBuildDir:         DefaultClientBuildDir,
		Repetitions:            DefaultRepetitions,
		TestSequence:           DefaultTestSequence,
		ClientAddress:          DefaultServerAddress,
		TestType:               DefaultTestType,
		TestingClient:          DefaultClientName,
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
		DisableHttpCompression: false,
	}
}

// Validate checks the configuration for conflicts and invalid values.
func (c *Config) Validate() error {
	if c.JSONReportFile != "" && c.TestingClient == "" {
		return fmt.Errorf("with json-report must also set testing-client")
	}

	if c.ClientBuildDir != "" {
		if _, err := os.Stat(c.ClientBuildDir); os.IsNotExist(err) {
			return fmt.Errorf("client build dir not specified correctly: %s", c.ClientBuildDir)
		}
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

	return nil
}

// RunDirs holds the temporary directory paths used during a perf run.
type RunDirs struct {
	RunTestDir  string
	PatternDir  string
	ReportFile  string
	TarFileName string
	PatternBase string
}

// NewRunDirs creates a new set of run directories based on a timestamp.
func NewRunDirs() *RunDirs {
	timestamp := time.Now().UnixNano()
	runTestDir := fmt.Sprintf("/tmp/run_tests_%d", timestamp)
	return &RunDirs{
		RunTestDir:  runTestDir,
		PatternDir:  runTestDir + "/erigon_stress_test",
		ReportFile:  runTestDir + "/vegeta_report.hrd",
		TarFileName: runTestDir + "/vegeta_TAR_File",
		PatternBase: runTestDir + "/erigon_stress_test/vegeta_erigon_",
	}
}
