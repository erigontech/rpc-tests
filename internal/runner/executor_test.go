package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/erigontech/rpc-tests/internal/config"
	internalrpc "github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/erigontech/rpc-tests/internal/testdata"
)

// rpcServer returns an httptest.Server answering each JSON-RPC request with
// results[method], and records the order in which methods are called.
func rpcServer(t *testing.T, results map[string]any, calls *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			ID     any    `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("server: cannot decode request: %v", err)
			return
		}
		mu.Lock()
		*calls = append(*calls, req.Method)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  results[req.Method],
		})
	}))
}

// writeFixture writes a fixture file with one command per (method, expectedResult)
// pair and returns the config and descriptor to run it.
func writeFixture(t *testing.T, server *httptest.Server, commands []map[string]any) (*config.Config, *testdata.TestDescriptor) {
	t.Helper()
	tmp := t.TempDir()
	apiDir := filepath.Join(tmp, "testing_api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(commands)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "test_01.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	hostPort := strings.TrimPrefix(server.URL, "http://")
	host, portStr, ok := strings.Cut(hostPort, ":")
	if !ok {
		t.Fatalf("unexpected server URL %q", server.URL)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.NewConfig()
	cfg.DaemonOnHost = host
	cfg.ServerPort = port
	cfg.JSONDir = tmp
	cfg.OutputDir = filepath.Join(tmp, "results") + string(os.PathSeparator)

	descriptor := &testdata.TestDescriptor{
		Name:          filepath.Join("testing_api", "test_01.json"),
		Number:        1,
		TransportType: "http",
	}
	return cfg, descriptor
}

func command(method string, expectedResult any) map[string]any {
	return map[string]any{
		"request":  map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": []any{}},
		"response": map[string]any{"jsonrpc": "2.0", "id": 1, "result": expectedResult},
	}
}

func TestRunTest_SingleCommand(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"m1": "r1"}, &calls)
	defer server.Close()

	cfg, descriptor := writeFixture(t, server, []map[string]any{command("m1", "r1")})
	client := internalrpc.NewClient("http", "", 0)

	outcome := RunTest(context.Background(), descriptor, cfg, client)
	if outcome.Error != nil {
		t.Fatalf("RunTest error: %v", outcome.Error)
	}
	if !outcome.Success {
		t.Error("expected success")
	}
	if len(calls) != 1 {
		t.Errorf("server calls: got %v, want [m1]", calls)
	}
}

func TestRunTest_MultiCommandAllMatch(t *testing.T) {
	var calls []string
	server := rpcServer(t, map[string]any{"m1": "r1", "m2": "r2", "m3": "r3"}, &calls)
	defer server.Close()

	cfg, descriptor := writeFixture(t, server, []map[string]any{
		command("m1", "r1"),
		command("m2", "r2"),
		command("m3", "r3"),
	})
	client := internalrpc.NewClient("http", "", 0)

	outcome := RunTest(context.Background(), descriptor, cfg, client)
	if outcome.Error != nil {
		t.Fatalf("RunTest error: %v", outcome.Error)
	}
	if !outcome.Success {
		t.Error("expected success when every command response matches")
	}
	want := []string{"m1", "m2", "m3"}
	if len(calls) != len(want) {
		t.Fatalf("server calls: got %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("server calls out of order: got %v, want %v", calls, want)
		}
	}
}

func TestRunTest_MultiCommandStopsAtFirstMismatch(t *testing.T) {
	var calls []string
	// The server answers m2 with something different from the fixture expectation.
	server := rpcServer(t, map[string]any{"m1": "r1", "m2": "unexpected", "m3": "r3"}, &calls)
	defer server.Close()

	cfg, descriptor := writeFixture(t, server, []map[string]any{
		command("m1", "r1"),
		command("m2", "r2"),
		command("m3", "r3"),
	})
	client := internalrpc.NewClient("http", "", 0)

	outcome := RunTest(context.Background(), descriptor, cfg, client)
	if outcome.Success {
		t.Error("expected failure when a command response mismatches")
	}
	want := []string{"m1", "m2"}
	if len(calls) != len(want) {
		t.Fatalf("expected to stop after the mismatching command: got calls %v, want %v", calls, want)
	}
}

func TestRunTest_EmptyFixture(t *testing.T) {
	var calls []string
	server := rpcServer(t, nil, &calls)
	defer server.Close()

	cfg, descriptor := writeFixture(t, server, []map[string]any{})
	client := internalrpc.NewClient("http", "", 0)

	outcome := RunTest(context.Background(), descriptor, cfg, client)
	if outcome.Error == nil {
		t.Fatal("expected an error for a fixture with no commands")
	}
	if len(calls) != 0 {
		t.Errorf("no RPC call expected, got %v", calls)
	}
}
