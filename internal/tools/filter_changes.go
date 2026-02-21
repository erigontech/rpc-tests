package tools

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/urfave/cli/v2"
)

// ERC20 Transfer event topic
const transferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3e0"

var filterChangesCommand = &cli.Command{
	Name:  "filter-changes",
	Usage: "Create ERC20 Transfer filter and poll changes/logs via WebSocket",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "url",
			Value: "ws://127.0.0.1:8545",
			Usage: "WebSocket URL of the Ethereum node",
		},
	},
	Action: runFilterChanges,
}

func runFilterChanges(c *cli.Context) error {
	url := c.String("url")

	conn, err := rpc.Dial(url)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", url, err)
	}
	defer conn.Close()
	log.Printf("Successfully connected to Ethereum node at %s", url)

	// Create filter with Transfer topic
	var filterResp jsonRPCResponse
	err = conn.CallJSON(jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_newFilter",
		Params:  []any{map[string]any{"topics": []string{transferTopic}}},
		ID:      1,
	}, &filterResp)
	if err != nil {
		return fmt.Errorf("create filter: %w", err)
	}
	if filterResp.Error != nil {
		return fmt.Errorf("create filter RPC error: %v", filterResp.Error)
	}
	filterID, ok := filterResp.Result.(string)
	if !ok {
		return fmt.Errorf("unexpected filter ID type: %T", filterResp.Result)
	}
	log.Printf("State change filter registered: %s", filterID)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	delay := 2 * time.Second
	ticker := time.NewTicker(delay)
	defer ticker.Stop()

	for {
		// Get filter changes
		var changesResp jsonRPCResponse
		err = conn.CallJSON(jsonRPCRequest{
			Jsonrpc: "2.0",
			Method:  "eth_getFilterChanges",
			Params:  []any{filterID},
			ID:      2,
		}, &changesResp)
		if err != nil {
			log.Printf("Error getting filter changes: %v", err)
		} else if changes, ok := changesResp.Result.([]any); ok && len(changes) > 0 {
			log.Printf("Changes: %v", changes)
		} else {
			log.Printf("No change received")
		}

		// Get filter logs
		var logsResp jsonRPCResponse
		err = conn.CallJSON(jsonRPCRequest{
			Jsonrpc: "2.0",
			Method:  "eth_getFilterLogs",
			Params:  []any{filterID},
			ID:      3,
		}, &logsResp)
		if err != nil {
			log.Printf("Error getting filter logs: %v", err)
		} else if logs, ok := logsResp.Result.([]any); ok && len(logs) > 0 {
			log.Printf("Logs: %v", logs)
		} else {
			log.Printf("No log received")
		}

		select {
		case <-sigs:
			log.Printf("Received interrupt signal")
			// Uninstall filter
			var uninstallResp jsonRPCResponse
			_ = conn.CallJSON(jsonRPCRequest{
				Jsonrpc: "2.0",
				Method:  "eth_uninstallFilter",
				Params:  []any{filterID},
				ID:      4,
			}, &uninstallResp)
			log.Printf("State change filter unregistered")
			return nil
		case <-ticker.C:
		}
	}
}
