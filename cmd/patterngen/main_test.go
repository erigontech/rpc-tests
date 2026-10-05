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

// node answers eth_blockNumber and eth_getBlockByNumber with one block holding one tx.
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
