package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// node answers eth_blockNumber and eth_getBlockByNumber with one block holding one tx, eth_getTransactionByHash
// with that tx, eth_getBlockReceipts with its receipt, and eth_getLogs with one log.
func node(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := string(body)
		switch {
		case strings.Contains(req, "eth_blockNumber"):
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x10"}`)
		case strings.Contains(req, "eth_getBlockByNumber"):
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"number":"0x10","hash":"0x1","transactions":[{"hash":"0xa","from":"0x01","to":"0x02","gas":"0x5208","gasPrice":"0x1","value":"0x0","input":"0x"}]}}`)
		case strings.Contains(req, "eth_getTransactionByHash"):
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"hash":"0xa","blockNumber":"0x10","from":"0x01","to":"0x02","gas":"0x5208","gasPrice":"0x1","value":"0x0","input":"0x"}}`)
		case strings.Contains(req, "eth_getBlockReceipts"):
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":[{"transactionHash":"0xa","gasUsed":"0x5208","status":"0x1"}]}`)
		case strings.Contains(req, "eth_getLogs"):
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":[{"address":"0x02","topics":["0x7e"],"transactionHash":"0xa","logIndex":"0x0"}]}`)
		default:
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestEthCallCommandWritesThePattern(t *testing.T) {
	out := filepath.Join(t.TempDir(), "eth_call.tar")
	err := newApp().Run([]string{"rpc_pattern_gen", "eth_call", "--url", node(t), "--blocks", "1", "--out", out})
	if err != nil {
		t.Fatalf("eth_call command: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("pattern not written: %v", err)
	}
}

func TestEthGetLogsCommandWritesThePattern(t *testing.T) {
	out := filepath.Join(t.TempDir(), "eth_getLogs.tar")
	err := newApp().Run([]string{"rpc_pattern_gen", "eth_getLogs", "--url", node(t), "--counts", "1", "--range", "10", "--verify", "--out", out})
	if err != nil {
		t.Fatalf("eth_getLogs command: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("pattern not written: %v", err)
	}
	if err := newApp().Run([]string{"rpc_pattern_gen", "eth_getLogs", "--url", node(t), "--counts", "1", "--range", "0", "--out", out}); err == nil {
		t.Error("range 0: want an error")
	}
}

func TestProfileWritesThePattern(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "mixed.profile")
	if err := os.WriteFile(profile, []byte("[methods]\neth_call 3\neth_chainId 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "mixed.tar")
	err := newApp().Run([]string{"rpc_pattern_gen", "--profile", profile, "--url", node(t), "--counts", "4", "--out", out})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("pattern not written: %v", err)
	}
}

func TestProfileWithOneMethodDrawsFromTheWindow(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "eth_call.profile")
	if err := os.WriteFile(profile, []byte("[methods]\neth_call 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "eth_call.tar")
	if err := newApp().Run([]string{"rpc_pattern_gen", "--profile", profile, "--url", node(t), "--counts", "4", "--max-blocks", "2", "--out", out}); err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := newApp().Run([]string{"rpc_pattern_gen", "--profile", profile, "--url", node(t), "--blocks", "1", "--out", out}); err == nil {
		t.Error("profile with --blocks: want an error, the window needs --counts")
	}
}

func TestProfileWithLogsOptionsWritesThePattern(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "eth_getLogs.profile")
	if err := os.WriteFile(profile, []byte("[methods]\neth_getLogs 1 filter=address+topics or=4 range=10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "eth_getLogs.tar")
	if err := newApp().Run([]string{"rpc_pattern_gen", "--profile", profile, "--url", node(t), "--counts", "4", "--max-blocks", "2", "--out", out}); err != nil {
		t.Fatalf("profile: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("pattern not written: %v", err)
	}
}

func TestEthCallCommandWithBlockRangeWritesThePattern(t *testing.T) {
	for _, args := range [][]string{
		{"--start-block", "5", "--end-block", "8"},
		{"--start-block", "5", "--max-blocks", "2"},
		{"--start-block", "5", "--end-block", "8", "--max-blocks", "2"},
		{"--start-block", "5", "--end-block", "8", "--sample-blocks", "2"},
	} {
		out := filepath.Join(t.TempDir(), "eth_call.tar")
		cmd := append([]string{"rpc_pattern_gen", "eth_call", "--url", node(t), "--block-tag", "parent", "--out", out}, args...)
		if err := newApp().Run(cmd); err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if _, err := os.Stat(out); err != nil {
			t.Errorf("%v: pattern not written: %v", args, err)
		}
	}
}

func TestBlockRangeFlagsRejectInvalidCombinations(t *testing.T) {
	for _, args := range [][]string{
		{"--start-block", "5"},
		{"--end-block", "8"},
		{"--start-block", "5", "--end-block", "8", "--blocks", "1"},
		{"--sample-blocks", "2", "--counts", "1"},
	} {
		out := filepath.Join(t.TempDir(), "eth_call.tar")
		cmd := append([]string{"rpc_pattern_gen", "eth_call", "--url", node(t), "--block-tag", "parent", "--out", out}, args...)
		if err := newApp().Run(cmd); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestProfileWithBlockRangeWritesThePattern(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "mixed.profile")
	if err := os.WriteFile(profile, []byte("[methods]\neth_call 3\neth_chainId 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "mixed.tar")
	if err := newApp().Run([]string{"rpc_pattern_gen", "--profile", profile, "--url", node(t), "--counts", "4", "--block-tag", "parent",
		"--start-block", "5", "--max-blocks", "2", "--out", out}); err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := newApp().Run([]string{"rpc_pattern_gen", "--profile", profile, "--url", node(t), "--block-tag", "parent",
		"--start-block", "5", "--max-blocks", "2", "--out", out}); err == nil {
		t.Error("profile with a block range and no --counts: want an error")
	}
}

func TestPerBlockFlag(t *testing.T) {
	out := filepath.Join(t.TempDir(), "eth_call.tar")
	if err := newApp().Run([]string{"rpc_pattern_gen", "eth_call", "--url", node(t), "--blocks", "1", "--per-block", "1", "--out", out}); err != nil {
		t.Fatalf("eth_call with --per-block: %v", err)
	}
	profile := filepath.Join(t.TempDir(), "eth_call.profile")
	if err := os.WriteFile(profile, []byte("[methods]\neth_call 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := newApp().Run([]string{"rpc_pattern_gen", "--profile", profile, "--url", node(t), "--counts", "4", "--per-block", "1", "--out", out}); err == nil {
		t.Error("--per-block with a profile: want an error")
	}
}

func TestEthCallCommandWritesJSONL(t *testing.T) {
	out := filepath.Join(t.TempDir(), "eth_call.jsonl")
	if err := newApp().Run([]string{"rpc_pattern_gen", "eth_call", "--url", node(t), "--blocks", "1", "--out", out}); err != nil {
		t.Fatalf("eth_call command: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("pattern not written: %v", err)
	}
	if !strings.HasPrefix(string(data), `{"jsonrpc":"2.0","method":"eth_call"`) {
		t.Errorf("not a JSONL pattern: %s", data)
	}
}

func TestJSONLRejectsTheVegetaFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--target-url", "http://localhost:8545"},
		{"--name", "eth_call"},
	} {
		out := filepath.Join(t.TempDir(), "eth_call.jsonl")
		cmd := append([]string{"rpc_pattern_gen", "eth_call", "--url", node(t), "--blocks", "1", "--out", out}, args...)
		if err := newApp().Run(cmd); err == nil {
			t.Errorf("%v with a .jsonl output: want an error", args)
		}
	}
}

func TestEthCallCommandWithTxsWritesOneCallPerTx(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "slow.txt")
	if err := os.WriteFile(list, []byte("# slow calls\n1-div-loop 0x5a158d08067feeffe06fb1930c50c9f82b0c9175a22d6d110c190fe0dd47d1aa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "slow.jsonl")
	if err := newApp().Run([]string{"rpc_pattern_gen", "eth_call", "--url", node(t), "--txs", list, "--out", out}); err != nil {
		t.Fatalf("eth_call with --txs: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("pattern not written: %v", err)
	}
	if !strings.HasSuffix(string(data), `"id":"1-div-loop"}`+"\n") {
		t.Errorf("not the call of 1-div-loop: %s", data)
	}
	for _, args := range [][]string{{"--counts", "1"}, {"--blocks", "1"}, {"--start-block", "5", "--end-block", "8"}} {
		cmd := append([]string{"rpc_pattern_gen", "eth_call", "--url", node(t), "--block-tag", "parent", "--txs", list, "--out", out}, args...)
		if err := newApp().Run(cmd); err == nil {
			t.Errorf("--txs with %v: want an error", args)
		}
	}
}

func TestTxListCommandWritesTheHeaviestTxs(t *testing.T) {
	out := filepath.Join(t.TempDir(), "candidates.txt")
	if err := newApp().Run([]string{"rpc_pattern_gen", "tx-list", "--url", node(t), "--top-gas", "1", "--blocks", "1", "--out", out}); err != nil {
		t.Fatalf("tx-list: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("list not written: %v", err)
	}
	if !strings.Contains(string(data), "\n1-0.02Mgas 0xa\n") {
		t.Errorf("not the heaviest tx: %s", data)
	}
	for _, args := range [][]string{{"--blocks", "1"}, {"--top-gas", "1"}, {"--top-gas", "1", "--blocks", "1", "--start-block", "5", "--end-block", "8"}} {
		cmd := append([]string{"rpc_pattern_gen", "tx-list", "--url", node(t), "--out", out}, args...)
		if err := newApp().Run(cmd); err == nil {
			t.Errorf("tx-list %v: want an error", args)
		}
	}
}
