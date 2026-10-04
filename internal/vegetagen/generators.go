package vegetagen

import (
	"fmt"
	"slices"
	"strconv"
)

// Kind tells what a generator builds one request from.
type Kind int

const (
	PerTx    Kind = iota // one request per transaction
	PerBlock             // one request per block
	Constant             // the same request, with no parameters taken from the chain
)

func (k Kind) String() string {
	return [...]string{"per-tx", "per-block", "constant"}[k]
}

// Item is the chain data a request is built from: Txn is nil for PerBlock generators.
type Item struct {
	Block    *Block
	BlockNum uint64
	Txn      *Transaction
}

type Params struct {
	BlockParam string   // state to run on, for methods that take a block parameter
	Contracts  []string // contract filter, used by eth_getLogs as its address list
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
	"eth_getTransactionReceipt": noOptions(simple{"eth_getTransactionReceipt", PerTx, txHash}),
	"eth_getTransactionByHash":  noOptions(simple{"eth_getTransactionByHash", PerTx, txHash}),
	"eth_getBalance":            noOptions(simple{"eth_getBalance", PerTx, senderAtBlock}),
	"eth_getTransactionCount":   noOptions(simple{"eth_getTransactionCount", PerTx, senderAtBlock}),
	"eth_getBlockByNumber":      blockGenerator("eth_getBlockByNumber", func(it Item) any { return hexNum(it.BlockNum) }),
	"eth_getBlockByHash":        blockGenerator("eth_getBlockByHash", func(it Item) any { return it.Block.Hash }),
	"eth_getBlockReceipts":      noOptions(simple{"eth_getBlockReceipts", PerBlock, func(it Item, _ Params) []any { return []any{hexNum(it.BlockNum)} }}),
	"eth_getLogs":               noOptions(simple{"eth_getLogs", PerBlock, logsOfBlock}),
	"debug_traceBlockByHash":    traceBlockGenerator,
	"eth_blockNumber":           noOptions(simple{"eth_blockNumber", Constant, noParams}),
	"eth_chainId":               noOptions(simple{"eth_chainId", Constant, noParams}),
	"net_listening":             noOptions(simple{"net_listening", Constant, noParams}),
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

func logsOfBlock(it Item, p Params) []any {
	return []any{struct {
		FromBlock string   `json:"fromBlock"`
		ToBlock   string   `json:"toBlock"`
		Address   []string `json:"address,omitempty"`
	}{hexNum(it.BlockNum), hexNum(it.BlockNum), p.Contracts}}
}

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

// blockGenerator builds a block lookup whose "full" option selects full transactions.
func blockGenerator(method string, blockID func(Item) any) func(map[string]string) (Generator, error) {
	return func(options map[string]string) (Generator, error) {
		if err := checkOptions(method, options, "full"); err != nil {
			return nil, err
		}
		full := false
		if v, ok := options["full"]; ok {
			var err error
			if full, err = strconv.ParseBool(v); err != nil {
				return nil, fmt.Errorf("%s: invalid full %q: want true or false", method, v)
			}
		}
		return simple{method, PerBlock, func(it Item, _ Params) []any { return []any{blockID(it), full} }}, nil
	}
}

func traceBlockGenerator(options map[string]string) (Generator, error) {
	const method = "debug_traceBlockByHash"
	if err := checkOptions(method, options, "tracer"); err != nil {
		return nil, err
	}
	tracer := options["tracer"]
	return simple{method, PerBlock, func(it Item, _ Params) []any {
		if tracer == "" {
			return []any{it.Block.Hash}
		}
		return []any{it.Block.Hash, map[string]string{"tracer": tracer}}
	}}, nil
}
