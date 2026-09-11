package tools

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/urfave/cli/v2"
)

// TestMain silences the log output the subcommands write to the standard
// logger, which would otherwise bury the test results.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// rpcRequest is a decoded JSON-RPC request as seen by the fake nodes below.
type rpcRequest struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      any    `json:"id"`
}

// fakeNode is an in-process HTTP JSON-RPC server. Handlers return the value to
// put in "result"; returning an error object instead puts it in "error".
type fakeNode struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []rpcRequest
}

type rpcErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// newFakeNode starts an HTTP JSON-RPC server dispatching on the method name.
func newFakeNode(t *testing.T, handlers map[string]func(params []any) (any, *rpcErrorObject)) *fakeNode {
	t.Helper()
	node := &fakeNode{t: t}
	node.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		node.mu.Lock()
		node.requests = append(node.requests, req)
		node.mu.Unlock()

		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		handler, ok := handlers[req.Method]
		if !ok {
			resp["error"] = rpcErrorObject{Code: -32601, Message: "method not found: " + req.Method}
		} else if result, rpcErr := handler(req.Params); rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(node.server.Close)
	return node
}

// target strips the scheme, matching what rpc.Client expects.
func (n *fakeNode) target() string {
	return strings.TrimPrefix(n.server.URL, "http://")
}

func (n *fakeNode) url() string {
	return n.server.URL
}

func (n *fakeNode) callsTo(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, req := range n.requests {
		if req.Method == method {
			count++
		}
	}
	return count
}

var wsUpgrader = websocket.Upgrader{}

// newFakeWSNode starts a WebSocket server that runs handle on each accepted
// connection and returns its ws:// URL.
func newFakeWSNode(t *testing.T, handle func(conn *websocket.Conn)) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handle(conn)
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	u.Scheme = "ws"
	return u.String()
}

// serveWSRequests answers JSON-RPC requests on conn until it is closed.
func serveWSRequests(conn *websocket.Conn, handlers map[string]func(params []any) (any, *rpcErrorObject)) {
	for {
		var req rpcRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		handler, ok := handlers[req.Method]
		if !ok {
			resp["error"] = rpcErrorObject{Code: -32601, Message: "method not found: " + req.Method}
		} else if result, rpcErr := handler(req.Params); rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		if err := conn.WriteJSON(resp); err != nil {
			return
		}
	}
}

// runSubcommand executes one of the registered subcommands through a urfave/cli
// app, so flag parsing and defaults are exercised alongside the action.
func runSubcommand(args ...string) error {
	app := &cli.App{
		Name:           "rpc_int",
		Commands:       Commands(),
		Writer:         io.Discard,
		ErrWriter:      io.Discard,
		ExitErrHandler: func(*cli.Context, error) {},
	}
	return app.Run(append([]string{"rpc_int"}, args...))
}

// unusedPort returns an address that nothing is listening on, for dial-failure
// tests. Port 1 is privileged and never bound by the test harness.
const unusedPort = "127.0.0.1:1"
