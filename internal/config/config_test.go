package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfig_Defaults(t *testing.T) {
	c := NewConfig()

	if !c.ExitOnFail {
		t.Error("ExitOnFail should default to true")
	}
	if !c.Parallel {
		t.Error("Parallel should default to true")
	}
	if c.LoopNumber != 1 {
		t.Errorf("LoopNumber: got %d, want 1", c.LoopNumber)
	}
	if c.ReqTestNum != -1 {
		t.Errorf("ReqTestNum: got %d, want -1", c.ReqTestNum)
	}
	if c.Net != "mainnet" {
		t.Errorf("Net: got %q, want %q", c.Net, "mainnet")
	}
	if c.DaemonOnHost != "localhost" {
		t.Errorf("DaemonOnHost: got %q, want %q", c.DaemonOnHost, "localhost")
	}
	if c.DiffKind != JsonDiffGo {
		t.Errorf("DiffKind: got %v, want %v", c.DiffKind, JsonDiffGo)
	}
	if c.TransportType != TransportHTTP {
		t.Errorf("TransportType: got %q, want %q", c.TransportType, TransportHTTP)
	}
	if c.DaemonUnderTest != DaemonOnDefaultPort {
		t.Errorf("DaemonUnderTest: got %q, want %q", c.DaemonUnderTest, DaemonOnDefaultPort)
	}
	if c.DaemonAsReference != None {
		t.Errorf("DaemonAsReference: got %q, want %q", c.DaemonAsReference, None)
	}
}

func TestValidate_WaitingTimeParallel(t *testing.T) {
	c := NewConfig()
	c.WaitingTime = 100
	c.Parallel = true
	if err := c.Validate(); err == nil {
		t.Error("expected error for waiting-time with parallel")
	}
}

func TestValidate_DaemonPortWithCompare(t *testing.T) {
	c := NewConfig()
	c.DaemonUnderTest = DaemonOnOtherPort
	c.VerifyWithDaemon = true
	c.DaemonAsReference = DaemonOnDefaultPort
	if err := c.Validate(); err == nil {
		t.Error("expected error for daemon-port with compare")
	}
}

func TestValidate_RunTestWithExclude(t *testing.T) {
	c := NewConfig()
	c.ReqTestNum = 5
	c.ExcludeTestList = "1,2,3"
	if err := c.Validate(); err == nil {
		t.Error("expected error for run-test with exclude-test-list")
	}
}

func TestValidate_ApiListWithExcludeApi(t *testing.T) {
	c := NewConfig()
	c.TestingAPIs = "eth_call"
	c.ExcludeAPIList = "eth_getBalance"
	if err := c.Validate(); err == nil {
		t.Error("expected error for api-list with exclude-api-list")
	}
}

func TestValidate_CompareWithoutCompare(t *testing.T) {
	c := NewConfig()
	c.VerifyWithDaemon = true
	c.WithoutCompareResults = true
	if err := c.Validate(); err == nil {
		t.Error("expected error for compare with without-compare")
	}
}

