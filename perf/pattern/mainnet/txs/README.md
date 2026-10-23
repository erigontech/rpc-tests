# Mainnet tx lists

Lists of chosen mainnet txs for `rpc_pattern_gen eth_call --txs`, one file per kind of tx. Each one is
replayed with `rpc_perf -i`, so every tx has its own line in the results (see
[`../../README.md`](../../README.md#tx-lists)).

| List | Txs | `parent` pattern |
|------|-----|------------------|
| `heavy.txt` | heavy calls: the named rows of the RPC rps dataset | `heavy_parent.jsonl` (15 calls) |
| `slow_vs_reth.txt` | calls slower than on reth, from blocks near 26.06M | not yet: needs an archive node past them |

## Format

One `<name> <hash>` per line; `#` starts a comment. The name is a free label, unique in the file: a sequence
number and a short summary of the bottleneck, e.g. `17-ecrecover-330-110KB`. Do not reuse numbers.

## Add a tx

Add a line to the list of its kind, or start a new list for a new kind, with a comment that says where its
txs come from. `rpc_pattern_gen tx-list` writes candidates in this format.

## Generate and replay

```bash
./build/bin/rpc_pattern_gen eth_call --txs perf/pattern/mainnet/txs/heavy.txt --block-tag parent --verify \
    --out perf/pattern/mainnet/txs/heavy_parent.jsonl
./build/bin/rpc_perf -p perf/pattern/mainnet/txs/heavy_parent.jsonl -Z -t 50:30 -r 1 -i
```

A `parent` pattern is pinned to the block before each tx, so it does not change: it can be saved here,
next to its list, as `<list>_parent.jsonl`. It needs a node that holds those blocks (an archive node for
old txs). A `latest` pattern runs on the state at replay and drifts with it, so it is generated from its
list when needed.
