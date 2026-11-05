package vegetagen

import (
	"strings"
	"testing"
)

func TestProfileDescription(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"paragraph up to Usage", "# eth_getLogs on one block\n# at a time. Usage:\n#   rpc_pattern_gen ...\n[methods]\neth_getLogs 1\n", "eth_getLogs on one block at a time"},
		{"paragraph up to an empty comment line", "#\n# Calls only.\n#\n# rpc_perf -p x.jsonl\n[methods]\neth_call 1\n", "Calls only"},
		{"paragraph up to a blank line", "# Calls only\n\n# a note\n[methods]\neth_call 1\n", "Calls only"},
		{"no leading comment", "[methods]\neth_call 1  # the calls\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseProfile(strings.NewReader(tc.text))
			if err != nil {
				t.Fatal(err)
			}
			if p.Description != tc.want {
				t.Errorf("description %q, want %q", p.Description, tc.want)
			}
		})
	}
}

func TestProfileContractNames(t *testing.T) {
	p, err := ParseProfile(strings.NewReader("[methods]\neth_call 1\n[contracts]\n0xdac17f958d2ee523a2206206994597c13d831ec7  Tether USD  # 14% of txs\n0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := contractNames(&p); got != "Tether USD, 0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48" {
		t.Errorf("contract names %q", got)
	}
}

func TestDescription(t *testing.T) {
	mixed := "# Mixed load. Usage:\n[methods]\neth_call 50\neth_getLogs 20 filter=address range=100\ndebug_traceBlockByHash 10 tracer=callTracer blocks=50\n" +
		"eth_blockNumber 10\nnet_listening 5\neth_chainId 3\neth_gasPrice 2\n[contracts]\n0xdac17f958d2ee523a2206206994597c13d831ec7  USDT\n"
	for _, tc := range []struct {
		name    string
		cfg     Config
		profile string // empty for a subcommand
		specs   []Spec
		txList  string
		want    string
	}{
		{
			name:  "eth_call from the head",
			cfg:   Config{Tag: TagLatest, Count: 10},
			specs: []Spec{{Gen: EthCall{}, Weight: 1}},
			want:  "eth_call of the txs of the most recent blocks, block tag latest, no fees",
		}, {
			name:  "eth_createAccessList of a sampled range with the tx fees",
			cfg:   Config{Tag: TagHead, KeepFees: true, Range: &BlockRange{From: 100, Len: 50, Sample: 10}, PerBlock: 2},
			specs: []Spec{{Gen: CreateAccessList{}, Weight: 1}},
			want:  "eth_createAccessList of the txs of 10 blocks sampled from blocks 100-149, 2 txs per block, block tag head, tx fees",
		}, {
			name:   "eth_call of a tx list",
			cfg:    Config{Tag: TagLatest},
			specs:  []Spec{{Gen: EthCall{}, Weight: 1}},
			txList: "slow.txt",
			want:   "eth_call of the txs of slow.txt, block tag latest, no fees",
		}, {
			name:  "eth_getLogs from the head",
			cfg:   Config{Tag: TagLatest, Count: 10},
			specs: []Spec{{Gen: GetLogs{Range: 100}, Weight: 1}},
			want:  "eth_getLogs with filters built from the real logs of the most recent blocks, every filter shape (address, address and topics, topics only), ranges up to 100 blocks",
		}, {
			name:  "eth_getBlockByNumber with options",
			cfg:   Config{Tag: TagLatest, Blocks: 5},
			specs: []Spec{{Name: "eth_getBlockByNumber full=true tag=latest", Gen: mustGenerator(t, "eth_getBlockByNumber", map[string]string{"full": "true", "tag": "latest"}), Weight: 1}},
			want:  "eth_getBlockByNumber full=true tag=latest, one per block of the 5 most recent blocks",
		}, {
			name:  "eth_gasPrice",
			cfg:   Config{Tag: TagLatest, Count: 10},
			specs: []Spec{{Gen: mustGenerator(t, "eth_gasPrice", nil), Weight: 1}},
			want:  "eth_gasPrice",
		}, {
			name:    "mix with contracts",
			cfg:     Config{Tag: TagLatest, MaxBlocks: 2500},
			profile: mixed,
			want: "Mixed load; eth_call 50.0%, eth_getLogs filter=address range=100 20.0%, debug_traceBlockByHash tracer=callTracer blocks=50 10.0%, " +
				"eth_blockNumber 10.0%, net_listening 5.0%, eth_chainId 3.0%, ... 7 methods; only the txs and logs of USDT; " +
				"from the 2500 most recent blocks, block tag latest, no fees",
		}, {
			name:    "logs of the latest block",
			cfg:     Config{Tag: TagLatest, MaxBlocks: 3000},
			profile: "# Polling the head\n[methods]\neth_getLogs 1 tag=latest filter=address\n[contracts]\n0xdac17f958d2ee523a2206206994597c13d831ec7  USDT\n",
			want:    "Polling the head; only the logs of USDT",
		}, {
			name:    "method with its own blocks",
			cfg:     Config{Tag: TagLatest, MaxBlocks: 3000},
			profile: "# Following the head\n[methods]\neth_getLogs 1 range=1 blocks=100\n",
			want:    "Following the head",
		}, {
			name:    "profile without a description, over a block range",
			cfg:     Config{Tag: TagHead, Range: &BlockRange{From: 10, To: 20}},
			profile: "[methods]\ndebug_traceBlockByNumber 1 tracer=callTracer\n",
			want:    "debug_traceBlockByNumber tracer=callTracer; from blocks 10-20",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			specs := tc.specs
			var profile *Profile
			if tc.profile != "" {
				p, err := ParseProfile(strings.NewReader(tc.profile))
				if err != nil {
					t.Fatal(err)
				}
				if specs, err = p.Specs(); err != nil {
					t.Fatal(err)
				}
				profile = &p
			}
			if got := Description(tc.cfg, specs, profile, tc.txList); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func mustGenerator(t *testing.T, method string, options map[string]string) Generator {
	t.Helper()
	gen, err := NewGenerator(method, options)
	if err != nil {
		t.Fatal(err)
	}
	return gen
}
