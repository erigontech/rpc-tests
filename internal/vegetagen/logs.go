package vegetagen

import (
	"context"
	"fmt"
	"hash/fnv"
	"maps"
	"math"
	"math/rand"
	"strconv"
	"strings"
)

// Log is a log of a block, the source of an eth_getLogs filter that matches it.
type Log struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	TxHash  string   `json:"transactionHash"`
	Index   string   `json:"logIndex"`
	Removed bool     `json:"removed"`
}

// DefaultLogsRange is the widest block range of a generated eth_getLogs.
const DefaultLogsRange = 100

// GetLogs turns a log into an eth_getLogs whose filter matches it, so the result is never empty.
// The filter shape (address, address and topics, topics only) and the block range, up to Range
// blocks around the log's block and never past the head, vary with the log, deterministically.
// The range is in block numbers: the requests keep their results when run some blocks later.
// Filter, when set, restricts the shapes to one family: address, address+topics or topics.
// Or, above 1, turns the address and each topic of the filter into a list of up to Or alternatives:
// the log's value and others of the same block at the same position.
type GetLogs struct {
	Range  uint64
	Filter string
	Or     int
}

// Log filter families of the filter option; the empty one allows every shape.
const (
	FilterAddress       = "address"
	FilterAddressTopics = "address+topics"
	FilterTopics        = "topics"
)

func (GetLogs) Method() string { return "eth_getLogs" }
func (GetLogs) Kind() Kind     { return PerLog }

type logFilter struct {
	FromBlock string `json:"fromBlock"`
	ToBlock   string `json:"toBlock"`
	Address   any    `json:"address,omitempty"` // one address or a list of alternatives
	Topics    []any  `json:"topics,omitempty"`  // a nil entry matches any topic
}

func (g GetLogs) Request(it Item, p Params) ([]byte, error) {
	lg := it.Log
	h := fnv.New64a()
	fmt.Fprintf(h, "%d/%s/%s", it.BlockNum, lg.TxHash, lg.Index)
	rng := rand.New(rand.NewSource(int64(h.Sum64()))) //nolint:gosec

	// Log-uniform width: mostly narrow ranges, some up to Range blocks.
	width := logUniform(rng, max(g.Range, 1))
	before := uint64(rng.Int63n(int64(width)))
	after := width - 1 - before
	if it.BlockNum+after > p.Head { // the blocks past the head do not exist yet
		after = p.Head - it.BlockNum
	}
	before = min(before, it.BlockNum)
	f := logFilter{FromBlock: hexNum(it.BlockNum - before), ToBlock: hexNum(it.BlockNum + after)}

	shapes := logShapes(lg.Topics, g.Filter)
	if len(shapes) == 0 {
		return nil, fmt.Errorf("log %s/%s has too few topics for filter %s", lg.TxHash, lg.Index, g.Filter)
	}
	switch shapes[rng.Intn(len(shapes))] {
	case "address":
		f.Address = lg.Address
	case "address+topic0":
		f.Address, f.Topics = lg.Address, []any{lg.Topics[0]}
	case "address+topic0+topic1":
		f.Address, f.Topics = lg.Address, []any{lg.Topics[0], lg.Topics[1]}
	case "address+topic0+topic2":
		f.Address, f.Topics = lg.Address, []any{lg.Topics[0], nil, lg.Topics[2]}
	case "topic0+topic1":
		f.Topics = []any{lg.Topics[0], lg.Topics[1]}
	case "topic0+topic2":
		f.Topics = []any{lg.Topics[0], nil, lg.Topics[2]}
	}
	if g.Or > 1 {
		if f.Address != nil {
			f.Address = orList(rng, g.Or, lg.Address, blockValues(it.Block, -1, p.Contracts))
		}
		for pos, v := range f.Topics {
			if v != nil {
				f.Topics[pos] = orList(rng, g.Or, lg.Topics[pos], blockValues(it.Block, pos, p.Contracts))
			}
		}
	}
	return marshalRequest("eth_getLogs", f)
}

// logUniform returns a number in [1, n], mostly small, a few up to n.
func logUniform(rng *rand.Rand, n uint64) uint64 {
	return max(uint64(math.Round(math.Pow(float64(n), rng.Float64()))), 1)
}

// orList returns value alone or, with a longer draw of up to maxLen, a list of value and other
// distinct values of pool.
func orList(rng *rand.Rand, maxLen int, value string, pool []string) any {
	k := int(logUniform(rng, uint64(maxLen)))
	if k == 1 {
		return value
	}
	others := make([]string, 0, len(pool))
	for _, v := range pool {
		if !strings.EqualFold(v, value) {
			others = append(others, v)
		}
	}
	rng.Shuffle(len(others), func(i, j int) { others[i], others[j] = others[j], others[i] })
	return append([]string{value}, others[:min(k-1, len(others))]...)
}

