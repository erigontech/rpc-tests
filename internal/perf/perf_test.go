package perf

import (
	"testing"
	"time"
)

func TestParseTestSequence_Valid(t *testing.T) {
	seq, err := ParseTestSequence("50:30,1000:30,2500:20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seq) != 3 {
		t.Fatalf("expected 3 items, got %d", len(seq))
	}
	if seq[0].QPS != 50 || seq[0].Duration != 30 {
		t.Errorf("item 0: got QPS=%d Duration=%d, want 50:30", seq[0].QPS, seq[0].Duration)
	}
	if seq[1].QPS != 1000 || seq[1].Duration != 30 {
		t.Errorf("item 1: got QPS=%d Duration=%d, want 1000:30", seq[1].QPS, seq[1].Duration)
	}
	if seq[2].QPS != 2500 || seq[2].Duration != 20 {
		t.Errorf("item 2: got QPS=%d Duration=%d, want 2500:20", seq[2].QPS, seq[2].Duration)
	}
}

func TestParseTestSequence_Default(t *testing.T) {
	seq, err := ParseTestSequence(DefaultTestSequence)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seq) != 4 {
		t.Fatalf("expected 4 items, got %d", len(seq))
	}
}

func TestParseTestSequence_InvalidFormat(t *testing.T) {
	_, err := ParseTestSequence("50:30,invalid")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestParseTestSequence_InvalidQPS(t *testing.T) {
	_, err := ParseTestSequence("abc:30")
	if err == nil {
		t.Error("expected error for invalid QPS")
	}
}

func TestParseTestSequence_InvalidDuration(t *testing.T) {
	_, err := ParseTestSequence("50:abc")
	if err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestFormatDuration_Microseconds(t *testing.T) {
	d := 500 * time.Microsecond
	got := FormatDuration(d)
	if got != "500µs" {
		t.Errorf("FormatDuration(%v): got %q, want %q", d, got, "500µs")
	}
}

func TestFormatDuration_Milliseconds(t *testing.T) {
	d := 150 * time.Millisecond
	got := FormatDuration(d)
	if got != "150.00ms" {
		t.Errorf("FormatDuration(%v): got %q, want %q", d, got, "150.00ms")
	}
}

func TestFormatDuration_Seconds(t *testing.T) {
	d := 2500 * time.Millisecond
	got := FormatDuration(d)
	if got != "2.50s" {
		t.Errorf("FormatDuration(%v): got %q, want %q", d, got, "2.50s")
	}
}

func TestCountDigits(t *testing.T) {
	tests := []struct {
		n    int
		want int
	}{
		{0, 1},
		{1, 1},
		{9, 1},
		{10, 2},
		{99, 2},
		{100, 3},
		{1000, 4},
		{10000, 5},
	}
	for _, tt := range tests {
		got := CountDigits(tt.n)
		if got != tt.want {
			t.Errorf("CountDigits(%d): got %d, want %d", tt.n, got, tt.want)
		}
	}
}

func TestMaxQpsAndDurationDigits(t *testing.T) {
	seq := TestSequence{
		{QPS: 50, Duration: 30},
		{QPS: 10000, Duration: 20},
		{QPS: 100, Duration: 5},
	}
	maxQps, maxDur := MaxQpsAndDurationDigits(seq)
	if maxQps != 5 {
		t.Errorf("maxQpsDigits: got %d, want 5", maxQps)
	}
	if maxDur != 2 {
		t.Errorf("maxDurationDigits: got %d, want 2", maxDur)
	}
}

func TestNewConfig_Defaults(t *testing.T) {
	cfg := NewConfig()
	if cfg.Repetitions != DefaultRepetitions {
		t.Errorf("Repetitions: got %d, want %d", cfg.Repetitions, DefaultRepetitions)
	}
	if cfg.TestSequence != DefaultTestSequence {
		t.Errorf("TestSequence: got %q, want %q", cfg.TestSequence, DefaultTestSequence)
	}
	if cfg.ClientAddress != DefaultServerAddress {
		t.Errorf("ClientAddress: got %q, want %q", cfg.ClientAddress, DefaultServerAddress)
	}
	if cfg.TestType != DefaultTestType {
		t.Errorf("TestType: got %q, want %q", cfg.TestType, DefaultTestType)
	}
	if cfg.MaxConnection != DefaultMaxConn {
		t.Errorf("MaxConnection: got %q, want %q", cfg.MaxConnection, DefaultMaxConn)
	}
	if !cfg.CheckServerAlive {
		t.Error("CheckServerAlive should be true by default")
	}
	if cfg.ChainName != "mainnet" {
		t.Errorf("ChainName: got %q, want %q", cfg.ChainName, "mainnet")
	}
}

func TestConfig_Validate_JSONReportWithoutClient(t *testing.T) {
	cfg := NewConfig()
	cfg.JSONReportFile = "report.json"
	cfg.TestingClient = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when JSONReportFile set without TestingClient")
	}
}

func TestConfig_Validate_NonExistentBuildDir(t *testing.T) {
	cfg := NewConfig()
	cfg.ClientBuildDir = "/nonexistent/path/that/does/not/exist"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for nonexistent ClientBuildDir")
	}
}

