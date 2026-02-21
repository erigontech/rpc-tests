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

	"github.com/erigontech/rpc-tests/internal/eth"
	"github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/urfave/cli/v2"
)

var scanBlockReceiptsCommand = &cli.Command{
	Name:  "scan-block-receipts",
	Usage: "Verify receipts root via MPT trie for block ranges or latest blocks",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "url",
			Value: "http://127.0.0.1:8545",
			Usage: "HTTP URL of the Ethereum node",
		},
		&cli.Int64Flag{
			Name:  "start-block",
			Value: -1,
			Usage: "Starting block number (inclusive)",
		},
		&cli.Int64Flag{
			Name:  "end-block",
			Value: -1,
			Usage: "Ending block number (inclusive)",
		},
		&cli.BoolFlag{
			Name:  "beyond-latest",
			Usage: "Scan next-after-latest blocks",
		},
		&cli.BoolFlag{
			Name:  "stop-at-reorg",
			Usage: "Stop at first chain reorg",
		},
		&cli.Float64Flag{
			Name:  "interval",
			Value: 0.1,
			Usage: "Sleep interval between queries in seconds",
		},
	},
	Action: runScanBlockReceipts,
}

func runScanBlockReceipts(c *cli.Context) error {
	url := c.String("url")
	startBlock := c.Int64("start-block")
	endBlock := c.Int64("end-block")
	beyondLatest := c.Bool("beyond-latest")
	stopAtReorg := c.Bool("stop-at-reorg")
	interval := time.Duration(c.Float64("interval") * float64(time.Second))

	isRangeMode := startBlock >= 0 && endBlock >= 0
	isLatestMode := startBlock < 0 && endBlock < 0

	if !isRangeMode && !isLatestMode {
		return fmt.Errorf("you must specify --start-block AND --end-block, or neither")
	}
	if isRangeMode && endBlock < startBlock {
		return fmt.Errorf("end block %d must be >= start block %d", endBlock, startBlock)
	}

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

	if isRangeMode {
		return scanReceiptsRange(ctx, client, target, startBlock, endBlock)
	}
	if beyondLatest {
		return scanReceiptsBeyondLatest(ctx, client, target, interval, stopAtReorg)
	}
	return scanReceiptsLatest(ctx, client, target, interval, stopAtReorg)
}

func scanReceiptsRange(ctx context.Context, client *rpc.Client, target string, start, end int64) error {
	log.Printf("Scanning block receipts from %d to %d...", start, end)

	for blockNum := start; blockNum <= end; blockNum++ {
		if ctx.Err() != nil {
			log.Printf("Scan terminated by user.")
			return nil //nolint:nilerr // graceful shutdown on signal
		}

		if err := verifyReceiptsRoot(ctx, client, target, blockNum); err != nil {
			return err
		}
	}

	log.Printf("Successfully scanned and verified all receipts from %d to %d.", start, end)
	return nil
}

func scanReceiptsLatest(ctx context.Context, client *rpc.Client, target string, interval time.Duration, stopAtReorg bool) error {
	log.Printf("Scanning latest blocks... Press Ctrl+C to stop.")

	var currentBlockNumber int64
	var previousBlockHash string

	for ctx.Err() == nil {
		block, err := getFullBlock(ctx, client, target, "latest")
		if err != nil {
			log.Printf("Error: %v", err)
			sleepCtx(ctx, 1*time.Second)
			continue
		}

		blockNum := hexToInt64(block["number"])
		if blockNum == currentBlockNumber {
			sleepCtx(ctx, interval)
			continue
		}

		if currentBlockNumber > 0 && blockNum != currentBlockNumber+1 {
			log.Printf("Warning: gap detected at block %d, node still syncing...", blockNum)
		}

		// Check for reorg
		reorgDetected := false
		if previousBlockHash != "" && blockNum == currentBlockNumber+1 {
			parentHash, _ := block["parentHash"].(string)
			if parentHash != previousBlockHash {
				log.Printf("Warning: REORG DETECTED at block %d", currentBlockNumber)
				log.Printf("Expected parentHash: %s", previousBlockHash)
				log.Printf("Actual parentHash: %s", parentHash)
				reorgDetected = true
			}
		}

		currentBlockNumber = blockNum
		previousBlockHash, _ = block["hash"].(string)

		if err := verifyBlockReceipts(ctx, client, target, block, reorgDetected); err != nil {
			return err
		}

		if reorgDetected && stopAtReorg {
			log.Printf("Stopping scan due to reorg detection (receipts were checked).")
			return nil
		}
	}

	return nil
}