func TestValidate_InvalidTransport(t *testing.T) {
	c := NewConfig()
	c.TransportType = "invalid"
	if err := c.Validate(); err == nil {
		t.Error("expected error for invalid transport type")
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	c := NewConfig()
	if err := c.Validate(); err != nil {
		t.Errorf("valid config should not error: %v", err)
	}
}

func TestUpdateDirs(t *testing.T) {
	c := NewConfig()
	c.Net = "sepolia"
	c.UpdateDirs()

	if c.JSONDir != "./integration/sepolia/" {
		t.Errorf("JSONDir: got %q, want %q", c.JSONDir, "./integration/sepolia/")
	}
	if c.OutputDir != "./integration/sepolia/results/" {
		t.Errorf("OutputDir: got %q, want %q", c.OutputDir, "./integration/sepolia/results/")
	}
	if c.ServerPort != DefaultServerPort {
		t.Errorf("ServerPort: got %d, want %d", c.ServerPort, DefaultServerPort)
	}
	if c.EnginePort != DefaultEnginePort {
		t.Errorf("EnginePort: got %d, want %d", c.EnginePort, DefaultEnginePort)
	}
	if c.LocalServer != "http://localhost:8545" {
		t.Errorf("LocalServer: got %q, want %q", c.LocalServer, "http://localhost:8545")
	}
}

func TestUpdateDirs_CustomPorts(t *testing.T) {
	c := NewConfig()
	c.ServerPort = 9090
	c.EnginePort = 9091
	c.UpdateDirs()

	if c.ServerPort != 9090 {
		t.Errorf("ServerPort: got %d, want 9090", c.ServerPort)
	}
	if c.EnginePort != 9091 {
		t.Errorf("EnginePort: got %d, want 9091", c.EnginePort)
	}
}

func TestGetTarget(t *testing.T) {
	c := NewConfig()
	c.UpdateDirs()

	tests := []struct {
		name       string
		targetType string
		method     string
		want       string
	}{
		{"default eth_call", DaemonOnDefaultPort, "eth_call", "localhost:8545"},
		{"default engine_", DaemonOnDefaultPort, "engine_exchangeCapabilities", "localhost:8551"},
		{"other port eth", DaemonOnOtherPort, "eth_call", "localhost:51515"},
		{"other port engine", DaemonOnOtherPort, "engine_exchangeCapabilities", "localhost:51516"},
		{"external provider", ExternalProvider, "eth_call", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.targetType == ExternalProvider {
				c.ExternalProviderURL = "http://example.com"
				tt.want = "http://example.com"
			}
			got := c.GetTarget(tt.targetType, tt.method)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetJSONFilenameExt(t *testing.T) {
	tests := []struct {
		targetType string
		target     string
		want       string
	}{
		{DaemonOnOtherPort, "localhost:51515", "_51515-daemon.json"},
		{ExternalProvider, "http://example.com", "-external_provider_url.json"},
		{DaemonOnDefaultPort, "localhost:8545", "_8545-rpcdaemon.json"},
	}

	for _, tt := range tests {
		got := GetJSONFilenameExt(tt.targetType, tt.target)
		if got != tt.want {
			t.Errorf("GetJSONFilenameExt(%q, %q): got %q, want %q", tt.targetType, tt.target, got, tt.want)
		}
	}
}

func TestParseDiffKind(t *testing.T) {
	tests := []struct {
		input string
		want  DiffKind
		err   bool
	}{
		{"jd", JdLibrary, false},
		{"json-diff", JsonDiffTool, false},
		{"diff", DiffTool, false},
		{"json-diff-go", JsonDiffGo, false},
		{"JD", JdLibrary, false},
		{"invalid", JdLibrary, true},
	}

	for _, tt := range tests {
		got, err := ParseDiffKind(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("ParseDiffKind(%q): error = %v, wantErr %v", tt.input, err, tt.err)
		}
		if !tt.err && got != tt.want {
			t.Errorf("ParseDiffKind(%q): got %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDiffKind_String(t *testing.T) {
	tests := []struct {
		kind DiffKind
		want string
	}{
		{JdLibrary, "jd"},
		{JsonDiffTool, "json-diff"},
		{DiffTool, "diff"},
		{JsonDiffGo, "json-diff-go"},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("DiffKind(%d).String(): got %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestIsValidTransport(t *testing.T) {
	valid := []string{"http", "http_comp", "https", "websocket", "websocket_comp"}
	for _, v := range valid {
		if !IsValidTransport(v) {
			t.Errorf("IsValidTransport(%q) should be true", v)
		}
	}

	invalid := []string{"tcp", "grpc", "ftp", ""}
	for _, v := range invalid {
		if IsValidTransport(v) {
			t.Errorf("IsValidTransport(%q) should be false", v)
		}
	}
}

func TestTransportTypes(t *testing.T) {
	c := NewConfig()
	c.TransportType = "http,websocket"
	types := c.TransportTypes()
	if len(types) != 2 || types[0] != "http" || types[1] != "websocket" {
		t.Errorf("TransportTypes: got %v", types)
	}
}

func TestServerEndpoints(t *testing.T) {
	c := NewConfig()
	c.UpdateDirs()

	endpoints := c.ServerEndpoints()
	if endpoints != "localhost:8545/localhost:8551" {
		t.Errorf("ServerEndpoints: got %q", endpoints)
	}
}

func TestServerEndpoints_VerifyWithDaemon(t *testing.T) {
	c := NewConfig()
	c.UpdateDirs()
	c.VerifyWithDaemon = true
	c.DaemonAsReference = ExternalProvider
	c.ExternalProviderURL = "http://infura.io"

	endpoints := c.ServerEndpoints()
	want := "both servers (rpcdaemon with http://infura.io)"
	if endpoints != want {
		t.Errorf("ServerEndpoints: got %q, want %q", endpoints, want)
	}
}

func TestJWTSecret_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "jwt.hex")

	if err := GenerateJWTSecret(path, 64); err != nil {
		t.Fatalf("GenerateJWTSecret: %v", err)
	}

	secret, err := GetJWTSecret(path)
	if err != nil {
		t.Fatalf("GetJWTSecret: %v", err)
	}

	if len(secret) != 64 {
		t.Errorf("secret length: got %d, want 64", len(secret))
	}

	// Verify it's valid hex
	for _, c := range secret {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("secret contains non-hex char: %c", c)
		}
	}
}

func TestGetJWTSecret_FileNotFound(t *testing.T) {
	_, err := GetJWTSecret("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestGetJWTSecret_Without0xPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "jwt.hex")
	if err := os.WriteFile(path, []byte("abcdef1234567890"), 0600); err != nil {
		t.Fatal(err)
	}

	secret, err := GetJWTSecret(path)
	if err != nil {
		t.Fatalf("GetJWTSecret: %v", err)
	}
	if secret != "abcdef1234567890" {
		t.Errorf("got %q, want %q", secret, "abcdef1234567890")
	}
}
