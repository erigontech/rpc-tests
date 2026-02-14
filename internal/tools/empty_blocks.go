package tools

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/erigontech/rpc-tests/internal/rpc"
	"github.com/urfave/cli/v2"
)

var emptyBlocksCommand = &cli.Command{
	Name:  "empty-blocks",
	Usage: "Search backward for N empty blocks from latest",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "url",
			Value: "http://localhost:8545",
			Usage: "HTTP URL of the Ethereum node",
		},
		&cli.IntFlag{
			Name:  "count",
			Value: 10,
			Usage: "Number of empty blocks to search for",
		},
		&cli.BoolFlag{
			Name:  "ignore-withdrawals",
			Usage: "Ignore withdrawals when determining if a block is empty",
		},
		&cli.BoolFlag{
			Name:  "compare-state-root",
			Usage: "Compare state root with parent block",
		},
	},
	Action: runEmptyBlocks,
}

type blockInfo struct {
	Number         uint64
	Transactions   []any
	Withdrawals    []any
	HasWithdrawals bool
	StateRoot      string
	ParentHash     string
}

func runEmptyBlocks(c *cli.Context) error {
	url := c.String("url")
	count := c.Int("count")
	ignoreWithdrawals := c.Bool("ignore-withdrawals")
	compareStateRoot := c.Bool("compare-state-root")

	client := rpc.NewClient("http", "", 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Printf("Received interrupt signal")
		cancel()
	}()

	// Strip protocol prefix to get target for rpc.Client
	target := strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://")

	latestBlock, _, err := rpc.GetLatestBlockNumber(ctx, client, target)
	if err != nil {
		return fmt.Errorf("get latest block: %w", err)
	}
	log.Printf("Latest block number: %d", latestBlock)
	log.Printf("Searching for the last %d empty blocks...", count)

	var emptyBlocks []uint64
	batchSize := 100

	currentBlock := int64(latestBlock)
	for currentBlock >= 0 && len(emptyBlocks) < count {
		if ctx.Err() != nil {
			break
		}

		startBlock := max(0, currentBlock-int64(batchSize)+1)

		// Fetch blocks in parallel
		blocks := make([]blockInfo, currentBlock-startBlock+1)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var fetchErr error

		for i := startBlock; i <= currentBlock; i++ {
			wg.Add(1)
			go func(blockNum int64, idx int) {
				defer wg.Done()
				bi, err := fetchBlockInfo(ctx, client, target, blockNum)
				if err != nil {
					mu.Lock()
					if fetchErr == nil {
						fetchErr = err
					}
					mu.Unlock()
					return
				}
				blocks[idx] = bi
			}(i, int(i-startBlock))
		}
		wg.Wait()

		if fetchErr != nil {
			log.Printf("Warning: failed to fetch some blocks: %v", fetchErr)
		}

		// Process results backward
		for i := len(blocks) - 1; i >= 0 && len(emptyBlocks) < count; i-- {
			b := blocks[i]
			if b.Number == 0 && i > 0 {
				continue // skip unfetched blocks
			}

			noTxns := len(b.Transactions) == 0
			if !noTxns {
				continue
			}
			if !ignoreWithdrawals && b.HasWithdrawals && len(b.Withdrawals) > 0 {
				continue
			}

			emptyBlocks = append(emptyBlocks, b.Number)
			log.Printf("Block %d is empty. Total found: %d/%d", b.Number, len(emptyBlocks), count)

			if compareStateRoot && b.Number > 0 {
				parent, err := fetchBlockInfo(ctx, client, target, int64(b.Number-1))
				if err == nil {
					if b.StateRoot == parent.StateRoot {
						log.Printf("  stateRoot: %s MATCHES", b.StateRoot)
					} else {
						log.Printf("  stateRoot: %s DOES NOT MATCH [parent stateRoot: %s]", b.StateRoot, parent.StateRoot)
					}
				}
			}
		}

		currentBlock = startBlock - 1
		if currentBlock >= 0 && currentBlock%100000 == 0 {
			log.Printf("Reached block %d...", currentBlock)
		}
	}

	if len(emptyBlocks) == count {
		log.Printf("Found last %d empty blocks!", count)
	} else if len(emptyBlocks) == 0 {
		log.Printf("Warning: could not find %d empty blocks within the chain history.", count)
	}

	return nil
}

func fetchBlockInfo(ctx context.Context, client *rpc.Client, target string, blockNum int64) (blockInfo, error) {
	hexNum := "0x" + strconv.FormatInt(blockNum, 16)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["%s",false],"id":1}`, hexNum)

	var resp map[string]any
	_, err := client.Call(ctx, target, []byte(req), &resp)
	if err != nil {
		return blockInfo{}, err
	}
	if errVal, ok := resp["error"]; ok {
		return blockInfo{}, fmt.Errorf("RPC error: %v", errVal)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return blockInfo{}, fmt.Errorf("unexpected result type")
	}

	bi := blockInfo{
		Number: uint64(blockNum),
	}

	if txns, ok := result["transactions"].([]any); ok {
		bi.Transactions = txns
	}
	if withdrawals, ok := result["withdrawals"].([]any); ok {
		bi.HasWithdrawals = true
		bi.Withdrawals = withdrawals
	}
	if sr, ok := result["stateRoot"].(string); ok {
		bi.StateRoot = sr
	}
	if ph, ok := result["parentHash"].(string); ok {
		bi.ParentHash = ph
	}

	return bi, nil
}
