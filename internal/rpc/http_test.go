package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sequenceServer returns an httptest.Server that answers with results[call], clamping to
// the last entry once exhausted.
func sequenceServer(results ...any) *httptest.Server {
	var calls atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := min(int(calls.Add(1)-1), len(results)-1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  results[idx],
		})
	}))
}

// blockNumberServer answers eth_blockNumber with the given block numbers, in order.
func blockNumberServer(hexBlocks ...string) *httptest.Server {
	results := make([]any, 0, len(hexBlocks))
	for _, block := range hexBlocks {
		results = append(results, block)
	}
	return sequenceServer(results...)
}

func targetOf(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}

func TestGetLatestHead(t *testing.T) {
	server := sequenceServer(map[string]any{
		"number": "0x1e8481",
		"hash":   "0xabc",
		// A real header carries many more fields: they must not disturb the read.
		"parentHash": "0xdef",
	})
	defer server.Close()

	head, _, err := GetLatestHead(context.Background(), NewClient("http", "", 0), targetOf(server))
	if err != nil {
		t.Fatalf("GetLatestHead: %v", err)
	}
	if head.Number != 0x1e8481 {
		t.Errorf("head number: got %d, want %d", head.Number, uint64(0x1e8481))
	}
	if head.Hash != "0xabc" {
		t.Errorf("head hash: got %q, want %q", head.Hash, "0xabc")
	}
}

func TestGetLatestHead_Errors(t *testing.T) {
	tests := []struct {
		name   string
		result any
	}{
		{"null result", nil},
		{"missing number", map[string]any{"hash": "0xabc"}},
		{"missing hash", map[string]any{"number": "0x1"}},
		{"number not hex", map[string]any{"number": "0xzz", "hash": "0xabc"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := sequenceServer(tc.result)
			defer server.Close()

			if _, _, err := GetLatestHead(context.Background(), NewClient("http", "", 0), targetOf(server)); err == nil {
				t.Error("expected an error")
			}
		})
	}
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
