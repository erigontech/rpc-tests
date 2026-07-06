package rpc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// blockNumberServer returns an httptest.Server that answers eth_blockNumber
// with hexBlocks[call], clamping to the last entry once exhausted.
func blockNumberServer(hexBlocks ...string) *httptest.Server {
	var calls int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(atomic.AddInt64(&calls, 1) - 1)
		if idx >= len(hexBlocks) {
			idx = len(hexBlocks) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  hexBlocks[idx],
		})
	}))
}

func targetOf(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}

func TestGetConsistentLatestBlock_ImmediateMatch(t *testing.T) {
	server1 := blockNumberServer("0x64")
	defer server1.Close()
	server2 := blockNumberServer("0x64")
	defer server2.Close()

	bn, err := GetConsistentLatestBlock(0, targetOf(server1), targetOf(server2), 3, time.Millisecond)
	if err != nil {
		t.Fatalf("GetConsistentLatestBlock: %v", err)
	}
	if bn != 0x64 {
		t.Errorf("block number: got %d, want %d", bn, uint64(0x64))
	}
}

func TestGetConsistentLatestBlock_ConvergesAfterRetries(t *testing.T) {
	server1 := blockNumberServer("0x64") // testing node stays put
	defer server1.Close()
	server2 := blockNumberServer("0x65", "0x65", "0x64") // reference node catches up on 3rd attempt
	defer server2.Close()

	bn, err := GetConsistentLatestBlock(0, targetOf(server1), targetOf(server2), 5, time.Millisecond)
	if err != nil {
		t.Fatalf("GetConsistentLatestBlock: %v", err)
	}
	if bn != 0x64 {
		t.Errorf("block number: got %d, want %d", bn, uint64(0x64))
	}
}

func TestGetConsistentLatestBlock_NeverConverges(t *testing.T) {
	server1 := blockNumberServer("0x64")
	defer server1.Close()
	server2 := blockNumberServer("0x65")
	defer server2.Close()

	_, err := GetConsistentLatestBlock(0, targetOf(server1), targetOf(server2), 3, time.Millisecond)
	if err == nil {
		t.Fatal("expected error when nodes never converge")
	}
	if !strings.Contains(err.Error(), "nodes not synced") {
		t.Errorf("error: got %q, want to contain %q", err.Error(), "nodes not synced")
	}
}
