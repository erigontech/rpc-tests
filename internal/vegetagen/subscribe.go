package vegetagen

import (
	"errors"
	"fmt"
	"strings"
)

// Subscription types of the type option of eth_subscribe.
const (
	SubscribeNewHeads            = "newHeads"
	SubscribeLogsType            = "logs"
	SubscribeTransactionReceipts = "transactionReceipts"
)

// FilterAll is the filter option of a logs subscription that receives every log.
const FilterAll = "all"

// SubscribeLogs turns a log into a logs subscription whose filter matches it, of a shape of the family
// Filter (all shapes when empty), as GetLogs but with no block range.
type SubscribeLogs struct {
	Filter string
}

type subscribeFilter struct {
	Address any   `json:"address,omitempty"`
	Topics  []any `json:"topics,omitempty"`
}

func (SubscribeLogs) Method() string { return "eth_subscribe" }
func (SubscribeLogs) Kind() Kind     { return PerLog }

func (g SubscribeLogs) Request(it Item, _ Params) ([]byte, error) {
	address, topics, err := matchLog(logRand(it.BlockNum, it.Log), it.Log, g.Filter)
	if err != nil {
		return nil, err
	}
	return marshalRequest("eth_subscribe", SubscribeLogsType, subscribeFilter{Address: address, Topics: topics})
}

func subscribeGenerator(options map[string]string) (Generator, error) {
	const method = "eth_subscribe"
	typ := options["type"]
	if typ == "" {
		typ = SubscribeNewHeads
	}
	switch typ {
	case SubscribeNewHeads, SubscribeTransactionReceipts:
		if err := checkOptions(method+" type="+typ, options, "type"); err != nil {
			return nil, err
		}
		return simple{method, Constant, func(Item, Params) []any { return []any{typ} }}, nil
	case SubscribeLogsType:
		if err := checkOptions(method+" type="+typ, options, "type", "filter"); err != nil {
			return nil, err
		}
		switch filter := options["filter"]; filter {
		case FilterAll:
			return simple{method, Constant, func(Item, Params) []any { return []any{typ, struct{}{}} }}, nil
		case "", FilterAddress, FilterAddressTopics, FilterTopics:
			return SubscribeLogs{Filter: filter}, nil
		default:
			return nil, fmt.Errorf("%s: invalid filter %q: want %s, %s, %s or %s", method, filter, FilterAddress, FilterAddressTopics, FilterTopics, FilterAll)
		}
	default:
		return nil, fmt.Errorf("%s: invalid type %q: want %s, %s or %s", method, typ, SubscribeNewHeads, SubscribeLogsType, SubscribeTransactionReceipts)
	}
}

// checkSubscriptions rejects the subscriptions of specs that cfg cannot write or verify: they are sent
// over a WebSocket, so they go only to a .jsonl, alone, and cannot be run on the node over HTTP.
func checkSubscriptions(cfg Config, specs []Spec) error {
	subs := 0
	for _, sp := range specs {
		if sp.Gen.Method() == "eth_subscribe" {
			subs++
		}
	}
	switch {
	case subs == 0:
		return nil
	case subs < len(specs):
		return errors.New("eth_subscribe cannot be mixed with other methods: it runs over a WebSocket")
	case !strings.HasSuffix(cfg.Out, ".jsonl"):
		return errors.New("eth_subscribe needs a .jsonl output: a vegeta tar holds HTTP requests")
	case cfg.Verify:
		return errors.New("eth_subscribe cannot be verified: it needs a WebSocket, verify runs over HTTP")
	}
	return nil
}
