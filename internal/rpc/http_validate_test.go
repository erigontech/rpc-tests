package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateJsonRpcResponsePointer(t *testing.T) {
	valid := map[string]any{"jsonrpc": "2.0", "id": 1.0, "result": "0x1"}
	if err := ValidateJsonRpcResponse(&valid); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	invalid := map[string]any{"id": 1.0}
	if err := ValidateJsonRpcResponse(&invalid); err == nil {
		t.Error("expected an error for a response missing 'jsonrpc'")
	}
}

func TestValidateJsonRpcResponseNilPointer(t *testing.T) {
	var nilMap *map[string]any
	err := ValidateJsonRpcResponse(nilMap)
	if err == nil || !strings.Contains(err.Error(), "nil response pointer") {
		t.Errorf("got %v, want a nil-pointer error", err)
	}
}

func TestValidateJsonRpcResponseWrongVersion(t *testing.T) {
	tests := []struct {
		name    string
		jsonrpc any
	}{
		{"1.0", "1.0"},
		{"not a string", 2.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJsonRpcResponse(map[string]any{"jsonrpc": tt.jsonrpc, "id": 1.0})
			if err == nil || !strings.Contains(err.Error(), "noncompliant JSON-RPC 2.0 version") {
				t.Errorf("got %v, want a version error", err)
			}
		})
	}
}

func TestValidateJsonRpcResponseBatchWithInvalidElement(t *testing.T) {
	batch := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1.0},
		map[string]any{"jsonrpc": "2.0"}, // missing id
	}
	if err := ValidateJsonRpcResponse(batch); err == nil {
		t.Error("expected an error when a batch element is invalid")
	}
}

// TestValidateJsonRpcResponseBatchSkipsNonObjects documents that non-object
// batch elements are ignored rather than rejected.
func TestValidateJsonRpcResponseBatchSkipsNonObjects(t *testing.T) {
	batch := []any{"garbage", 42, map[string]any{"jsonrpc": "2.0", "id": 1.0}}
	if err := ValidateJsonRpcResponse(batch); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateJsonRpcResponseUnknownTypeIsSkipped documents that types the
// validator does not understand pass through unchecked.
func TestValidateJsonRpcResponseUnknownTypeIsSkipped(t *testing.T) {
	for _, response := range []any{nil, "a string", 42, struct{ A int }{1}} {
		if err := ValidateJsonRpcResponse(response); err != nil {
			t.Errorf("ValidateJsonRpcResponse(%T) = %v, want nil", response, err)
		}
	}
}

func TestMarshalRequest(t *testing.T) {
	tests := []struct {
		name   string
		method string
		params []any
		want   string
	}{
		{
			name:   "no params",
			method: "eth_blockNumber",
			want:   `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`,
		},
		{
			name:   "with params",
			method: "eth_getBlockByNumber",
			params: []any{"latest", false},
			want:   `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(marshalRequest(tt.method, tt.params...))
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGetLatestBlockNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1b4"}`))
	}))
	defer server.Close()

	got, _, err := GetLatestBlockNumber(context.Background(), NewClient("http", "", 0),
		strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 436 {
		t.Errorf("got %d, want 436", got)
	}
}

func TestGetLatestBlockNumberErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"rpc error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"boom"}}`, "RPC error"},
		{"result is not a string", `{"jsonrpc":"2.0","id":1,"result":436}`, "result is not a string"},
		{"no result and no error", `{"jsonrpc":"2.0","id":1}`, "no result or error"},
		{"response is not an object", `["not","an","object"]`, "response is not a map"},
		{"malformed hex", `{"jsonrpc":"2.0","id":1,"result":"0xzz"}`, "invalid hex character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			_, _, err := GetLatestBlockNumber(context.Background(), NewClient("http", "", 0),
				strings.TrimPrefix(server.URL, "http://"))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestGetLatestBlockNumberTransportError(t *testing.T) {
	_, _, err := GetLatestBlockNumber(context.Background(), NewClient("http", "", 0), "127.0.0.1:1")
	if err == nil {
		t.Error("expected an error for an unreachable node")
	}
}

func TestCallHTTPRawReportsTransportError(t *testing.T) {
	var gotErr error
	var gotResp *http.Response
	err := CallHTTPRaw(context.Background(), 1, "http", "", "127.0.0.1:1", []byte(`{}`),
		func(resp *http.Response, callErr error, elapsed time.Duration) error {
			gotResp, gotErr = resp, callErr
			return nil
		})
	if err != nil {
		t.Fatalf("the handler's return value should be propagated verbatim, got %v", err)
	}
	if gotErr == nil {
		t.Error("the handler should receive the transport error")
	}
	if gotResp != nil {
		t.Error("no response is expected when the transport fails")
	}
}

func TestCallHTTPRawInvalidURL(t *testing.T) {
	called := false
	err := CallHTTPRaw(context.Background(), 1, "http", "", "invalid host\x7f", []byte(`{}`),
		func(*http.Response, error, time.Duration) error {
			called = true
			return nil
		})
	if err == nil {
		t.Error("expected an error building the request")
	}
	if called {
		t.Error("the handler must not run when the request cannot be built")
	}
}

func TestCallHTTPRawSetsHeaders(t *testing.T) {
	tests := []struct {
		name           string
		transport      string
		jwtAuth        string
		wantEncoding   string
		wantAuthHeader string
	}{
		{"plain http asks for no compression", "http", "", "Identity", ""},
		// With no explicit header the Go transport negotiates gzip itself.
		{"http_comp allows compression", "http_comp", "", "gzip", ""},
		{"jwt is forwarded", "http", "Bearer tok", "Identity", "Bearer tok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotEncoding, gotAuth string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotEncoding = r.Header.Get("Accept-Encoding")
				gotAuth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
			}))
			defer server.Close()

			err := CallHTTPRaw(context.Background(), 0, tt.transport, tt.jwtAuth,
				strings.TrimPrefix(server.URL, "http://"), []byte(`{"id":1}`),
				func(resp *http.Response, callErr error, elapsed time.Duration) error {
					if callErr != nil {
						return callErr
					}
					defer resp.Body.Close()
					if elapsed <= 0 {
						t.Error("elapsed time should be measured")
					}
					return nil
				})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotEncoding != tt.wantEncoding {
				t.Errorf("Accept-Encoding: got %q, want %q", gotEncoding, tt.wantEncoding)
			}
			if gotAuth != tt.wantAuthHeader {
				t.Errorf("Authorization: got %q, want %q", gotAuth, tt.wantAuthHeader)
			}
		})
	}
}
