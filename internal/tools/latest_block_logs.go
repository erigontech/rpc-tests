package tools

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/urfave/cli/v2"
)

const emptyTrieRoot = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"

var latestBlockLogsCommand = &cli.Command{
	Name:  "latest-block-logs",
	Usage: "Monitor latest block and validate getLogs vs receiptsRoot",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "url",
			Value: "http://127.0.0.1:8545",
			Usage: "HTTP URL of the Ethereum node",
		},
		&cli.Float64Flag{
			Name:  "interval",
			Value: 0.1,
			Usage: "Sleep interval between queries in seconds",
		},
	},
	Action: runLatestBlockLogs,
}

func runLatestBlockLogs(c *cli.Context) error {
	url := c.String("url")
	interval := time.Duration(c.Float64("interval") * float64(time.Second))

	client := rpc.NewClient("http", "", 0)
	target := strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Printf("Received interrupt signal. Shutting down...")
		cancel()
	}()

	log.Printf("Query latest block logs started... Press Ctrl+C to stop.")

	var currentBlockNumber string
	for {
		if ctx.Err() != nil {
			break
		}

		// Get latest block
		block, err := getBlock(ctx, client, target, "latest")
		if err != nil {
			log.Printf("Error: get_block failed: %v", err)
			select {
			case <-ctx.Done():
			case <-time.After(interval):
			}
			continue
		}

		blockNumber, _ := block["number"].(string)
		if blockNumber == currentBlockNumber {
			select {
			case <-ctx.Done():
			case <-time.After(interval):
			}
			continue
		}

		log.Printf("Latest block is %s", blockNumber)
		currentBlockNumber = blockNumber
		blockHash, _ := block["hash"].(string)
		receiptsRoot, _ := block["receiptsRoot"].(string)

		// Call eth_getLogs with block hash
		logs, err := getLogs(ctx, client, target, blockHash)
		if err != nil {
			log.Printf("Error: get_logs for block %s failed: %v", blockNumber, err)
			select {
			case <-ctx.Done():
			case <-time.After(interval):
			}
			continue
		}

		if len(logs) > 0 {
			log.Printf("Block %s: eth_getLogs returned %d log(s).", blockNumber, len(logs))
		} else if receiptsRoot != emptyTrieRoot {
			log.Printf("Block %s: eth_getLogs returned 0 logs and receiptsRoot is non-empty...", blockNumber)

			// Wait half block time to be sure latest block got executed
			select {
			case <-ctx.Done():
				break
			case <-time.After(6 * time.Second):
			}

			// Fetch receipts and count logs
			receipts, err := getBlockReceipts(ctx, client, target, blockNumber)
			if err != nil {
				log.Printf("Error: get_block_receipts for block %s failed: %v", blockNumber, err)
				continue
			}

			numLogs := countReceiptLogs(receipts)
			if numLogs > 0 {
				log.Printf("Warning: Block %s: eth_getLogs returned 0 logs but there are %d", blockNumber, numLogs)
				break
			}
		}
	}

	log.Printf("Query latest block logs terminated.")
	return nil
}

func getBlock(ctx context.Context, client *rpc.Client, target, tag string) (map[string]any, error) {
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["%s",false],"id":1}`, tag)
	var resp map[string]any
	_, err := client.Call(ctx, target, []byte(req), &resp)
	if err != nil {
		return nil, err
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected result type")
	}
	return result, nil
}

func getLogs(ctx context.Context, client *rpc.Client, target, blockHash string) ([]any, error) {
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getLogs","params":[{"blockHash":"%s"}],"id":1}`, blockHash)
	var resp map[string]any
	_, err := client.Call(ctx, target, []byte(req), &resp)
	if err != nil {
		return nil, err
	}
	if errVal, ok := resp["error"]; ok {
		return nil, fmt.Errorf("RPC error: %v", errVal)
	}
	result, ok := resp["result"].([]any)
	if !ok {
		return nil, nil
	}
	return result, nil
}

func getBlockReceipts(ctx context.Context, client *rpc.Client, target, blockNumber string) ([]any, error) {
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockReceipts","params":["%s"],"id":1}`, blockNumber)
	var resp map[string]any
	_, err := client.Call(ctx, target, []byte(req), &resp)
	if err != nil {
		return nil, err
	}
	if errVal, ok := resp["error"]; ok {
		return nil, fmt.Errorf("RPC error: %v", errVal)
	}
	result, ok := resp["result"].([]any)
	if !ok {
		return nil, nil
	}
	return result, nil
}

func countReceiptLogs(receipts []any) int {
	count := 0
	for _, r := range receipts {
		receipt, ok := r.(map[string]any)
		if !ok {
			continue
		}
		logs, ok := receipt["logs"].([]any)
		if ok {
			count += len(logs)
		}
	}
	return count
}
