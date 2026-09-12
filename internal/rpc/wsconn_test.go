package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var testUpgrader = websocket.Upgrader{EnableCompression: true}

// wsTestServer starts a WebSocket endpoint running handle on each connection
// and returns its ws:// URL plus the bare host:port used by rpc.Client.
func wsTestServer(t *testing.T, handle func(*websocket.Conn)) (wsURL, target string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
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
	target = u.Host
	u.Scheme = "ws"
	return u.String(), target
}

// echoResult answers every request with {"result": value}.
func echoResult(value any) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		for {
			var req map[string]any
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			if err := conn.WriteJSON(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  value,
			}); err != nil {
				return
			}
		}
	}
}

func TestDialAndClose(t *testing.T) {
	wsURL, _ := wsTestServer(t, echoResult("0x1"))

	conn, err := Dial(wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestDialFailure(t *testing.T) {
	_, err := Dial("ws://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected an error dialling a closed port")
	}
	if !strings.Contains(err.Error(), "websocket dial") {
		t.Errorf("error %q should be wrapped as a dial failure", err)
	}
}

func TestDialRejectsNonWebSocketEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := Dial("ws://" + strings.TrimPrefix(server.URL, "http://")); err == nil {
		t.Error("expected an error when the server does not upgrade the connection")
	}
}

