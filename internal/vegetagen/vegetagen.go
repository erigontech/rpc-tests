// Package vegetagen generates vegeta target files for rpc_perf by turning the
// transactions and blocks of recent blocks into JSON-RPC requests.
package vegetagen

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"slices"
	"sort"
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
	Hash         string        `json:"hash"`
	Transactions []Transaction `json:"transactions"`
}

// Spec is one method of a load with its relative weight.
type Spec struct {
	Name   string // as reported in the summary; the method when empty
	Gen    Generator
	Weight float64
}

func (s Spec) name() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Gen.Method()
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
	KeepFees  bool     // copy the tx gas price; dropped by default, see Run
	Count     int      // or stop after writing this many requests, scanning as many blocks as needed
	MinCount  int      // fail if fewer requests are written
	Contracts []string // keep only the txs to these contracts (lower case), and the blocks holding them
	Name      string   // vegeta_erigon_<Name>.txt; the method for one method, "mixed" for several
	MaxBlocks uint64   // with several methods, the window of recent blocks the requests are drawn from
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
	Errors        map[string]int            // verification errors by kind
	Methods       map[string]*MethodSummary // by spec name
}

type MethodSummary struct {
	Generated int
	OK        int
	Written   int
	Errors    map[string]int
}

// candidate is a request waiting for verification.
type candidate struct {
	spec   int
	req    []byte
	offset uint64 // blocks from the head
}

// Run reads blocks from the head downwards, turns their transactions and blocks into
// requests with the generators of specs and writes them as a vegeta target file inside a tar.
// With several specs the requests are interleaved at random, with a fixed seed, so that every
// part of the file keeps the shares of the weights.
func Run(ctx context.Context, cfg Config, specs []Spec) (Summary, error) {
	sum := Summary{Errors: map[string]int{}, Methods: map[string]*MethodSummary{}}
	if len(specs) == 0 {
		return sum, errors.New("no methods")
	}
	if (cfg.Blocks == 0) == (cfg.Count == 0) {
		return sum, errors.New("set exactly one of blocks or count")
	}
	if cfg.Blocks > 0 && len(specs) > 1 {
		return sum, errors.New("a mix of methods needs count, since the shares apply to a total")
	}
	for _, sp := range specs {
		sum.Methods[sp.name()] = &MethodSummary{Errors: map[string]int{}}
	}
	if len(specs) > 1 {
		return runMix(ctx, cfg, specs, sum)
	}
	targets := shares(cfg.Count, specs) // -1 = no limit
	verify := cfg.Verify
	workers := max(cfg.Workers, 1)

	n := newNode(cfg.URL)
	head, _, err := rpc.GetLatestBlockNumber(ctx, n.client, n.target)
	if err != nil {
		return sum, fmt.Errorf("reading head block: %w", err)
	}
	sum.FirstBlock = head

	written := make([][][]byte, len(specs))
	full := func(i int) bool { return targets[i] >= 0 && len(written[i]) >= targets[i] }
	var usedOffset uint64
	accept := func(cands []candidate, failures []string) {
		for j, c := range cands {
			if full(c.spec) {
				continue
			}
			ms := sum.Methods[specs[c.spec].name()]
			ms.Generated++
			sum.Generated++
			if verify {
				if failures[j] == "" {
					ms.OK++
					sum.OK++
				} else {
					ms.Errors[failures[j]]++
					sum.Errors[failures[j]]++
					if cfg.Keep == KeepOK {
						continue
					}
				}
			}
			written[c.spec] = append(written[c.spec], c.req)
			ms.Written++
			sum.Written++
			usedOffset = max(usedOffset, c.offset)
		}
	}

	var constants []candidate
	for i, sp := range specs {
		if sp.Gen.Kind() != Constant {
			continue
		}
		req, err := sp.Gen.Request(Item{}, Params{})
		if err != nil {
			return sum, err
		}
		for range targets[i] {
			constants = append(constants, candidate{spec: i, req: req})
		}
	}
	if err := n.check(ctx, constants, verify, workers, accept); err != nil {
		return sum, err
	}

	pending := func() bool {
		for i, sp := range specs {
			if sp.Gen.Kind() != Constant && !full(i) {
				return true
			}
		}
		return false
	}
	contracts := map[string]bool{}
	for _, c := range cfg.Contracts {
		contracts[strings.ToLower(c)] = true
	}
	params := Params{Contracts: cfg.Contracts}

	scanAll := cfg.Blocks == 0 // stop on count only
	var scanned uint64
	for offset := uint64(0); (scanAll || offset < cfg.Blocks) && offset <= head && pending(); offset += uint64(workers) {
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
		scanned += uint64(len(blocks))
		var cands []candidate
		for i, sp := range specs {
			if sp.Gen.Kind() == Constant || full(i) {
				continue
			}
			for bi := range blocks {
				b := &blocks[bi]
				txs := b.Transactions
				if len(contracts) > 0 {
					txs = slices.DeleteFunc(slices.Clone(txs), func(t Transaction) bool {
						return t.To == nil || !contracts[strings.ToLower(*t.To)]
					})
					if len(txs) == 0 {
						continue
					}
				}
				p := params
				p.BlockParam = blockParam(cfg.Tag, nums[bi], head)
				item := Item{Block: b, BlockNum: nums[bi]}
				if sp.Gen.Kind() == PerBlock {
					req, err := sp.Gen.Request(item, p)
					if err != nil {
						return sum, fmt.Errorf("building %s for block %d: %w", sp.Gen.Method(), nums[bi], err)
					}
					cands = append(cands, candidate{spec: i, req: req, offset: offset + uint64(bi)})
					continue
				}
				for _, txn := range txs {
					// A copied gas price fails the base fee and balance checks once the base fee has risen;
					// with no fee the node skips both and still executes the call.
					if !cfg.KeepFees {
						txn.GasPrice = ""
					}
					item.Txn = &txn
					req, err := sp.Gen.Request(item, p)
					if err != nil {
						return sum, fmt.Errorf("building %s for tx %s: %w", sp.Gen.Method(), txn.Hash, err)
					}
					cands = append(cands, candidate{spec: i, req: req, offset: offset + uint64(bi)})
				}
			}
		}
		if err := n.check(ctx, cands, verify, workers, accept); err != nil {
			return sum, err
		}
	}
	if scanAll {
		if sum.Written > 0 {
			sum.BlocksScanned = usedOffset + 1
		}
	} else {
		sum.BlocksScanned = scanned
	}
	if sum.BlocksScanned > 0 {
		sum.LastBlock = head - (sum.BlocksScanned - 1)
	}

	return sum, write(cfg, specs, sum, written)
}

