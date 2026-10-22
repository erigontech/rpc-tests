package vegetagen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/erigontech/rpc-tests/internal/rpc"
)

// TxListConfig selects the blocks whose successful txs are ranked by the gas they use.
type TxListConfig struct {
	URL     string
	Blocks  uint64 // read this many blocks from the head; or set Range
	Range   *BlockRange
	Top     int
	Workers int
}

// GasTx is a tx with the gas it used and its block.
type GasTx struct {
	Hash    string
	GasUsed uint64
	Block   uint64
}

// GasTxList is the result of TopGasTxs: the txs, and the blocks they were drawn from.
type GasTxList struct {
	Txs         []GasTx
	Scanned     uint64
	First, Last uint64
}

type receipt struct {
	TransactionHash string `json:"transactionHash"`
	GasUsed         string `json:"gasUsed"`
	Status          string `json:"status"` // empty before Byzantium
}

// TopGasTxs returns the cfg.Top successful txs that use the most gas in the blocks of cfg, the heaviest first.
// Failed txs are left out: as an eth_call they would revert.
func TopGasTxs(ctx context.Context, cfg TxListConfig) (GasTxList, error) {
	var list GasTxList
	if cfg.Top <= 0 {
		return list, errors.New("top must be a positive number of txs")
	}
	if (cfg.Blocks == 0) == (cfg.Range == nil) {
		return list, errors.New("set exactly one of blocks or a block range")
	}
	n := newNode(cfg.URL)
	head, _, err := rpc.GetLatestBlockNumber(ctx, n.client, n.target)
	if err != nil {
		return list, fmt.Errorf("reading head block: %w", err)
	}
	_, count, num, err := Config{Range: cfg.Range}.blocksToRead(head)
	if err != nil {
		return list, err
	}
	if cfg.Range == nil {
		count = min(cfg.Blocks, count)
	}
	nums := make([]uint64, count)
	for i := range nums {
		nums[i] = num(uint64(i))
	}
	receipts := make([][]receipt, len(nums))
	if err := parallel(len(nums), max(cfg.Workers, 1), func(i int) error {
		r, err := n.blockReceipts(ctx, nums[i])
		receipts[i] = r
		return err
	}); err != nil {
		return list, err
	}
	for i, rs := range receipts {
		for _, r := range rs {
			if r.Status == "0x0" {
				continue
			}
			gas, err := strconv.ParseUint(strings.TrimPrefix(r.GasUsed, "0x"), 16, 64)
			if err != nil {
				return list, fmt.Errorf("tx %s: invalid gasUsed %q", r.TransactionHash, r.GasUsed)
			}
			list.Txs = append(list.Txs, GasTx{Hash: r.TransactionHash, GasUsed: gas, Block: nums[i]})
		}
	}
	sort.Slice(list.Txs, func(a, b int) bool {
		ta, tb := list.Txs[a], list.Txs[b]
		if ta.GasUsed != tb.GasUsed {
			return ta.GasUsed > tb.GasUsed
		}
		if ta.Block != tb.Block {
			return ta.Block > tb.Block
		}
		return ta.Hash < tb.Hash
	})
	list.Txs = list.Txs[:min(cfg.Top, len(list.Txs))]
	list.Scanned, list.First, list.Last = count, slices.Min(nums), slices.Max(nums)
	return list, nil
}

// blockReceipts reads the receipts of a block.
func (n node) blockReceipts(ctx context.Context, num uint64) ([]receipt, error) {
	var resp struct {
		Result []receipt `json:"result"`
		Error  *rpcError `json:"error"`
	}
	req, err := marshalRequest("eth_getBlockReceipts", hexNum(num))
	if err != nil {
		return nil, err
	}
	if _, err := n.client.Call(ctx, n.target, req, &resp); err != nil {
		return nil, fmt.Errorf("reading the receipts of block %d: %w", num, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("reading the receipts of block %d: %s", num, resp.Error.Message)
	}
	return resp.Result, nil
}

// WriteGasTxList writes list as a tx list for --txs, each tx named after its rank and the gas it used.
func WriteGasTxList(path string, list GasTxList) error {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# the %d txs that use the most gas in %d blocks between %d and %d\n", len(list.Txs), list.Scanned, list.First, list.Last)
	for i, tx := range list.Txs {
		fmt.Fprintf(&out, "%d-%.2fMgas %s\n", i+1, float64(tx.GasUsed)/1e6, tx.Hash)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}