func TestCallJSON(t *testing.T) {
	wsURL, _ := wsTestServer(t, echoResult(map[string]any{"number": "0x2a"}))

	conn, err := Dial(wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	var resp struct {
		ID     int            `json:"id"`
		Result map[string]any `json:"result"`
	}
	req := map[string]any{"jsonrpc": "2.0", "method": "eth_blockNumber", "id": 7}
	if err := conn.CallJSON(req, &resp); err != nil {
		t.Fatalf("CallJSON: %v", err)
	}
	if resp.ID != 7 {
		t.Errorf("id: got %d, want 7", resp.ID)
	}
	if resp.Result["number"] != "0x2a" {
		t.Errorf("result: got %v, want 0x2a", resp.Result["number"])
	}
}

func TestSendAndRecvJSON(t *testing.T) {
	wsURL, _ := wsTestServer(t, echoResult("0x1"))

	conn, err := Dial(wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if err := conn.SendJSON(map[string]any{"id": 1, "method": "eth_blockNumber"}); err != nil {
		t.Fatalf("SendJSON: %v", err)
	}
	var resp map[string]any
	if err := conn.RecvJSON(&resp); err != nil {
		t.Fatalf("RecvJSON: %v", err)
	}
	if resp["result"] != "0x1" {
		t.Errorf("result: got %v, want 0x1", resp["result"])
	}
}

func TestCallJSONOnClosedConnection(t *testing.T) {
	wsURL, _ := wsTestServer(t, echoResult("0x1"))

	conn, err := Dial(wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var resp map[string]any
	err = conn.CallJSON(map[string]any{"id": 1}, &resp)
	if err == nil {
		t.Fatal("expected an error writing to a closed connection")
	}
	if !strings.Contains(err.Error(), "send:") {
		t.Errorf("error %q should be tagged as a send failure", err)
	}
}

func TestCallJSONRecvFailure(t *testing.T) {
	// The server accepts the request then hangs up without replying.
	wsURL, _ := wsTestServer(t, func(conn *websocket.Conn) {
		var req map[string]any
		_ = conn.ReadJSON(&req)
	})

	conn, err := Dial(wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	var resp map[string]any
	err = conn.CallJSON(map[string]any{"id": 1}, &resp)
	if err == nil {
		t.Fatal("expected an error when no reply arrives")
	}
	if !strings.Contains(err.Error(), "recv:") {
		t.Errorf("error %q should be tagged as a receive failure", err)
	}
}

// TestSendJSONIsSerialised exercises the mutex guarding concurrent writers:
// gorilla panics on concurrent writes to the same connection.
func TestSendJSONIsSerialised(t *testing.T) {
	wsURL, _ := wsTestServer(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	conn, err := Dial(wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	const writers = 8
	errs := make(chan error, writers)
	for i := range writers {
		go func() {
			errs <- conn.SendJSON(map[string]any{"id": i, "method": "eth_blockNumber"})
		}()
	}
	for range writers {
		if err := <-errs; err != nil {
			t.Errorf("concurrent SendJSON: %v", err)
		}
	}
}

func TestCloseAfterPeerHangUp(t *testing.T) {
	wsURL, _ := wsTestServer(t, func(conn *websocket.Conn) {
		conn.Close()
	})

	conn, err := Dial(wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	// Give the peer a moment to drop the connection, then confirm Close still
	// releases the local socket instead of blocking or panicking.
	var resp map[string]any
	_ = conn.RecvJSON(&resp)
	_ = conn.Close()
}

func TestCallWebSocket(t *testing.T) {
	_, target := wsTestServer(t, echoResult("0x10"))

	client := NewClient("websocket", "", 0)
	var resp map[string]any
	metrics, err := client.Call(context.Background(), target,
		[]byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","id":1}`), &resp)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp["result"] != "0x10" {
		t.Errorf("result: got %v, want 0x10", resp["result"])
	}
	if metrics.RoundTripTime <= 0 {
		t.Error("RoundTripTime should be measured")
	}
	if metrics.UnmarshallingTime <= 0 {
		t.Error("UnmarshallingTime should be measured")
	}
}

func TestCallWebSocketCompressed(t *testing.T) {
	_, target := wsTestServer(t, echoResult("0x20"))

	client := NewClient("websocket_comp", "", 0)
	var resp map[string]any
	if _, err := client.Call(context.Background(), target,
		[]byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","id":1}`), &resp); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp["result"] != "0x20" {
		t.Errorf("result: got %v, want 0x20", resp["result"])
	}
}

func TestCallWebSocketSendsJWTHeader(t *testing.T) {
	gotAuth := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case gotAuth <- r.Header.Get("Authorization"):
		default:
		}
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		echoResult("0x1")(conn)
	}))
	defer server.Close()

	client := NewClient("websocket", "Bearer test-token", 0)
	var resp map[string]any
	if _, err := client.Call(context.Background(), strings.TrimPrefix(server.URL, "http://"),
		[]byte(`{"id":1}`), &resp); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := <-gotAuth; got != "Bearer test-token" {
		t.Errorf("Authorization: got %q, want %q", got, "Bearer test-token")
	}
}

func TestCallWebSocketDialFailure(t *testing.T) {
	client := NewClient("websocket", "", 1)
	var resp map[string]any
	if _, err := client.Call(context.Background(), "127.0.0.1:1", []byte(`{"id":1}`), &resp); err == nil {
		t.Error("expected an error dialling a closed port")
	}
}

func TestCallWebSocketReadFailure(t *testing.T) {
	// The server closes without answering, so the read fails.
	_, target := wsTestServer(t, func(conn *websocket.Conn) {
		var req map[string]any
		_ = conn.ReadJSON(&req)
	})

	client := NewClient("websocket", "", 1)
	var resp map[string]any
	if _, err := client.Call(context.Background(), target, []byte(`{"id":1}`), &resp); err == nil {
		t.Error("expected an error when the peer hangs up before replying")
	}
}

func TestCallWebSocketMalformedResponse(t *testing.T) {
	_, target := wsTestServer(t, func(conn *websocket.Conn) {
		var req map[string]any
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte("this is not json"))
	})

	client := NewClient("websocket", "", 2)
	var resp map[string]any
	_, err := client.Call(context.Background(), target, []byte(`{"id":1}`), &resp)
	if err == nil {
		t.Fatal("expected a decode error")
	}
	if !strings.Contains(err.Error(), "cannot decode websocket message as json") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestCallWebSocketVerboseOutput drives the verbose logging branches so a nil
// dereference or format-verb mistake there cannot go unnoticed.
func TestCallWebSocketVerboseOutput(t *testing.T) {
	_, target := wsTestServer(t, echoResult("0x1"))

	for _, verbose := range []int{0, 1, 2} {
		client := NewClient("websocket", "", verbose)
		var resp map[string]any
		if _, err := client.Call(context.Background(), target, []byte(`{"id":1}`), &resp); err != nil {
			t.Errorf("verbose=%d: %v", verbose, err)
		}
	}
}

func TestCallUnsupportedTransportName(t *testing.T) {
	client := NewClient("carrier-pigeon", "", 0)
	var resp map[string]any
	_, err := client.Call(context.Background(), "127.0.0.1:1", []byte(`{}`), &resp)
	if err == nil || !strings.Contains(err.Error(), "unsupported transport") {
		t.Errorf("got %v, want an unsupported-transport error", err)
	}
}

// TestWSConnReadDeadlineIsGenerous guards against a regression that would make
// long-running engine calls time out: callWebSocket sets a 300s read deadline.
func TestWSConnReadDeadlineIsGenerous(t *testing.T) {
	_, target := wsTestServer(t, func(conn *websocket.Conn) {
		var req map[string]any
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
		_ = conn.WriteJSON(map[string]any{"id": req["id"], "result": "0x1"})
	})

	client := NewClient("websocket", "", 0)
	var resp map[string]any
	if _, err := client.Call(context.Background(), target, []byte(`{"id":1}`), &resp); err != nil {
		t.Errorf("a slow reply should not time out: %v", err)
	}
}