func scanReceiptsBeyondLatest(ctx context.Context, client *rpc.Client, target string, interval time.Duration, stopAtReorg bool) error {
	log.Printf("Scanning next-after-latest blocks... Press Ctrl+C to stop.")

	var currentBlockNumber int64
	var previousBlockHash string

	for ctx.Err() == nil {
		block, err := getFullBlock(ctx, client, target, "latest")
		if err != nil {
			log.Printf("Error: %v", err)
			sleepCtx(ctx, 1*time.Second)
			continue
		}

		blockNum := hexToInt64(block["number"])
		if blockNum == currentBlockNumber {
			sleepCtx(ctx, interval)
			continue
		}

		// Check for gap and reorg
		gapDetected := false
		reorgDetected := false
		if currentBlockNumber > 0 && blockNum != currentBlockNumber+1 {
			log.Printf("Warning: gap detected at block %d, node still syncing...", blockNum)
			gapDetected = true
		}
		if previousBlockHash != "" && blockNum == currentBlockNumber+1 {
			parentHash, _ := block["parentHash"].(string)
			if parentHash != previousBlockHash {
				log.Printf("Warning: REORG DETECTED at block %d", currentBlockNumber)
				reorgDetected = true
			}
		}

		currentBlockNumber = blockNum
		previousBlockHash, _ = block["hash"].(string)

		// Verify current block receipts on gap or reorg
		if gapDetected || reorgDetected {
			if err := verifyBlockReceipts(ctx, client, target, block, reorgDetected); err != nil {
				return err
			}
		}

		// Aggressively query the next block
		var nextBlock map[string]any
		for ctx.Err() == nil {
			nextBlock, err = getFullBlockByNumber(ctx, client, target, currentBlockNumber+1)
			if err == nil && nextBlock != nil {
				break
			}
			sleepCtx(ctx, interval)
		}
		if ctx.Err() != nil {
			return nil //nolint:nilerr // graceful shutdown on signal
		}

		if err := verifyBlockReceipts(ctx, client, target, nextBlock, reorgDetected); err != nil {
			return err
		}

		if reorgDetected && stopAtReorg {
			log.Printf("Stopping scan due to reorg detection (receipts were checked).")
			return nil
		}
	}

	return nil
}

func verifyReceiptsRoot(ctx context.Context, client *rpc.Client, target string, blockNum int64) error {
	block, err := getFullBlockByNumber(ctx, client, target, blockNum)
	if err != nil {
		return fmt.Errorf("get block %d: %w", blockNum, err)
	}
	if block == nil {
		log.Printf("Block %d not found. Skipping.", blockNum)
		return nil
	}

	return verifyBlockReceipts(ctx, client, target, block, false)
}

func verifyBlockReceipts(ctx context.Context, client *rpc.Client, target string, block map[string]any, reorgDetected bool) error {
	blockNum := hexToInt64(block["number"])
	headerReceiptsRoot, _ := block["receiptsRoot"].(string)
	blockHash, _ := block["hash"].(string)

	// Fetch receipts
	receipts, err := fetchBlockReceiptsRaw(ctx, client, target, blockHash)
	if err != nil {
		log.Printf("Error fetching receipts for block %d: %v", blockNum, err)
		return nil // Continue scanning
	}

	computedRoot, err := eth.ComputeReceiptsRoot(receipts)
	if err != nil {
		log.Printf("Error computing receipts root for block %d: %v", blockNum, err)
		return nil
	}

	if computedRoot == headerReceiptsRoot {
		if reorgDetected {
			log.Printf("Block %d: Reorg detected, but receipts root IS valid.", blockNum)
		} else {
			log.Printf("Block %d: Receipts root verified (%d receipts).", blockNum, len(receipts))
		}
		return nil
	}

	log.Printf("CRITICAL: Receipt root mismatch detected at block %d", blockNum)
	log.Printf("Expected header root: %s", headerReceiptsRoot)
	log.Printf("Actual computed root: %s", computedRoot)
	return fmt.Errorf("receipt root mismatch at block %d", blockNum)
}

func getFullBlock(ctx context.Context, client *rpc.Client, target, tag string) (map[string]any, error) {
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["%s",false],"id":1}`, tag)
	var resp map[string]any
	_, err := client.Call(ctx, target, []byte(req), &resp)
	if err != nil {
		return nil, err
	}
	if errVal, ok := resp["error"]; ok {
		return nil, fmt.Errorf("RPC error: %v", errVal)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("no result in response")
	}
	return result, nil
}

func getFullBlockByNumber(ctx context.Context, client *rpc.Client, target string, blockNum int64) (map[string]any, error) {
	hexNum := fmt.Sprintf("0x%x", blockNum)
	return getFullBlock(ctx, client, target, hexNum)
}

func fetchBlockReceiptsRaw(ctx context.Context, client *rpc.Client, target, blockHash string) ([]map[string]any, error) {
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockReceipts","params":["%s"],"id":1}`, blockHash)
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
		return nil, fmt.Errorf("unexpected result type")
	}

	receipts := make([]map[string]any, 0, len(result))
	for _, r := range result {
		receipt, ok := r.(map[string]any)
		if !ok {
			continue
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

func hexToInt64(v any) int64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	s = strings.TrimPrefix(s, "0x")
	var result int64
	for _, c := range s {
		result <<= 4
		switch {
		case c >= '0' && c <= '9':
			result |= int64(c - '0')
		case c >= 'a' && c <= 'f':
			result |= int64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			result |= int64(c - 'A' + 10)
		}
	}
	return result
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
