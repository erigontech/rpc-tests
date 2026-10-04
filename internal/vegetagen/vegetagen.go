// Package vegetagen generates vegeta target files for rpc_perf by turning the
// transactions of recent blocks into JSON-RPC requests.
package vegetagen

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/erigontech/rpc-tests/internal/perf"
	"github.com/erigontech/rpc-tests/internal/rpc"
)

type Transaction struct {
	Hash     string  `json:"hash"`
	From     string  `json:"from"`
	To       *string `json:"to"`
	Gas      string  `json:"gas"`
	GasPrice string  `json:"gasPrice"`
	Value    string  `json:"value"`
	Input    string  `json:"input"`
}

type Block struct {
	Number       string        `json:"number"`
	Transactions []Transaction `json:"transactions"`
}

// Generator turns one transaction into the body of one JSON-RPC request.
type Generator interface {
	Method() string
	Request(txn Transaction, blockParam string) ([]byte, error)
}

// BlockTag selects the state a generated request runs on.
type BlockTag string

const (
	TagLatest BlockTag = "latest" // the "latest" tag, resolved by each node when the request runs
	TagHead   BlockTag = "head"   // the head number at generation time, the same state on every node
	TagParent BlockTag = "parent" // the block before the transaction's own block
)

func ParseBlockTag(s string) (BlockTag, error) {
	switch tag := BlockTag(s); tag {
	case TagLatest, TagHead, TagParent:
		return tag, nil
	}
	return "", fmt.Errorf("invalid block tag %q: want latest, head or parent", s)
}

func blockParam(tag BlockTag, blockNum, head uint64) string {
	switch tag {
	case TagHead:
		return fmt.Sprintf("0x%x", head)
	case TagParent:
		return fmt.Sprintf("0x%x", blockNum-1)
	}
	return "latest"
}

// Keep selects which verified requests are written.
type Keep string

const (
	KeepOK  Keep = "ok"
	KeepAll Keep = "all"
)

type Config struct {
	URL       string // node used to read blocks and verify requests
	TargetURL string // url written in each vegeta target
	Blocks    uint64 // blocks to read, from the head downwards; set this or Count
	Tag       BlockTag
	Verify    bool // run each request on the node; Keep then selects what is written
	Keep      Keep
	KeepFees  bool // copy the tx gas price; dropped by default, see Run
	Count     int  // or stop after writing this many requests, scanning as many blocks as needed
	MinCount  int  // fail if fewer requests are written
	Workers   int
	Out       string // tar file path
}

type Summary struct {
	BlocksScanned uint64
	FirstBlock    uint64 // head, the first block scanned
	LastBlock     uint64 // the oldest block scanned
	Generated     int
	OK            int
	Written       int
	Errors        map[string]int // verification errors by kind
}

// Run reads blocks from the head downwards, turns their transactions into
// requests with gen and writes them as a vegeta target file inside a tar.
func Run(ctx context.Context, cfg Config, gen Generator) (Summary, error) {
	sum := Summary{Errors: map[string]int{}}
	if (cfg.Blocks == 0) == (cfg.Count == 0) {
		return sum, errors.New("set exactly one of blocks or count")
	}
	verify := cfg.Verify
	workers := max(cfg.Workers, 1)

	n := newNode(cfg.URL)
	head, _, err := rpc.GetLatestBlockNumber(ctx, n.client, n.target)
	if err != nil {
		return sum, fmt.Errorf("reading head block: %w", err)
	}

	var out bytes.Buffer
	sum.FirstBlock = head
	scanAll := cfg.Blocks == 0 // stop on count only
	full := func() bool { return cfg.Count > 0 && sum.Written >= cfg.Count }
	for offset := uint64(0); (scanAll || offset < cfg.Blocks) && offset <= head && !full(); offset += uint64(workers) {
		end := offset + uint64(workers)
		if !scanAll {
			end = min(end, cfg.Blocks)
		}
		var nums []uint64
		for i := offset; i < end && i <= head; i++ {
			nums = append(nums, head-i)
		}
		blocks, err := n.blocks(ctx, nums, workers)
		if err != nil {
			return sum, err
		}
		var (
			reqs     [][]byte
			reqBlock []int // index in blocks of each request
		)
		for i, b := range blocks {
			for _, txn := range b.Transactions {
				// A copied gas price fails the base fee and balance checks once the base fee has risen;
				// with no fee the node skips both and still executes the call.
				if !cfg.KeepFees {
					txn.GasPrice = ""
				}
				req, err := gen.Request(txn, blockParam(cfg.Tag, nums[i], head))
				if err != nil {
					return sum, fmt.Errorf("building request for tx %s: %w", txn.Hash, err)
				}
				reqs = append(reqs, req)
				reqBlock = append(reqBlock, i)
			}
		}
		var failures []string
		if verify {
			if failures, err = n.verify(ctx, reqs, workers); err != nil {
				return sum, err
			}
		}
		used, lastUsed := len(blocks), -1
		for i, req := range reqs {
			if full() {
				used = lastUsed + 1
				break
			}
			lastUsed = reqBlock[i]
			sum.Generated++
			if verify {
				if failures[i] == "" {
					sum.OK++
				} else {
					sum.Errors[failures[i]]++
					if cfg.Keep == KeepOK {
						continue
					}
				}
			}
			if err := writeTarget(&out, cfg.TargetURL, req); err != nil {
				return sum, err
			}
			sum.Written++
		}
		sum.BlocksScanned += uint64(used)
	}
	if sum.BlocksScanned > 0 {
		sum.LastBlock = head - (sum.BlocksScanned - 1)
	}

	if sum.Written < cfg.MinCount {
		return sum, fmt.Errorf("%d requests written, fewer than min-count %d", sum.Written, cfg.MinCount)
	}
	return sum, writeTar(cfg.Out, "erigon_stress_test/vegeta_erigon_"+gen.Method()+".txt", out.Bytes())
}

