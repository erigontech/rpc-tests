package rpc

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
)

var jsonAPI = jsoniter.ConfigCompatibleWithStandardLibrary

// sharedTransport is a single http.Transport shared across all goroutines.
// One transport = one connection pool = maximum TCP reuse across all workers.
var sharedTransport = &http.Transport{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     90 * time.Second,
}

// sharedHTTPClient is a goroutine-safe http.Client using the shared transport.
var sharedHTTPClient = &http.Client{
	Timeout:   300 * time.Second,
	Transport: sharedTransport,
}

// bufPool reuses bytes.Buffer instances for request bodies.
var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func (c *Client) callHTTP(ctx context.Context, target string, request []byte, response any) (Metrics, error) {
	var metrics Metrics

	protocol := "http://"
	if c.transport == "https" {
		protocol = "https://"
	}
	url := protocol + target

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.Write(request)
	defer bufPool.Put(buf)

	req, err := http.NewRequestWithContext(ctx, "POST", url, buf)
	if err != nil {
		if c.verbose > 0 {
			fmt.Printf("\nhttp request creation fail: %s %v\n", url, err)
		}
		return metrics, err
	}

	req.Header.Set("Content-Type", "application/json")
	if !strings.HasSuffix(c.transport, "_comp") {
		req.Header.Set("Accept-Encoding", "Identity")
	}
	if c.jwtAuth != "" {
		req.Header.Set("Authorization", c.jwtAuth)
	}

	start := time.Now()
	resp, err := sharedHTTPClient.Do(req)
	metrics.RoundTripTime = time.Since(start)

	if c.verbose > 1 {
		fmt.Printf("http round-trip time: %v\n", metrics.RoundTripTime)
	}

	if err != nil {
		if c.verbose > 0 {
			fmt.Printf("\nhttp connection fail: %s %v\n", target, err)
		}
		return metrics, err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			fmt.Printf("\nfailed to close response body: %v\n", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		if c.verbose > 1 {
			fmt.Printf("\npost result status_code: %d\n", resp.StatusCode)
		}
		return metrics, fmt.Errorf("http status %v", resp.Status)
	}

	unmarshalStart := time.Now()
	if err = jsonAPI.NewDecoder(resp.Body).Decode(response); err != nil {
		return metrics, fmt.Errorf("cannot decode http body as json %w", err)
	}
	metrics.UnmarshallingTime = time.Since(unmarshalStart)

	if c.verbose > 1 {
		raw, _ := jsonAPI.Marshal(response)
		fmt.Printf("Node: %s\nRequest: %s\nResponse: %v\n", target, request, string(raw))
	}

	return metrics, nil
}

// CallHTTPRaw sends a raw HTTP POST and invokes the provided handler with the response.
// This matches the v1 rpc.HttpPost signature for backward compatibility.
func CallHTTPRaw(ctx context.Context, verbose int, transport, jwtAuth, target string, request []byte, handler func(*http.Response, error, time.Duration) error) error {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if transport != "http_comp" {
		headers["Accept-Encoding"] = "Identity"
	}
	if jwtAuth != "" {
		headers["Authorization"] = jwtAuth
	}

	protocol := "http://"
	if transport == "https" {
		protocol = "https://"
	}
	url := protocol + target

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(request))
	if err != nil {
		if verbose > 0 {
			fmt.Printf("\nhttp request creation fail: %s %v\n", url, err)
		}
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := sharedHTTPClient.Do(req)
	elapsed := time.Since(start)

	return handler(resp, err, elapsed)
}

// ValidateJsonRpcResponse checks that a response is valid JSON-RPC 2.0.
func ValidateJsonRpcResponse(response any) error {
	switch r := response.(type) {
	case map[string]any:
		return validateJsonRpcResponseObject(r)
	case *map[string]any:
		if r != nil {
			return validateJsonRpcResponseObject(*r)
		}
		return fmt.Errorf("nil response pointer")
	default:
		// Try to handle []any (batch response)
		if arr, ok := response.([]any); ok {
			for _, elem := range arr {
				if m, ok := elem.(map[string]any); ok {
					if err := validateJsonRpcResponseObject(m); err != nil {
						return err
					}
				}
			}
			return nil
		}
		// Use io.ReadCloser or other types - just skip validation
		return nil
	}
}

func validateJsonRpcResponseObject(obj map[string]any) error {
	jsonrpc, ok := obj["jsonrpc"]
	if !ok {
		return fmt.Errorf("invalid JSON-RPC response: missing 'jsonrpc' field")
	}
	if version, ok := jsonrpc.(string); !ok || version != "2.0" {
		return fmt.Errorf("noncompliant JSON-RPC 2.0 version")
	}
	if _, ok := obj["id"]; !ok {
		return fmt.Errorf("invalid JSON-RPC response: missing 'id' field")
	}
	return nil
}

// GetLatestBlockNumber queries eth_blockNumber and returns the result as uint64.
func GetLatestBlockNumber(ctx context.Context, client *Client, url string) (uint64, Metrics, error) {
	type rpcReq struct {
		Jsonrpc string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
		Id      int    `json:"id"`
	}

	reqBytes, _ := jsonAPI.Marshal(rpcReq{
		Jsonrpc: "2.0",
		Method:  "eth_blockNumber",
		Params:  []any{},
		Id:      1,
	})

	var response any
	metrics, err := client.Call(ctx, url, reqBytes, &response)
	if err != nil {
		return 0, metrics, err
	}

	responseMap, ok := response.(map[string]any)
	if !ok {
		return 0, metrics, fmt.Errorf("response is not a map: %v", response)
	}
	if resultVal, hasResult := responseMap["result"]; hasResult {
		resultStr, isString := resultVal.(string)
		if !isString {
			return 0, metrics, fmt.Errorf("result is not a string: %v", resultVal)
		}
		cleanHex := strings.TrimPrefix(resultStr, "0x")
		val, err := parseHexUint64(cleanHex)
		return val, metrics, err
	}
	if errorVal, hasError := responseMap["error"]; hasError {
		return 0, metrics, fmt.Errorf("RPC error: %v", errorVal)
	}
	return 0, metrics, fmt.Errorf("no result or error found in response")
}

func parseHexUint64(s string) (uint64, error) {
	var result uint64
	for _, c := range s {
		result <<= 4
		switch {
		case c >= '0' && c <= '9':
			result |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			result |= uint64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			result |= uint64(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex character: %c", c)
		}
	}
	return result, nil
}

// GetConsistentLatestBlock retries until both servers agree on the latest block.
func GetConsistentLatestBlock(verbose int, server1URL, server2URL string, maxRetries int, retryDelay time.Duration) (uint64, error) {
	client := NewClient("http", "", verbose)
	var bn1, bn2 uint64

	for i := range maxRetries {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		var err1, err2 error
		bn1, _, err1 = GetLatestBlockNumber(ctx, client, server1URL)
		bn2, _, err2 = GetLatestBlockNumber(ctx, client, server2URL)
		cancel()

		if verbose > 1 {
			fmt.Printf("retry: %d nodes: %s, %s latest blocks: %d, %d\n", i+1, server1URL, server2URL, bn1, bn2)
		}

		if err1 == nil && err2 == nil && bn1 == bn2 {
			return bn1, nil
		}

		if i < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}

	return 0, fmt.Errorf("nodes not synced, last values: %d / %d", bn1, bn2)
}
