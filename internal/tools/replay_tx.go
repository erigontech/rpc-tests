package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/urfave/cli/v2"
)

const (
	silkTarget     = "127.0.0.1:51515"
	rpcdaemonTarget = "localhost:8545"
	outputDir      = "./output/"
)

var replayTxCommand = &cli.Command{
	Name:  "replay-tx",
	Usage: "Scan blocks for transactions and compare trace responses between two servers",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "start",
			Usage: "Starting point as block:tx (e.g., 1000:0)",
			Value: "0:0",
		},
		&cli.BoolFlag{
			Name:    "continue",
			Aliases: []string{"c"},
			Usage:   "Continue scanning, don't stop at first diff",
		},
		&cli.IntFlag{
			Name:    "number",
			Aliases: []string{"n"},
			Value:   0,
			Usage:   "Maximum number of failed txs before stopping",
		},
		&cli.IntFlag{
			Name:    "method",
			Aliases: []string{"m"},
			Value:   0,
			Usage:   "0: trace_replayTransaction, 1: debug_traceTransaction",
		},
	},
	Action: runReplayTx,
}

func runReplayTx(c *cli.Context) error {
	startStr := c.String("start")
	continueOnDiff := c.Bool("continue")
	maxFailed := c.Int("number")
	methodID := c.Int("method")

	if maxFailed > 0 {
		continueOnDiff = true
	}

	parts := strings.SplitN(startStr, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("bad start field definition: block:tx")
	}
	startBlock, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid start block: %w", err)
	}
	startTx, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid start tx: %w", err)
	}

	log.Printf("Starting scans from: %d tx-index: %d", startBlock, startTx)

	// Clean and recreate output directory
	os.RemoveAll(outputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	makeRequest := makeTraceTransaction
	if methodID == 1 {
		makeRequest = makeDebugTraceTransaction
	}

	client := rpc.NewClient("http", "", 0)
	ctx := context.Background()

	failedRequest := 0
	for block := startBlock; block < 18000000; block++ {
		fmt.Printf("%09d\r", block)

		// Get block with full transactions
		hexBlock := "0x" + strconv.FormatInt(block, 16)
		blockReq := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["%s",true],"id":1}`, hexBlock)

		var blockResp map[string]any
		_, err := client.Call(ctx, silkTarget, []byte(blockReq), &blockResp)
		if err != nil {
			continue
		}
		if blockResp["error"] != nil {
			continue
		}
		result, ok := blockResp["result"].(map[string]any)
		if !ok || result == nil {
			continue
		}
		transactions, ok := result["transactions"].([]any)
		if !ok || len(transactions) == 0 {
			continue
		}

		for txn := int(startTx); txn < len(transactions); txn++ {
			tx, ok := transactions[txn].(map[string]any)
			if !ok {
				continue
			}
			input, _ := tx["input"].(string)
			if len(input) < 2 {
				continue
			}
			txHash, _ := tx["hash"].(string)

			res := compareTxResponses(ctx, client, makeRequest, block, txn, txHash)
			if res == 1 {
				log.Printf("Diff on block: %d tx-index: %d Hash: %s", block, txn, txHash)
				if !continueOnDiff {
					return fmt.Errorf("diff found")
				}
				if maxFailed > 0 {
					failedRequest++
					if failedRequest >= maxFailed {
						return fmt.Errorf("max failed requests reached: %d", maxFailed)
					}
				}
			}
		}
		// Reset start tx after first block
		startTx = 0
	}

	return nil
}

type requestBuilder func(txHash string) string

func makeTraceTransaction(txHash string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","method":"trace_replayTransaction","params":["%s",["vmTrace"]],"id":1}`, txHash)
}

func makeDebugTraceTransaction(txHash string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","method":"debug_traceTransaction","params":["%s",{"disableMemory":false,"disableStack":false,"disableStorage":false}],"id":1}`, txHash)
}

func compareTxResponses(ctx context.Context, client *rpc.Client, makeRequest requestBuilder, block int64, txIndex int, txHash string) int {
	filename := fmt.Sprintf("bn_%d_txn_%d_hash_%s", block, txIndex, txHash)
	silkFilename := outputDir + filename + ".silk"
	rpcdaemonFilename := outputDir + filename + ".rpcdaemon"
	diffFilename := outputDir + filename + ".diffs"

	request := makeRequest(txHash)

	var silkResp, rpcdaemonResp any
	_, err1 := client.Call(ctx, silkTarget, []byte(request), &silkResp)
	_, err2 := client.Call(ctx, rpcdaemonTarget, []byte(request), &rpcdaemonResp)

	if err1 != nil || err2 != nil {
		log.Printf("Request error: silk=%v rpcdaemon=%v", err1, err2)
		return 0
	}

	silkJSON, _ := json.MarshalIndent(silkResp, "", "      ")
	rpcdaemonJSON, _ := json.MarshalIndent(rpcdaemonResp, "", "      ")

	_ = os.WriteFile(silkFilename, silkJSON, 0644)
	_ = os.WriteFile(rpcdaemonFilename, rpcdaemonJSON, 0644)

	// Compare
	if string(silkJSON) != string(rpcdaemonJSON) {
		_ = os.WriteFile(diffFilename, []byte("DIFF"), 0644)
		return 1
	}

	// Clean up if no diff
	os.Remove(silkFilename)
	os.Remove(rpcdaemonFilename)
	os.Remove(diffFilename)
	return 0
}