// blockValues returns the distinct addresses (pos -1) or topics at pos of the logs of b, in block order,
// keeping only the logs of contracts when it is not empty.
func blockValues(b *Block, pos int, contracts map[string]bool) []string {
	if b == nil {
		return nil
	}
	if b.values == nil || !maps.Equal(b.valuesContracts, contracts) {
		b.values, b.valuesContracts = map[int][]string{}, contracts
	}
	if v, ok := b.values[pos]; ok {
		return v
	}
	seen := map[string]bool{}
	var out []string
	for _, lg := range blockLogs(b.Logs, contracts) {
		v := lg.Address
		if pos >= 0 {
			if pos >= len(lg.Topics) {
				continue
			}
			v = lg.Topics[pos]
		}
		if seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	b.values[pos] = out
	return out
}

// logShapes returns the filter shapes of the family filter (all when empty) that a log with topics
// can match. A filter on topic0 alone is left out: an event signature such as Transfer matches
// too many logs across contracts.
func logShapes(topics []string, filter string) []string {
	address := filter == "" || filter == FilterAddress
	addressTopics := filter == "" || filter == FilterAddressTopics
	onlyTopics := filter == "" || filter == FilterTopics
	var shapes []string
	if address {
		shapes = append(shapes, "address")
	}
	if addressTopics && len(topics) >= 1 {
		shapes = append(shapes, "address+topic0")
	}
	if addressTopics && len(topics) >= 2 {
		shapes = append(shapes, "address+topic0+topic1")
	}
	if onlyTopics && len(topics) >= 2 {
		shapes = append(shapes, "topic0+topic1")
	}
	if addressTopics && len(topics) >= 3 {
		shapes = append(shapes, "address+topic0+topic2")
	}
	if onlyTopics && len(topics) >= 3 {
		shapes = append(shapes, "topic0+topic2")
	}
	return shapes
}

// usableLog tells whether gen can build a request from lg: a filter family can need topics the log lacks.
func usableLog(gen Generator, lg Log) bool {
	g, ok := gen.(GetLogs)
	return !ok || len(logShapes(lg.Topics, g.Filter)) > 0
}

func getLogsGenerator(options map[string]string) (Generator, error) {
	const method = "eth_getLogs"
	if err := checkOptions(method, options, "range", "filter", "or"); err != nil {
		return nil, err
	}
	g := GetLogs{Range: DefaultLogsRange, Or: 1}
	if v, ok := options["range"]; ok {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("%s: invalid range %q: want a positive number of blocks", method, v)
		}
		g.Range = n
	}
	if v, ok := options["or"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%s: invalid or %q: want a positive number of alternatives", method, v)
		}
		g.Or = n
	}
	if v, ok := options["filter"]; ok {
		switch v {
		case FilterAddress, FilterAddressTopics, FilterTopics:
			g.Filter = v
		default:
			return nil, fmt.Errorf("%s: invalid filter %q: want %s, %s or %s", method, v, FilterAddress, FilterAddressTopics, FilterTopics)
		}
	}
	return g, nil
}

// blockLogs returns the logs of a block a generator can draw from: not removed, emitted by one of
// contracts when set, and one per address and topics, as identical logs give identical filters.
func blockLogs(logs []Log, contracts map[string]bool) []Log {
	seen := map[string]bool{}
	var out []Log
	for _, lg := range logs {
		if lg.Removed || (len(contracts) > 0 && !contracts[strings.ToLower(lg.Address)]) {
			continue
		}
		key := strings.ToLower(lg.Address) + "/" + strings.Join(lg.Topics, "/")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, lg)
	}
	return out
}

// fetchLogs reads the logs of every block into its Logs.
func (n node) fetchLogs(ctx context.Context, blocks []Block, nums []uint64, workers int) error {
	return parallel(len(blocks), workers, func(i int) error {
		var resp struct {
			Result []Log     `json:"result"`
			Error  *rpcError `json:"error"`
		}
		req, err := marshalRequest("eth_getLogs", logFilter{FromBlock: hexNum(nums[i]), ToBlock: hexNum(nums[i])})
		if err != nil {
			return err
		}
		if _, err := n.client.Call(ctx, n.target, req, &resp); err != nil {
			return fmt.Errorf("reading logs of block %d: %w", nums[i], err)
		}
		if resp.Error != nil {
			return fmt.Errorf("reading logs of block %d: %s", nums[i], resp.Error.Message)
		}
		blocks[i].Logs = resp.Result
		return nil
	})
}

func needsLogs(specs []Spec) bool {
	for _, sp := range specs {
		if sp.Gen.Kind() == PerLog {
			return true
		}
	}
	return false
}
