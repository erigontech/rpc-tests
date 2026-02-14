package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/urfave/cli/v2"
)

// USDT contract address on mainnet
const usdtAddress = "0xdac17f958d2ee523a2206206994597c13d831ec7"

// ERC20 Transfer event topic (with final 'f')
const transferTopicFull = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

var subscriptionsCommand = &cli.Command{
	Name:  "subscriptions",
	Usage: "Subscribe to newHeads and USDT Transfer logs via WebSocket",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "url",
			Value: "ws://127.0.0.1:8545",
			Usage: "WebSocket URL of the Ethereum node",
		},
	},
	Action: runSubscriptions,
}

type subscriptionNotification struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		Subscription string          `json:"subscription"`
		Result       json.RawMessage `json:"result"`
	} `json:"params"`
	// For subscribe responses
	ID     *int   `json:"id,omitempty"`
	Result string `json:"result,omitempty"`
}

func runSubscriptions(c *cli.Context) error {
	url := c.String("url")

	conn, err := rpc.Dial(url)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", url, err)
	}
	defer conn.Close()
	log.Printf("Successfully connected to Ethereum node at %s", url)

	// Subscribe to newHeads
	var newHeadsResp jsonRPCResponse
	err = conn.CallJSON(jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_subscribe",
		Params:  []any{"newHeads"},
		ID:      1,
	}, &newHeadsResp)
	if err != nil {
		return fmt.Errorf("subscribe newHeads: %w", err)
	}
	if newHeadsResp.Error != nil {
		return fmt.Errorf("subscribe newHeads RPC error: %v", newHeadsResp.Error)
	}
	newHeadsSubID, _ := newHeadsResp.Result.(string)
	log.Printf("Subscribed to newHeads: %s", newHeadsSubID)

	// Subscribe to USDT Transfer logs
	var logsResp jsonRPCResponse
	err = conn.CallJSON(jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_subscribe",
		Params: []any{"logs", map[string]any{
			"address": usdtAddress,
			"topics":  []string{transferTopicFull},
		}},
		ID: 2,
	}, &logsResp)
	if err != nil {
		return fmt.Errorf("subscribe logs: %w", err)
	}
	if logsResp.Error != nil {
		return fmt.Errorf("subscribe logs RPC error: %v", logsResp.Error)
	}
	logsSubID, _ := logsResp.Result.(string)
	log.Printf("Subscribed to USDT logs: %s", logsSubID)

	log.Printf("Handle subscriptions started: 2")

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		<-sigs
		log.Printf("Received interrupt signal")
		// Signal done first, then close connection to break the read loop
		close(done)
		conn.Close()
	}()

	// Listen for incoming subscription events
	for {
		var notification subscriptionNotification
		if err := conn.RecvJSON(&notification); err != nil {
			select {
			case <-done:
				log.Printf("Handle subscriptions terminated")
				return nil
			default:
				return fmt.Errorf("receive notification: %w", err)
			}
		}

		switch notification.Params.Subscription {
		case newHeadsSubID:
			fmt.Printf("New block header: %s\n\n", notification.Params.Result)
		case logsSubID:
			fmt.Printf("Log receipt: %s\n\n", notification.Params.Result)
		}
	}
}
