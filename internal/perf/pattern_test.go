package perf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const jsonlHeader = `{"pattern":{"description":"eth_call of the txs of the most recent blocks","network":"mainnet","generator":"rpc_pattern_gen eth_call --counts 2","firstBlock":15,"lastBlock":16,"requests":2,"created":"2026-11-03T10:12:00Z"}}`

func TestParsePatternHeader(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		ok   bool
	}{
		{"header", jsonlHeader, true},
		{"header with spaces", "  " + jsonlHeader + " \r", true},
		{"request", jsonlCall, false},
		{"request with a pattern param", `{"jsonrpc":"2.0","method":"x","params":[],"id":1,"pattern":{}}`, false},
		{"pattern not an object", `{"pattern":"x"}`, false},
		{"not JSON", `{"pattern":`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := ParsePatternHeader([]byte(tc.line))
			if ok != tc.ok {
				t.Fatalf("ok %v, want %v", ok, tc.ok)
			}
			if ok && (info.Description != "eth_call of the txs of the most recent blocks" || info.FirstBlock != 15 || info.Requests != 2) {
				t.Errorf("info %+v", info)
			}
		})
	}
}

func TestWritePatternHeaderIsReadBack(t *testing.T) {
	var buf bytes.Buffer
	want := PatternInfo{Description: "d", Network: "sepolia", Requests: 3}
	if err := WritePatternHeader(&buf, want); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(buf.String(), "}\n") || strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("not one line: %q", buf.String())
	}
	got, ok := ParsePatternHeader(buf.Bytes())
	if !ok || *got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReadPatternHeader(t *testing.T) {
	dir := t.TempDir()
	with := filepath.Join(dir, "with.jsonl")
	if err := os.WriteFile(with, []byte("\n"+jsonlHeader+"\n"+jsonlCall+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, offset, err := ReadPatternHeader(with)
	if err != nil || info == nil || info.Network != "mainnet" {
		t.Fatalf("info %+v, err %v", info, err)
	}
	if want := int64(1 + len(jsonlHeader) + 1); offset != want {
		t.Errorf("offset %d, want %d", offset, want)
	}

	without := filepath.Join(dir, "without.jsonl")
	if err := os.WriteFile(without, []byte(jsonlCall+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if info, _, err := ReadPatternHeader(without); err != nil || info != nil {
		t.Errorf("without a header: info %+v, err %v", info, err)
	}
}

func TestBSDSumMatchesTheSumCommand(t *testing.T) {
	if _, err := exec.LookPath("sum"); err != nil {
		t.Skip("no sum command")
	}
	path := filepath.Join(t.TempDir(), "p.jsonl")
	if err := os.WriteFile(path, []byte(strings.Repeat(jsonlCall+"\n", 500)), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := bsdSum(f)
	if err != nil {
		t.Fatal(err)
	}
	if want := GetFileChecksum(path); got != want {
		t.Errorf("bsdSum %s, sum %s", got, want)
	}
}

func TestPatternChecksumIgnoresTheHeader(t *testing.T) {
	if _, err := exec.LookPath("sum"); err != nil {
		t.Skip("no sum command")
	}
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.jsonl")
	if err := os.WriteFile(plain, []byte(jsonlCall+"\n"+jsonlCall+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withHeader := filepath.Join(dir, "header.jsonl")
	if err := os.WriteFile(withHeader, []byte(jsonlHeader+"\n"+jsonlCall+"\n"+jsonlCall+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pattern, got, err := patternChecksum(withHeader)
	if err != nil || pattern == nil {
		t.Fatalf("pattern %+v, err %v", pattern, err)
	}
	if want := GetFileChecksum(plain); got != want {
		t.Errorf("checksum %s, want %s: the checksum of the requests alone", got, want)
	}
	if pattern, got, err := patternChecksum(plain); err != nil || pattern != nil || got != GetFileChecksum(plain) {
		t.Errorf("without a header: pattern %+v, checksum %s, err %v", pattern, got, err)
	}
}

func TestLoadTargetsSkipsThePatternHeader(t *testing.T) {
	pt, cfg, _ := newJSONLPerf(t, "localhost", jsonlHeader, jsonlCall)
	targets, err := pt.loadTargets(cfg.VegetaPatternTarFile)
	if err != nil {
		t.Fatalf("loadTargets: %v", err)
	}
	if len(targets) != 1 || string(targets[0].Body) != jsonlCall {
		t.Errorf("targets %d, want the request alone", len(targets))
	}
}

func TestLoadTargetsRejectsAHeaderAfterTheFirstLine(t *testing.T) {
	pt, cfg, _ := newJSONLPerf(t, "localhost", jsonlCall, jsonlHeader)
	if _, err := pt.loadTargets(cfg.VegetaPatternTarFile); err == nil {
		t.Error("want an error for a header after the first line")
	}
}

func TestJSONLTestTypeSkipsThePatternHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.jsonl")
	if err := os.WriteFile(path, []byte(jsonlHeader+"\n"+jsonlCall+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := JSONLTestType(path); err != nil || got != "eth_call" {
		t.Errorf("got %q, %v; want eth_call", got, err)
	}
}

func TestJSONReportHoldsThePatternHeader(t *testing.T) {
	dirs := reportDirs(t)
	cfg := NewConfig()
	cfg.TestType = "eth_call"
	cfg.TestingClient = "erigon"
	cfg.JSONReportFile = filepath.Join(t.TempDir(), "report.json")
	cfg.VegetaPatternTarFile = filepath.Join(t.TempDir(), "p.jsonl")
	if err := os.WriteFile(cfg.VegetaPatternTarFile, []byte(jsonlHeader+"\n"+jsonlCall+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dirs.RunTestDir, BinaryDir, "run.bin")
	writeVegetaBinary(t, binary)
	cfg.BinaryFile = "run.bin"
	cfg.BinaryFileFullPathname = binary

	report := NewTestReport(cfg, dirs)
	if err := report.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := report.WriteTestReport(sampleMetrics(0)); err != nil {
		t.Fatalf("WriteTestReport: %v", err)
	}
	if err := report.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(cfg.JSONReportFile)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Configuration struct {
			Pattern json.RawMessage `json:"pattern"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	want, _ := ParsePatternHeader([]byte(jsonlHeader))
	var got PatternInfo
	if err := json.Unmarshal(parsed.Configuration.Pattern, &got); err != nil || got != *want {
		t.Errorf("configuration.pattern %s, want %+v", parsed.Configuration.Pattern, *want)
	}
}

// clientVersionNode starts a node that answers web3_clientVersion with version and returns its host:port.
func clientVersionNode(t *testing.T, version string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"web3_clientVersion"`) {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":%q}`, version)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestClientVersion(t *testing.T) {
	address := clientVersionNode(t, "erigon/3.1.0/linux-amd64/go1.24.4")
	if got := ClientVersion(NodeURL(address)); got != "erigon/3.1.0/linux-amd64/go1.24.4" {
		t.Errorf("ClientVersion = %q", got)
	}
	// No node: an empty version, no error.
	if got := ClientVersion("http://127.0.0.1:1"); got != "" {
		t.Errorf("ClientVersion without a node = %q, want empty", got)
	}
	// A node that answers with an error: an empty version.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"the method web3_clientVersion does not exist"}}`)
	}))
	defer srv.Close()
	if got := ClientVersion(srv.URL); got != "" {
		t.Errorf("ClientVersion on an error = %q, want empty", got)
	}
}

func TestNodeURL(t *testing.T) {
	for address, want := range map[string]string{
		"localhost":      "http://localhost:8545",
		"10.0.0.5":       "http://10.0.0.5:8545",
		"10.0.0.5:51515": "http://10.0.0.5:51515",
	} {
		if got := NodeURL(address); got != want {
			t.Errorf("NodeURL(%q) = %q, want %q", address, got, want)
		}
	}
}