// DefaultMaxBlocks is the window of recent blocks a mix of methods is drawn from.
const DefaultMaxBlocks = 3000

// runMix draws every request of a mix from one window of recent blocks shared by all methods,
// with repetitions, as many users asking different methods about the same recent blocks and txs.
// Only the per-tx requests, which can fail on the state, are all verified: a block of the window
// exists, so one sample per method checks that the node serves it.
func runMix(ctx context.Context, cfg Config, specs []Spec, sum Summary) (Summary, error) {
	window := cfg.MaxBlocks
	if window == 0 {
		window = DefaultMaxBlocks
	}
	workers := max(cfg.Workers, 1)
	n := newNode(cfg.URL)
	head, _, err := rpc.GetLatestBlockNumber(ctx, n.client, n.target)
	if err != nil {
		return sum, fmt.Errorf("reading head block: %w", err)
	}
	var nums []uint64
	for i := uint64(0); i < window && i <= head; i++ {
		nums = append(nums, head-i)
	}
	blocks, err := n.blocks(ctx, nums, workers)
	if err != nil {
		return sum, err
	}
	sum.FirstBlock, sum.BlocksScanned = head, uint64(len(blocks))
	sum.LastBlock = head - (sum.BlocksScanned - 1)

	contracts := map[string]bool{}
	for _, c := range cfg.Contracts {
		contracts[strings.ToLower(c)] = true
	}
	type txRef struct {
		block int
		txn   Transaction
	}
	var (
		blockRefs []int
		txRefs    []txRef
	)
	for bi, b := range blocks {
		matched := false
		for _, txn := range b.Transactions {
			if len(contracts) > 0 && (txn.To == nil || !contracts[strings.ToLower(*txn.To)]) {
				continue
			}
			matched = true
			// A copied gas price fails the base fee and balance checks once the base fee has risen.
			if !cfg.KeepFees {
				txn.GasPrice = ""
			}
			txRefs = append(txRefs, txRef{bi, txn})
		}
		if matched || len(contracts) == 0 {
			blockRefs = append(blockRefs, bi)
		}
	}

	rng := rand.New(rand.NewSource(1)) //nolint:gosec
	targets := shares(cfg.Count, specs)
	written := make([][][]byte, len(specs))
	for i, sp := range specs {
		ms := sum.Methods[sp.name()]
		build := func(bi int, txn *Transaction) ([]byte, error) {
			p := Params{BlockParam: blockParam(cfg.Tag, nums[bi], head), Contracts: cfg.Contracts}
			return sp.Gen.Request(Item{Block: &blocks[bi], BlockNum: nums[bi], Txn: txn}, p)
		}
		var reqs [][]byte
		switch sp.Gen.Kind() {
		case Constant:
			req, err := sp.Gen.Request(Item{}, Params{})
			if err != nil {
				return sum, err
			}
			reqs = [][]byte{req}
		case PerBlock:
			for _, bi := range blockRefs {
				req, err := build(bi, nil)
				if err != nil {
					return sum, err
				}
				reqs = append(reqs, req)
			}
		case PerTx:
			for _, j := range rng.Perm(len(txRefs)) {
				req, err := build(txRefs[j].block, &txRefs[j].txn)
				if err != nil {
					return sum, err
				}
				reqs = append(reqs, req)
			}
		}
		pool, err := n.mixPool(ctx, cfg, sp.Gen.Kind(), reqs, targets[i], workers, ms, &sum)
		if err != nil {
			return sum, err
		}
		if len(pool) == 0 && targets[i] > 0 {
			return sum, fmt.Errorf("%s: no valid request in the window of %d blocks", sp.name(), len(blocks))
		}
		for range targets[i] {
			written[i] = append(written[i], pool[rng.Intn(len(pool))])
		}
		ms.Written = targets[i]
		sum.Written += targets[i]
	}
	return sum, write(cfg, specs, sum, written)
}