type node struct {
	client *rpc.Client
	target string
}

func newNode(url string) node {
	transport := "http"
	if strings.HasPrefix(url, "https://") {
		transport = "https"
	}
	target := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	return node{client: rpc.NewClient(transport, "", 0), target: target}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// blocks fetches the given blocks with full transactions, preserving their order.
func (n node) blocks(ctx context.Context, nums []uint64, workers int) ([]Block, error) {
	blocks := make([]Block, len(nums))
	err := parallel(len(nums), workers, func(i int) error {
		var resp struct {
			Result *Block    `json:"result"`
			Error  *rpcError `json:"error"`
		}
		req, err := marshalRequest("eth_getBlockByNumber", fmt.Sprintf("0x%x", nums[i]), true)
		if err != nil {
			return err
		}
		if _, err := n.client.Call(ctx, n.target, req, &resp); err != nil {
			return fmt.Errorf("reading block %d: %w", nums[i], err)
		}
		if resp.Error != nil {
			return fmt.Errorf("reading block %d: %s", nums[i], resp.Error.Message)
		}
		if resp.Result == nil {
			return fmt.Errorf("block %d not found", nums[i])
		}
		blocks[i] = *resp.Result
		return nil
	})
	return blocks, err
}

// verify runs every request on the node and returns, for each one, its error
// kind, or "" when it succeeded. A transport failure aborts the run.
func (n node) verify(ctx context.Context, reqs [][]byte, workers int) ([]string, error) {
	failures := make([]string, len(reqs))
	err := parallel(len(reqs), workers, func(i int) error {
		var resp struct {
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if _, err := n.client.Call(ctx, n.target, reqs[i], &resp); err != nil {
			return fmt.Errorf("verifying request: %w", err)
		}
		if resp.Error != nil {
			failures[i] = errorKind(resp.Error.Message)
		}
		return nil
	})
	return failures, err
}

// errorKind drops what nodes append after the first colon (addresses, amounts, revert reasons),
// so that errors of the same kind are counted together.
func errorKind(msg string) string {
	kind, _, _ := strings.Cut(msg, ":")
	return strings.TrimSpace(kind)
}

// parallel runs fn for every index in [0, n) on at most workers goroutines.
func parallel(n, workers int, fn func(i int) error) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
		sem  = make(chan struct{}, workers)
	)
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			if err := fn(i); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func marshalRequest(method string, params ...any) ([]byte, error) {
	return json.Marshal(struct {
		Jsonrpc string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
		ID      int    `json:"id"`
	}{Jsonrpc: "2.0", Method: method, Params: params, ID: 1})
}

func writeTarget(out *bytes.Buffer, url string, body []byte) error {
	line, err := json.Marshal(perf.VegetaTarget{
		Method: "POST",
		URL:    url,
		Body:   body,
		Header: map[string][]string{"Content-Type": {"application/json"}},
	})
	if err != nil {
		return err
	}
	out.Write(line)
	out.WriteByte('\n')
	return nil
}

// writeTar writes a tar holding the directory of name followed by name itself.
func writeTar(path, name string, content []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	now := time.Now()
	tw := tar.NewWriter(f)
	dir := name[:strings.LastIndex(name, "/")+1]
	if err := tw.WriteHeader(&tar.Header{Name: dir, Typeflag: tar.TypeDir, Mode: 0755, ModTime: now}); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(content)), ModTime: now}); err != nil {
		return err
	}
	if _, err := tw.Write(content); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return f.Close()
}
