package vegetagen

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// maxDescribedMethods is the number of methods of a mix the description names, the heaviest first.
const maxDescribedMethods = 6

// Description returns the description of a pattern, built from its parameters only, so that every run of
// the same command describes its pattern the same way whatever the head of the node. profile is the profile
// of the pattern, nil for a subcommand; txList is the base name of the tx list of --txs.
func Description(cfg Config, specs []Spec, profile *Profile, txList string) string {
	if profile == nil {
		return commandDescription(cfg, specs[0], txList)
	}
	head := profile.Description
	if head == "" {
		names := make([]string, len(specs))
		for i, sp := range specs {
			names[i] = sp.name()
		}
		head = strings.Join(names, ", ")
	}
	groups := []string{head}
	if len(specs) > 1 {
		groups = append(groups, methodShares(specs))
	}
	if len(profile.Contracts) > 0 {
		what := "txs and logs"
		if !slices.ContainsFunc(specs, func(sp Spec) bool { return sp.Gen.Kind() != PerLog }) {
			what = "logs"
		}
		groups = append(groups, fmt.Sprintf("only the %s of %s", what, contractNames(profile)))
	}
	var last []string
	switch {
	case cfg.Range != nil:
		last = append(last, "from "+rangeText(*cfg.Range))
	case slices.ContainsFunc(specs, usesWindow):
		window := cfg.MaxBlocks
		if window == 0 {
			window = DefaultMaxBlocks
		}
		last = append(last, fmt.Sprintf("from the %d most recent blocks", window))
	}
	last = append(last, stateParts(cfg, specs)...)
	if len(last) > 0 {
		groups = append(groups, strings.Join(last, ", "))
	}
	return strings.Join(groups, "; ")
}

// commandDescription describes the pattern of a subcommand: one method read from the head, a block range or a tx list.
func commandDescription(cfg Config, sp Spec, txList string) string {
	method := sp.Gen.Method()
	parts := []string{}
	switch g := sp.Gen.(type) {
	case GetLogs:
		parts = append(parts, fmt.Sprintf("%s with filters built from the real logs of %s", method, blockSource(cfg)),
			filterText(g), fmt.Sprintf("ranges up to %d blocks", g.Range))
		if cfg.PerBlock > 0 {
			parts = append(parts, fmt.Sprintf("%d logs per block", cfg.PerBlock))
		}
	default:
		source := blockSource(cfg)
		if txList != "" {
			source = txList
		}
		parts = append(parts, fmt.Sprintf("%s of the txs of %s", method, source))
		if cfg.PerBlock > 0 {
			parts = append(parts, fmt.Sprintf("%d txs per block", cfg.PerBlock))
		}
	}
	return strings.Join(append(parts, stateParts(cfg, []Spec{sp})...), ", ")
}

// blockSource names the blocks a subcommand reads.
func blockSource(cfg Config) string {
	switch {
	case cfg.Range != nil:
		return rangeText(*cfg.Range)
	case cfg.Blocks > 0:
		return fmt.Sprintf("the %d most recent blocks", cfg.Blocks)
	}
	return "the most recent blocks"
}

func rangeText(r BlockRange) string {
	last, err := r.last()
	if err != nil {
		return fmt.Sprintf("the blocks from %d", r.From)
	}
	if r.Sample > 0 {
		return fmt.Sprintf("%d blocks sampled from blocks %d-%d", r.Sample, r.From, last)
	}
	return fmt.Sprintf("blocks %d-%d", r.From, last)
}

func filterText(g GetLogs) string {
	text := map[string]string{
		"":                  "every filter shape (address, address and topics, topics only)",
		FilterAddress:       "address filter only",
		FilterAddressTopics: "address and topics filters",
		FilterTopics:        "topics only filters, no address",
	}[g.Filter]
	if g.Or > 1 {
		text += fmt.Sprintf(", up to %d alternatives per address and topic", g.Or)
	}
	return text
}

// methodShares lists the heaviest methods of a mix with their share of the requests.
func methodShares(specs []Spec) string {
	total := 0.0
	for _, sp := range specs {
		total += sp.Weight
	}
	sorted := slices.Clone(specs)
	slices.SortStableFunc(sorted, func(a, b Spec) int {
		switch {
		case a.Weight > b.Weight:
			return -1
		case a.Weight < b.Weight:
			return 1
		}
		return 0
	})
	var parts []string
	for _, sp := range sorted[:min(len(sorted), maxDescribedMethods)] {
		parts = append(parts, fmt.Sprintf("%s %.1f%%", sp.name(), 100*sp.Weight/total))
	}
	if len(sorted) > maxDescribedMethods {
		parts = append(parts, fmt.Sprintf("... %d methods", len(sorted)))
	}
	return strings.Join(parts, ", ")
}

func contractNames(p *Profile) string {
	names := make([]string, len(p.Contracts))
	for i, addr := range p.Contracts {
		names[i] = addr
		if i < len(p.ContractNames) && p.ContractNames[i] != "" {
			names[i] = p.ContractNames[i]
		}
	}
	return strings.Join(names, ", ")
}

// usesWindow tells whether a method of a profile draws its requests from the whole window of recent blocks:
// not when it has its own blocks option, queries the latest block or takes no parameters from the chain.
func usesWindow(sp Spec) bool {
	if g, ok := sp.Gen.(GetLogs); ok && g.Latest {
		return false
	}
	return sp.Blocks == 0 && sp.Gen.Kind() != Constant
}

// The methods whose requests take the block tag, and those that copy the gas price of the tx.
var (
	stateMethods = []string{"eth_call", "eth_estimateGas", "eth_createAccessList", "eth_getBalance", "eth_getTransactionCount"}
	feeMethods   = []string{"eth_call", "eth_estimateGas", "eth_createAccessList"}
)

// stateParts returns the block tag and the fees of the requests, for the methods that take them.
func stateParts(cfg Config, specs []Spec) []string {
	uses := func(methods []string) bool {
		return slices.ContainsFunc(specs, func(sp Spec) bool { return slices.Contains(methods, sp.Gen.Method()) })
	}
	var parts []string
	if uses(stateMethods) {
		parts = append(parts, "block tag "+string(cfg.Tag))
	}
	if uses(feeMethods) {
		if cfg.KeepFees {
			parts = append(parts, "tx fees")
		} else {
			parts = append(parts, "no fees")
		}
	}
	return parts
}

// chainNames are the networks of the integration tests by chain id.
var chainNames = map[uint64]string{
	1:        "mainnet",
	100:      "gnosis",
	137:      "polygon-pos",
	17000:    "holesky",
	421614:   "arb-sepolia",
	560048:   "hoodi",
	11155111: "sepolia",
}

// Network returns the name of the network of the node at url, "chain <id>" for an unknown one, or "" when
// the node does not answer eth_chainId.
func Network(ctx context.Context, url string) string {
	result := callString(ctx, url, "eth_chainId")
	if result == "" {
		return ""
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(result, "0x"), 16, 64)
	if err != nil {
		return ""
	}
	if name, ok := chainNames[id]; ok {
		return name
	}
	return fmt.Sprintf("chain %d", id)
}

// callString calls a method without parameters that returns a string, "" on any error.
func callString(ctx context.Context, url, method string) string {
	n := newNode(url)
	req, err := marshalRequest(method)
	if err != nil {
		return ""
	}
	var resp struct {
		Result string    `json:"result"`
		Error  *rpcError `json:"error"`
	}
	if _, err := n.client.Call(ctx, n.target, req, &resp); err != nil || resp.Error != nil {
		return ""
	}
	return resp.Result
}
