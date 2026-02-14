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

var blockByNumberCommand = &cli.Command{
	Name:  "block-by-number",
	Usage: "Query latest/safe/finalized block numbers via WebSocket every 2s",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "url",
			Value: "ws://127.0.0.1:8545",
			Usage: "WebSocket URL of the Ethereum node",
		},
	},
	Action: runBlockByNumber,
}

type jsonRPCRequest struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

type jsonRPCResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  any    `json:"result"`
	Error   any    `json:"error"`
}

func runBlockByNumber(c *cli.Context) error {
	url := c.String("url")

	conn, err := rpc.Dial(url)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", url, err)
	}
	defer conn.Close()
	log.Printf("Successfully connected to Ethereum node at %s", url)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	delay := 2 * time.Second
	log.Printf("Query blocks started delay: %v", delay)

	ticker := time.NewTicker(delay)
	defer ticker.Stop()

	// Query immediately, then on ticker
	for {
		latest, err := getBlockNumber(conn, "latest", 1)
		if err != nil {
			return fmt.Errorf("get latest block: %w", err)
		}
		safe, err := getBlockNumber(conn, "safe", 2)
		if err != nil {
			return fmt.Errorf("get safe block: %w", err)
		}
		finalized, err := getBlockNumber(conn, "finalized", 3)
		if err != nil {
			return fmt.Errorf("get finalized block: %w", err)
		}
		log.Printf("Block latest: %s safe: %s finalized: %s", latest, safe, finalized)

		select {
		case <-sigs:
			log.Printf("Received interrupt signal")
			log.Printf("Query blocks terminated")
			return nil
		case <-ticker.C:
		}
	}
}

func getBlockNumber(conn *rpc.WSConn, tag string, id int) (string, error) {
	req := jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_getBlockByNumber",
		Params:  []any{tag, false},
		ID:      id,
	}
	var resp jsonRPCResponse
	if err := conn.CallJSON(req, &resp); err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("RPC error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected result type: %T", resp.Result)
	}
	number, ok := result["number"].(string)
	if !ok {
		return "", fmt.Errorf("missing number field in block result")
	}
	return number, nil
}