func TestConfig_Validate_OK(t *testing.T) {
	cfg := NewConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestNewRunDirs(t *testing.T) {
	dirs := NewRunDirs()
	if dirs.RunTestDir == "" {
		t.Error("RunTestDir should not be empty")
	}
	if dirs.PatternDir == "" {
		t.Error("PatternDir should not be empty")
	}
	if dirs.TarFileName == "" {
		t.Error("TarFileName should not be empty")
	}
	if dirs.PatternBase == "" {
		t.Error("PatternBase should not be empty")
	}
}

func TestParseLatency(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"500µs", "500us"},
		{"  150ms  ", "150ms"},
		{"2.5s", "2.5s"},
	}
	for _, tt := range tests {
		got := ParseLatency(tt.input)
		if got != tt.want {
			t.Errorf("ParseLatency(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetCompressionType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"test.tar.gz", GzipCompression},
		{"test.tgz", GzipCompression},
		{"test.tar.bz2", Bzip2Compression},
		{"test.tbz", Bzip2Compression},
		{"test.tar", NoCompression},
		{"test.json", NoCompression},
	}
	for _, tt := range tests {
		got := getCompressionType(tt.filename)
		if got != tt.want {
			t.Errorf("getCompressionType(%q): got %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestHardware_NonLinux(t *testing.T) {
	h := &Hardware{}
	// On macOS (darwin), all Linux-specific methods return "unknown"
	if h.Vendor() != "unknown" && h.Vendor() != "" {
		// On Linux, this would return actual vendor. On macOS, "unknown".
		// Just make sure it doesn't panic.
	}
	_ = h.NormalizedVendor()
	_ = h.Product()
	_ = h.Board()
	_ = h.NormalizedProduct()
	_ = h.NormalizedBoard()
	_ = h.GetCPUModel()
	_ = h.GetBogomips()
}

func TestGetKernelVersion(t *testing.T) {
	v := GetKernelVersion()
	if v == "" {
		t.Error("GetKernelVersion should not return empty string")
	}
}

func TestGetGoVersion(t *testing.T) {
	v := GetGoVersion()
	if v == "" {
		t.Error("GetGoVersion should not return empty string")
	}
}

func TestGetGitCommit_EmptyDir(t *testing.T) {
	commit := GetGitCommit("")
	if commit != "" {
		t.Errorf("GetGitCommit empty dir: got %q, want empty", commit)
	}
}

func TestIsProcessRunning_NonExistent(t *testing.T) {
	if IsProcessRunning("nonexistent_process_12345") {
		t.Error("nonexistent process should not be running")
	}
}
