package rpc

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSConn wraps a gorilla/websocket.Conn for persistent JSON-RPC communication.
type WSConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// Dial establishes a persistent WebSocket connection to the given URL.
func Dial(url string) (*WSConn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
	}
	conn, _, err := dialer.Dial(url, http.Header{})
	if err != nil {
		return nil, fmt.Errorf("websocket dial %s: %w", url, err)
	}
	return &WSConn{conn: conn}, nil
}

// SendJSON writes a JSON-RPC request to the WebSocket connection.
func (w *WSConn) SendJSON(request any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(request)
}

// RecvJSON reads a JSON-RPC response from the WebSocket connection.
func (w *WSConn) RecvJSON(response any) error {
	return w.conn.ReadJSON(response)
}

// CallJSON sends a JSON-RPC request and reads the response.
func (w *WSConn) CallJSON(request any, response any) error {
	if err := w.SendJSON(request); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	if err := w.RecvJSON(response); err != nil {
		return fmt.Errorf("recv: %w", err)
	}
	return nil
}

// Close gracefully closes the WebSocket connection.
func (w *WSConn) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	if err != nil {
		_ = w.conn.Close()
		return err
	}
	return w.conn.Close()
}
