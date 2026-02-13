package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkCallHTTP(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient("http", "", 0)
	request := []byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result interface{}
		client.Call(ctx, server.URL, request, &result)
	}
}

func BenchmarkValidateJsonRpcResponse(b *testing.B) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result":  "0x1",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateJsonRpcResponse(response)
	}
}
