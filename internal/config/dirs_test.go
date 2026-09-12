package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanOutputDirCreatesMissingDirectory(t *testing.T) {
	cfg := NewConfig()
	cfg.OutputDir = filepath.Join(t.TempDir(), "results")

	if err := cfg.CleanOutputDir(); err != nil {
		t.Fatalf("CleanOutputDir: %v", err)
	}
	info, err := os.Stat(cfg.OutputDir)
	if err != nil {
		t.Fatalf("output dir was not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("output path should be a directory")
	}
}

func TestCleanOutputDirRemovesPreviousContent(t *testing.T) {
	cfg := NewConfig()
	cfg.OutputDir = filepath.Join(t.TempDir(), "results")

	nested := filepath.Join(cfg.OutputDir, "eth_call")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(nested, "test_01-diff.json")
	if err := os.WriteFile(stale, []byte("old diff"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := cfg.CleanOutputDir(); err != nil {
		t.Fatalf("CleanOutputDir: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale result should be gone, stat err = %v", err)
	}
	entries, err := os.ReadDir(cfg.OutputDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("output dir should be empty, found %v", entries)
	}
}

func TestCleanOutputDirBlockedByFile(t *testing.T) {
	cfg := NewConfig()
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg.OutputDir = filepath.Join(blocker, "results")

	if err := cfg.CleanOutputDir(); err == nil {
		t.Error("expected an error when the output path cannot be created")
	}
}

func TestResultsAbsDir(t *testing.T) {
	cfg := NewConfig()
	cfg.OutputDir = "./integration/mainnet/results/"

	got, err := cfg.ResultsAbsDir()
	if err != nil {
		t.Fatalf("ResultsAbsDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("got %q, want an absolute path", got)
	}
	if !strings.HasSuffix(got, filepath.Join("integration", "mainnet", "results")) {
		t.Errorf("got %q, want it to end in integration/mainnet/results", got)
	}
}

func TestResultsAbsDirKeepsAbsolutePath(t *testing.T) {
	cfg := NewConfig()
	cfg.OutputDir = filepath.Join(t.TempDir(), "results")

	got, err := cfg.ResultsAbsDir()
	if err != nil {
		t.Fatalf("ResultsAbsDir: %v", err)
	}
	if got != cfg.OutputDir {
		t.Errorf("got %q, want %q", got, cfg.OutputDir)
	}
}

func TestGenerateJWTSecretLength(t *testing.T) {
	tests := []struct {
		name      string
		length    int
		wantChars int // hex characters after the 0x prefix
	}{
		{"explicit length", 32, 32},
		{"zero falls back to 64", 0, 64},
		{"negative falls back to 64", -1, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "jwt.hex")
			if err := GenerateJWTSecret(path, tt.length); err != nil {
				t.Fatalf("GenerateJWTSecret: %v", err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			content := string(data)
			if !strings.HasPrefix(content, "0x") {
				t.Errorf("secret should be 0x-prefixed, got %q", content)
			}
			body := strings.TrimPrefix(content, "0x")
			if len(body) != tt.wantChars {
				t.Errorf("hex length: got %d, want %d", len(body), tt.wantChars)
			}
			if _, err := hex.DecodeString(body); err != nil {
				t.Errorf("secret is not valid hex: %v", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if perm := info.Mode().Perm(); perm != 0600 {
				t.Errorf("permissions: got %o, want 600", perm)
			}
		})
	}
}

func TestGenerateJWTSecretIsRandom(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.hex")
	second := filepath.Join(dir, "b.hex")

	if err := GenerateJWTSecret(first, 64); err != nil {
		t.Fatalf("GenerateJWTSecret: %v", err)
	}
	if err := GenerateJWTSecret(second, 64); err != nil {
		t.Fatalf("GenerateJWTSecret: %v", err)
	}

	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if string(a) == string(b) {
		t.Error("two generated secrets should differ")
	}
}

func TestGenerateJWTSecretUnwritablePath(t *testing.T) {
	err := GenerateJWTSecret(filepath.Join(t.TempDir(), "missing-dir", "jwt.hex"), 64)
	if err == nil {
		t.Error("expected an error for an unwritable path")
	}
}

// TestGetTargetEnginePorts covers the engine_-method routing, which sends those
// calls to the engine port rather than the normal RPC port.
func TestGetTargetEnginePorts(t *testing.T) {
	cfg := NewConfig()
	cfg.DaemonOnHost = "node1"
	cfg.ServerPort = 0
	cfg.EnginePort = 0

	tests := []struct {
		name       string
		targetType string
		method     string
		verify     bool
		want       string
	}{
		{"default rpc port", DaemonOnDefaultPort, "eth_call", false, "node1:8545"},
		{"default engine port", DaemonOnDefaultPort, "engine_newPayloadV3", false, "node1:8551"},
		{"other port rpc", DaemonOnOtherPort, "eth_call", false, "node1:51515"},
		{"other port engine", DaemonOnOtherPort, "engine_newPayloadV3", false, "node1:51516"},
		// VerifyWithDaemon does not change the "other port" routing: the first two
		// branches of GetTarget resolve to the same ports as the two after them.
		{"verify mode other port rpc", DaemonOnOtherPort, "eth_call", true, "node1:51515"},
		{"verify mode other port engine", DaemonOnOtherPort, "engine_newPayloadV3", true, "node1:51516"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg.VerifyWithDaemon = tt.verify
			if got := cfg.GetTarget(tt.targetType, tt.method); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetTargetExternalProvider(t *testing.T) {
	cfg := NewConfig()
	cfg.ExternalProviderURL = "provider.example:443"
	if got := cfg.GetTarget(ExternalProvider, "eth_call"); got != "provider.example:443" {
		t.Errorf("got %q, want the external provider URL", got)
	}
}

func TestServerEndpointsWithEngineTarget(t *testing.T) {
	cfg := NewConfig()
	cfg.DaemonOnHost = "node1"
	cfg.ServerPort = 8545
	cfg.EnginePort = 8551

	got := cfg.ServerEndpoints()
	if !strings.Contains(got, "node1:8545") || !strings.Contains(got, "node1:8551") {
		t.Errorf("endpoints should mention both ports, got %q", got)
	}
}
