package rpc

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func (c *Client) callWebSocket(target string, request []byte, response any) (Metrics, error) {
	var metrics Metrics

	wsTarget := "ws://" + target
	dialer := websocket.Dialer{
		HandshakeTimeout:  300 * time.Second,
		EnableCompression: strings.HasSuffix(c.transport, "_comp"),
	}

	headers := http.Header{}
	if c.jwtAuth != "" {
		headers.Set("Authorization", c.jwtAuth)
	}

	conn, _, err := dialer.Dial(wsTarget, headers)
	if err != nil {
		if c.verbose > 0 {
			fmt.Printf("\nwebsocket connection fail: %v\n", err)
		}
		return metrics, err
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			fmt.Printf("\nfailed to close websocket connection: %v\n", cerr)
		}
	}()

	start := time.Now()
	if err = conn.WriteMessage(websocket.BinaryMessage, request); err != nil {
		if c.verbose > 0 {
			fmt.Printf("\nwebsocket write fail: %v\n", err)
		}
		return metrics, err
	}

	_, message, err := conn.NextReader()
	if err != nil {
		if c.verbose > 0 {
			fmt.Printf("\nwebsocket read fail: %v\n", err)
		}
		return metrics, err
	}
	metrics.RoundTripTime = time.Since(start)

	unmarshalStart := time.Now()
	if err = jsonAPI.NewDecoder(message).Decode(response); err != nil {
		return metrics, fmt.Errorf("cannot decode websocket message as json %w", err)
	}
	metrics.UnmarshallingTime = time.Since(unmarshalStart)

	if c.verbose > 1 {
		raw, _ := jsonAPI.Marshal(response)
		fmt.Printf("Node: %s\nRequest: %s\nResponse: %v\n", target, request, string(raw))
	}

	return metrics, nil
}
