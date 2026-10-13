package vegetagen

import (
	"context"
	"fmt"
	"hash/fnv"
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
type GetLogs struct {
	Range uint64
}

func (GetLogs) Method() string { return "eth_getLogs" }
func (GetLogs) Kind() Kind     { return PerLog }

type logFilter struct {
	FromBlock string `json:"fromBlock"`
	ToBlock   string `json:"toBlock"`
	Address   string `json:"address,omitempty"`
	Topics    []any  `json:"topics,omitempty"` // a nil entry matches any topic
}

func (g GetLogs) Request(it Item, p Params) ([]byte, error) {
	lg := it.Log
	h := fnv.New64a()
	fmt.Fprintf(h, "%d/%s/%s", it.BlockNum, lg.TxHash, lg.Index)
	rng := rand.New(rand.NewSource(int64(h.Sum64()))) //nolint:gosec

	// Log-uniform width: mostly narrow ranges, some up to Range blocks.
	width := uint64(math.Round(math.Pow(float64(max(g.Range, 1)), rng.Float64())))
	width = max(width, 1)
	before := uint64(rng.Int63n(int64(width)))
	after := width - 1 - before
	if it.BlockNum+after > p.Head { // the blocks past the head do not exist yet
		after = p.Head - it.BlockNum
	}
	before = min(before, it.BlockNum)
	f := logFilter{FromBlock: hexNum(it.BlockNum - before), ToBlock: hexNum(it.BlockNum + after)}

	shapes := logShapes(lg.Topics)
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
	return marshalRequest("eth_getLogs", f)
}

// logShapes returns the filter shapes that a log with topics can match. A filter on topic0 alone
// is left out: an event signature such as Transfer matches too many logs across contracts.
func logShapes(topics []string) []string {
	shapes := []string{"address"}
	if len(topics) >= 1 {
		shapes = append(shapes, "address+topic0")
	}
	if len(topics) >= 2 {
		shapes = append(shapes, "address+topic0+topic1", "topic0+topic1")
	}
	if len(topics) >= 3 {
		shapes = append(shapes, "address+topic0+topic2", "topic0+topic2")
	}
	return shapes
}

func getLogsGenerator(options map[string]string) (Generator, error) {
	const method = "eth_getLogs"
	if err := checkOptions(method, options, "range"); err != nil {
		return nil, err
	}
	g := GetLogs{Range: DefaultLogsRange}
	if v, ok := options["range"]; ok {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("%s: invalid range %q: want a positive number of blocks", method, v)
		}
		g.Range = n
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
