package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	json "encoding/json"
)

func TestNewClient(t *testing.T) {
	c := NewClient("http", "Bearer token", 1)
	if c.transport != "http" {
		t.Errorf("transport: got %q, want %q", c.transport, "http")
	}
	if c.jwtAuth != "Bearer token" {
		t.Errorf("jwtAuth: got %q, want %q", c.jwtAuth, "Bearer token")
	}
	if c.verbose != 1 {
		t.Errorf("verbose: got %d, want 1", c.verbose)
	}
}

func TestCallHTTP_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: got %q", ct)
		}
		if ae := r.Header.Get("Accept-Encoding"); ae != "Identity" {
			t.Errorf("Accept-Encoding: got %q, want Identity", ae)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0x1",
		})
	}))
	defer server.Close()

	// Strip http:// prefix since the client adds it
	target := strings.TrimPrefix(server.URL, "http://")
	client := NewClient("http", "", 0)

	var response any
	metrics, err := client.Call(context.Background(), target, []byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`), &response)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if metrics.RoundTripTime == 0 {
		t.Error("RoundTripTime should be > 0")
	}
	if metrics.UnmarshallingTime == 0 {
		t.Error("UnmarshallingTime should be > 0")
	}

	respMap, ok := response.(map[string]interface{})
	if !ok {
		t.Fatal("response is not a map")
	}
	if respMap["result"] != "0x1" {
		t.Errorf("result: got %v", respMap["result"])
	}
}

func TestCallHTTP_JWTHeader(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  nil,
		})
	}))
	defer server.Close()

	target := strings.TrimPrefix(server.URL, "http://")
	client := NewClient("http", "Bearer mytoken", 0)

	var response any
	_, err := client.Call(context.Background(), target, []byte(`{}`), &response)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if gotAuth != "Bearer mytoken" {
		t.Errorf("Authorization: got %q, want %q", gotAuth, "Bearer mytoken")
	}
}

func TestCallHTTP_Compression(t *testing.T) {
	var gotAcceptEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  nil,
		})
	}))
	defer server.Close()

	target := strings.TrimPrefix(server.URL, "http://")

	// http_comp should NOT set Accept-Encoding: Identity
	client := NewClient("http_comp", "", 0)
	var response any
	_, err := client.Call(context.Background(), target, []byte(`{}`), &response)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if gotAcceptEncoding == "Identity" {
		t.Error("http_comp should not set Accept-Encoding: Identity")
	}
}

func TestCallHTTP_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	target := strings.TrimPrefix(server.URL, "http://")
	client := NewClient("http", "", 0)

	var response any
	_, err := client.Call(context.Background(), target, []byte(`{}`), &response)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestCallHTTP_ConnectionRefused(t *testing.T) {
	client := NewClient("http", "", 0)
	var response any
	_, err := client.Call(context.Background(), "localhost:1", []byte(`{}`), &response)
	if err == nil {
		t.Error("expected error for connection refused")
	}
}

func TestCallHTTP_UnsupportedTransport(t *testing.T) {
	client := NewClient("grpc", "", 0)
	var response any
	_, err := client.Call(context.Background(), "localhost:1", []byte(`{}`), &response)
	if err == nil {
		t.Error("expected error for unsupported transport")
	}
}

func TestValidateJsonRpcResponse_Valid(t *testing.T) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result":  "0x1",
	}
	if err := ValidateJsonRpcResponse(resp); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateJsonRpcResponse_MissingJsonrpc(t *testing.T) {
	resp := map[string]any{
		"id":     float64(1),
		"result": "0x1",
	}
	if err := ValidateJsonRpcResponse(resp); err == nil {
		t.Error("expected error for missing jsonrpc")
	}
}

func TestValidateJsonRpcResponse_MissingId(t *testing.T) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"result":  "0x1",
	}
	if err := ValidateJsonRpcResponse(resp); err == nil {
		t.Error("expected error for missing id")
	}
}

func TestValidateJsonRpcResponse_BatchValid(t *testing.T) {
	resp := []any{
		map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": "0x1"},
		map[string]any{"jsonrpc": "2.0", "id": float64(2), "result": "0x2"},
	}
	if err := ValidateJsonRpcResponse(resp); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseHexUint64(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
		err   bool
	}{
		{"0", 0, false},
		{"1", 1, false},
		{"a", 10, false},
		{"ff", 255, false},
		{"100", 256, false},
		{"12ab34", 0x12ab34, false},
		{"DEADBEEF", 0xDEADBEEF, false},
		{"xyz", 0, true},
	}

	for _, tt := range tests {
		got, err := parseHexUint64(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("parseHexUint64(%q): error = %v, wantErr %v", tt.input, err, tt.err)
		}
		if !tt.err && got != tt.want {
			t.Errorf("parseHexUint64(%q): got %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCallHTTPRaw_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer server.Close()

	target := strings.TrimPrefix(server.URL, "http://")

	var gotRTT bool
	err := CallHTTPRaw(context.Background(), 0, "http", "", target, []byte(`{}`), func(resp *http.Response, err error, rtt time.Duration) error {
		gotRTT = rtt > 0
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return nil
	})
	if err != nil {
		t.Fatalf("CallHTTPRaw: %v", err)
	}
	if !gotRTT {
		t.Error("expected positive RTT")
	}
}
