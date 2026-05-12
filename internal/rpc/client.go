package rpc

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Metrics tracks timing statistics for a single RPC call.
type Metrics struct {
	RoundTripTime     time.Duration
	MarshallingTime   time.Duration
	UnmarshallingTime time.Duration
}

// Client dispatches JSON-RPC requests over HTTP or WebSocket transports.
type Client struct {
	verbose      int
	transport    string
	jwtAuth      string
	extraHeaders map[string]string
}

// NewClient creates a new RPC client for the given transport type.
func NewClient(transport string, jwtAuth string, verbose int) *Client {
	return &Client{
		verbose:   verbose,
		transport: transport,
		jwtAuth:   jwtAuth,
	}
}

// NewClientWithHeaders creates a new RPC client with additional HTTP headers.
func NewClientWithHeaders(transport string, jwtAuth string, verbose int, extraHeaders map[string]string) *Client {
	return &Client{
		verbose:      verbose,
		transport:    transport,
		jwtAuth:      jwtAuth,
		extraHeaders: extraHeaders,
	}
}

// Call sends a JSON-RPC request and decodes the response into the provided target.
// Returns timing metrics and any error encountered.
func (c *Client) Call(ctx context.Context, target string, request []byte, response any) (Metrics, error) {
	if strings.HasPrefix(c.transport, "http") {
		return c.callHTTP(ctx, target, request, response)
	}
	if strings.HasPrefix(c.transport, "websocket") {
		return c.callWebSocket(target, request, response)
	}
	return Metrics{}, fmt.Errorf("unsupported transport: %s", c.transport)
}