// mixPool returns the requests a mix method draws from. Per-tx requests are verified in batches
// until target of them are kept; for the others the first request is verified as a sample.
func (n node) mixPool(ctx context.Context, cfg Config, kind Kind, reqs [][]byte, target, workers int, ms *MethodSummary, sum *Summary) ([][]byte, error) {
	if !cfg.Verify || len(reqs) == 0 {
		return reqs, nil
	}
	if kind != PerTx {
		kept, err := n.tally(ctx, cfg, reqs[:1], workers, ms, sum)
		if err != nil || len(kept) == 0 {
			return nil, err
		}
		return reqs, nil
	}
	var pool [][]byte
	for start := 0; start < len(reqs) && len(pool) < target; {
		end := min(len(reqs), start+max(1024, 2*(target-len(pool))))
		kept, err := n.tally(ctx, cfg, reqs[start:end], workers, ms, sum)
		if err != nil {
			return nil, err
		}
		pool = append(pool, kept...)
		start = end
	}
	return pool, nil
}

// tally verifies reqs, counts the outcomes and returns the requests to keep.
func (n node) tally(ctx context.Context, cfg Config, reqs [][]byte, workers int, ms *MethodSummary, sum *Summary) ([][]byte, error) {
	failures, err := n.verify(ctx, reqs, workers)
	if err != nil {
		return nil, err
	}
	var kept [][]byte
	for j, failure := range failures {
		ms.Generated++
		sum.Generated++
		if failure == "" {
			ms.OK++
			sum.OK++
		} else {
			ms.Errors[failure]++
			sum.Errors[failure]++
			if cfg.Keep == KeepOK {
				continue
			}
		}
		kept = append(kept, reqs[j])
	}
	return kept, nil
}

// write checks MinCount and writes the requests, interleaved, as the vegeta file of the tar.
func write(cfg Config, specs []Spec, sum Summary, written [][][]byte) error {
	if sum.Written < cfg.MinCount {
		return fmt.Errorf("%d requests written, fewer than min-count %d", sum.Written, cfg.MinCount)
	}
	name := cfg.Name
	if name == "" {
		name = specs[0].Gen.Method()
		if len(specs) > 1 {
			name = "mixed"
		}
	}
	var out bytes.Buffer
	for _, req := range interleave(written) {
		if err := writeTarget(&out, cfg.TargetURL, req); err != nil {
			return err
		}
	}
	return writeTar(cfg.Out, "erigon_stress_test/vegeta_erigon_"+name+".txt", out.Bytes())
}

// shares splits count among specs by weight, giving the rest to the largest remainders.
// It returns -1 for every spec when count is 0, i.e. when blocks bound the run.
func shares(count int, specs []Spec) []int {
	out := make([]int, len(specs))
	if count == 0 {
		for i := range out {
			out[i] = -1
		}
		return out
	}
	total := 0.0
	for _, sp := range specs {
		total += sp.Weight
	}
	rest := make([]float64, len(specs))
	assigned := 0
	for i, sp := range specs {
		exact := float64(count) * sp.Weight / total
		out[i] = int(math.Floor(exact))
		rest[i] = exact - float64(out[i])
		assigned += out[i]
	}
	order := make([]int, len(specs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return rest[order[a]] > rest[order[b]] })
	for _, i := range order[:count-assigned] {
		out[i]++
	}
	return out
}

// interleave mixes the requests of every spec at random, with a fixed seed, keeping the order
// within each spec. With one spec it returns its requests as they are.
func interleave(written [][][]byte) [][]byte {
	if len(written) == 1 {
		return written[0]
	}
	var labels []int
	for i, reqs := range written {
		for range reqs {
			labels = append(labels, i)
		}
	}
	rand.New(rand.NewSource(1)).Shuffle(len(labels), func(a, b int) { labels[a], labels[b] = labels[b], labels[a] }) //nolint:gosec
	next := make([]int, len(written))
	out := make([][]byte, 0, len(labels))
	for _, i := range labels {
		out = append(out, written[i][next[i]])
		next[i]++
	}
	return out
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

// check verifies cands on the node when verify is set, then hands them to accept.
func (n node) check(ctx context.Context, cands []candidate, verify bool, workers int, accept func([]candidate, []string)) error {
	var failures []string
	if verify {
		reqs := make([][]byte, len(cands))
		for i, c := range cands {
			reqs[i] = c.req
		}
		var err error
		if failures, err = n.verify(ctx, reqs, workers); err != nil {
			return err
		}
	}
	accept(cands, failures)
	return nil
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
			failures[i] = perf.RPCErrorKind(resp.Error.Message)
		}
		return nil
	})
	return failures, err
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
	}{Jsonrpc: "2.0", Method: method, Params: append([]any{}, params...), ID: 1})
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
