package vegetagen

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// Kind tells what a generator builds one request from.
type Kind int

const (
	PerTx    Kind = iota // one request per transaction
	PerBlock             // one request per block
	Constant             // the same request, with no parameters taken from the chain
	PerLog               // one request per log of a block
)

func (k Kind) String() string {
	return [...]string{"per-tx", "per-block", "constant", "per-log"}[k]
}

// Item is the chain data a request is built from: Txn is set for PerTx generators only, Log for PerLog only.
type Item struct {
	Block    *Block
	BlockNum uint64
	Txn      *Transaction
	Log      *Log
}

type Params struct {
	BlockParam string          // state to run on, for methods that take a block parameter
	Head       uint64          // head at generation time, or the end of the block range: the newest block a request may refer to
	Contracts  map[string]bool // the [contracts] filter (lower case): a request takes values only from their logs; empty = all
}

// Generator turns one item into the body of one JSON-RPC request.
type Generator interface {
	Method() string
	Kind() Kind
	Request(it Item, p Params) ([]byte, error)
}

// NewGenerator returns the generator of method, configured with options.
func NewGenerator(method string, options map[string]string) (Generator, error) {
	newGen, ok := generators[method]
	if !ok {
		return nil, fmt.Errorf("unsupported method %s", method)
	}
	return newGen(options)
}

var generators = map[string]func(options map[string]string) (Generator, error){
	"eth_call":                  noOptions(EthCall{}),
	"eth_estimateGas":           noOptions(EstimateGas{}),
	"eth_createAccessList":      noOptions(CreateAccessList{}),
	"eth_getTransactionReceipt": noOptions(simple{"eth_getTransactionReceipt", PerTx, txHash}),
	"eth_getTransactionByHash":  noOptions(simple{"eth_getTransactionByHash", PerTx, txHash}),
	"eth_getBalance":            noOptions(simple{"eth_getBalance", PerTx, senderAtBlock}),
	"eth_getTransactionCount":   noOptions(simple{"eth_getTransactionCount", PerTx, senderAtBlock}),
	"eth_getBlockByNumber":      blockGenerator("eth_getBlockByNumber", true, func(it Item) any { return hexNum(it.BlockNum) }),
	"eth_getBlockByHash":        blockGenerator("eth_getBlockByHash", false, func(it Item) any { return it.Block.Hash }),
	"eth_getBlockReceipts":      blockReceiptsGenerator,
	"eth_getLogs":               getLogsGenerator,
	"debug_traceBlockByHash":    traceBlockGenerator("debug_traceBlockByHash", func(it Item) any { return it.Block.Hash }),
	"debug_traceBlockByNumber":  traceBlockGenerator("debug_traceBlockByNumber", func(it Item) any { return hexNum(it.BlockNum) }),
	"eth_blockNumber":           noOptions(simple{"eth_blockNumber", Constant, noParams}),
	"eth_chainId":               noOptions(simple{"eth_chainId", Constant, noParams}),
	"net_listening":             noOptions(simple{"net_listening", Constant, noParams}),
	"eth_gasPrice":              noOptions(simple{"eth_gasPrice", Constant, noParams}),
	"eth_maxPriorityFeePerGas":  noOptions(simple{"eth_maxPriorityFeePerGas", Constant, noParams}),
}

// Methods returns the methods with a generator, sorted.
func Methods() []string {
	return slices.Sorted(maps.Keys(generators))
}

type simple struct {
	method string
	kind   Kind
	params func(it Item, p Params) []any
}

func (g simple) Method() string { return g.method }
func (g simple) Kind() Kind     { return g.kind }
func (g simple) Request(it Item, p Params) ([]byte, error) {
	return marshalRequest(g.method, g.params(it, p)...)
}

func txHash(it Item, _ Params) []any        { return []any{it.Txn.Hash} }
func senderAtBlock(it Item, p Params) []any { return []any{it.Txn.From, p.BlockParam} }
func noParams(Item, Params) []any           { return nil }

func hexNum(n uint64) string { return fmt.Sprintf("0x%x", n) }

func noOptions(g Generator) func(map[string]string) (Generator, error) {
	return func(options map[string]string) (Generator, error) {
		if err := checkOptions(g.Method(), options); err != nil {
			return nil, err
		}
		return g, nil
	}
}

func checkOptions(method string, options map[string]string, allowed ...string) error {
	for key := range options {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("%s: unknown option %q", method, key)
		}
	}
	return nil
}

// blockGenerator builds a block lookup whose "full" option selects full transactions. With byNumber the
// "tag" option replaces the block number with a tag, see blockTagOption.
func blockGenerator(method string, byNumber bool, blockID func(Item) any) func(map[string]string) (Generator, error) {
	return func(options map[string]string) (Generator, error) {
		allowed := []string{"full"}
		if byNumber {
			allowed = append(allowed, "tag")
		}
		if err := checkOptions(method, options, allowed...); err != nil {
			return nil, err
		}
		full := false
		if v, ok := options["full"]; ok {
			var err error
			if full, err = strconv.ParseBool(v); err != nil {
				return nil, fmt.Errorf("%s: invalid full %q: want true or false", method, v)
			}
		}
		id, err := blockTagOption(method, options, blockID)
		if err != nil {
			return nil, err
		}
		return simple{method, PerBlock, func(it Item, _ Params) []any { return []any{id(it), full} }}, nil
	}
}

func blockReceiptsGenerator(options map[string]string) (Generator, error) {
	const method = "eth_getBlockReceipts"
	if err := checkOptions(method, options, "tag"); err != nil {
		return nil, err
	}
	id, err := blockTagOption(method, options, func(it Item) any { return hexNum(it.BlockNum) })
	if err != nil {
		return nil, err
	}
	return simple{method, PerBlock, func(it Item, _ Params) []any { return []any{id(it)} }}, nil
}

// blockTagOption returns blockID, or with tag=latest the "latest" tag, resolved by each node when the
// request runs: every request then reads the head block, one per block read at generation.
func blockTagOption(method string, options map[string]string, blockID func(Item) any) (func(Item) any, error) {
	v, ok := options["tag"]
	if !ok {
		return blockID, nil
	}
	if v != string(TagLatest) {
		return nil, fmt.Errorf("%s: invalid tag %q: want %s", method, v, TagLatest)
	}
	return func(Item) any { return string(TagLatest) }, nil
}

// traceBlockGenerator builds a block trace whose "tracer" option selects the tracer (default: the struct logger).
func traceBlockGenerator(method string, blockID func(Item) any) func(map[string]string) (Generator, error) {
	return func(options map[string]string) (Generator, error) {
		if err := checkOptions(method, options, "tracer"); err != nil {
			return nil, err
		}
		tracer := options["tracer"]
		return simple{method, PerBlock, func(it Item, _ Params) []any {
			if tracer == "" {
				return []any{blockID(it)}
			}
			return []any{blockID(it), map[string]string{"tracer": tracer}}
		}}, nil
	}
}
