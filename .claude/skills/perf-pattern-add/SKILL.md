---
name: perf-pattern-add
description: Use to add a performance pattern for rpc_perf, e.g. "I found a tx where the node is slow, where do I save it?", or to build a pattern from a block range or a profile, and to replay it with rpc_perf. Covers the tx lists of rpc_pattern_gen --txs, the per-tx results of rpc_perf -i, and where the files go.
allowed-tools: Bash, Read, Edit, Write, Glob, Grep
---

# Add a performance pattern

## Overview

A pattern is a `.jsonl` file, one JSON-RPC request per line, that `rpc_perf` replays at fixed rates.
`rpc_pattern_gen` builds it from a running node. There are three kinds:

| Kind | Built from | Use |
|------|-----------|-----|
| Tx list | chosen txs, `name hash` per line (`--txs`) | track a few txs where a node is slow, heavy or behaves oddly |
| Historical | a fixed block range (`--start-block`) | a reference dataset that never changes, for comparisons over time |
| Mix | a profile with methods and weights (`--profile`) | a realistic load on recent blocks |

Full option list: `perf/pattern/README.md` (rpc_pattern_gen) and `perf/README.md` (rpc_perf).

## Build

```bash
make rpc_pattern_gen rpc_perf
```

## Add a tx to a list

1. Pick the list of its kind in `perf/pattern/<network>/txs/` (e.g. `slow_vs_reth.txt`, `heavy.txt`), or
   create a new one for a new kind. Comment lines start with `#`; say where the txs come from.
2. Add one line `<name> <hash>`. The name is a free label, unique in the file: a sequence number and a
   short summary of the bottleneck, e.g. `17-ecrecover-330-110KB`. The number makes it easy to find in a
   long output; do not reuse numbers.
3. Check the tx exists on the node: an unknown or pending tx fails the generation.

```
# eth_call slower than on other clients
1-div-loop-28M 0x5a158d08067feeffe06fb1930c50c9f82b0c9175a22d6d110c190fe0dd47d1aa
2-sload-storm  0xea93481b5f1faf0c45f4569755b5cd8059df3fe2e47f89bca4ef25acaa1ebd7e
```

## Generate

Each tx is read from the node (`eth_getTransactionByHash`) and becomes one `eth_call` with its call
arguments; the request `id` is its name. The block parameter is `"latest"` with `--block-tag latest`, and
the block of the tx minus one with `parent`.

```bash
./build/bin/rpc_pattern_gen eth_call --txs slow.txt --block-tag latest --verify --out slow_latest.jsonl
./build/bin/rpc_pattern_gen eth_call --txs slow.txt --block-tag parent --verify --out slow_parent.jsonl
```

- `latest`: the calls run on the state at replay. Works on any node, also without archive, so it is the
  one for comparisons between clients. As the state moves some calls revert or take another branch:
  `--verify` drops them at generation and prints them with their error; regenerate when too many fail.
- `parent`: each call is pinned to the block before its tx, so the pattern never changes. Needs a node
  that still holds that state (an archive node for old txs). Not the original run: the call sees the
  previous block and misses the txs before it in its own block.

Historical and mix patterns:

```bash
./build/bin/rpc_pattern_gen eth_call --start-block 13000000 --end-block 13999999 --sample-blocks 1000 --per-block 1 \
    --block-tag parent --verify --out eth_call_13000000.jsonl
./build/bin/rpc_pattern_gen --profile perf/pattern/mainnet/config/mixed.profile --counts 10000 --verify --out mixed.jsonl
```

## Replay

```bash
./build/bin/rpc_perf -p slow_parent.jsonl -Z -t 50:30 -r 1 -i
./build/bin/rpc_perf -p mixed.jsonl -Z -t 1000:30 -r 1 -v
```

- `-i` prints one line per request id, i.e. per tx of a list, ordered by its number; `-v` one line per method.
- `-Z` skips the check that a process named `rpcdaemon` is running; without it a node with another
  process name fails with `server is Dead`.
- `-t qps:seconds,...` and `-r` set the load; the default sequence runs up to 10000 qps, 10 times.

## Use cases

### A tx where the node is slow: where do I save it?

Add it to the list of its kind (see "Add a tx to a list"), regenerate the patterns of that list and
replay them with `-i`: from then on the tx has its own line in every run.

### Find txs where eth_call is slower than on another client

1. Take candidates: the txs that use the most gas in some recent blocks, written as a list by
   `tx-list`, or chosen on a block explorer:

   ```bash
   ./build/bin/rpc_pattern_gen tx-list --top-gas 20 --blocks 100 --out candidates.txt
   ./build/bin/rpc_pattern_gen tx-list --top-gas 20 --start-block 26200000 --end-block 26200999 --out candidates.txt
   ```

   The first reads the last 100 blocks, the second every block of a range; `--sample-blocks` reads only
   some blocks of a wide range. The whole set is ranked: the 20 heaviest txs of all the blocks are written.

2. Put them in a list, generate with `--block-tag latest`, and replay the same `.jsonl` with `-i` on each
   client, one at a time, with the same `-t` and `-r` (see the rpc-perf-run skill).
3. Keep the txs whose p50 is much higher on the node under test. Profile them (pprof) and keep the ones
   with different bottlenecks; give each a name that says the bottleneck, e.g. `5-sload-storm`.

### Txs whose p99 is much worse than their p50

Replay a list with `-i -P` at a low and a higher rate, e.g. `-t 50:30,500:30`. A tx whose p99 grows much
more than its p50 when the rate goes up points to contention (locks) or to disk reads; save it in a
`p99_spikes.txt` list.

### Popular contracts

A profile can keep only the txs sent to some contracts: list them in its `[contracts]` section, as
`perf/pattern/mainnet/config/mixed.profile` does for USDT, USDC and the Uniswap routers. To track single
txs on those contracts, add them to a `popular_contracts.txt` list instead.

### Heavy txs

Keep them in a `heavy.txt` list and compare them per tx with `-i`. Their latency grows with the gas they
use; a latency per gas (Mgas/s) is not reported yet.

### Defeat the block caches

Read more blocks than the caches of the nodes hold, drawn at random from a wide range, so most requests
miss the caches:

```bash
./build/bin/rpc_pattern_gen eth_call --start-block 19000000 --end-block 19999999 --sample-blocks 10000 \
    --block-tag parent --verify --out eth_call_19000000.jsonl
```

## Workflow

1. **Confirm the inputs**: the network, the node URL, which list (or range, or profile), and the block tag.
   Ask if not known.
2. **Safety checks**
   - The node answers `eth_blockNumber` on its URL.
   - For `parent` or a block range, the blocks are below the head and the node still holds their state.
   - Do not overwrite an existing historical pattern: a new dataset goes into a new file.
3. **Generate** with `--verify` and read the summary: the requests written and the txs dropped.
4. **Replay** a short run (`-t 50:30 -r 1`) and check the success rate and the errors.
5. **Save**: the lists go in `perf/pattern/<network>/txs/` (e.g. `perf/pattern/mainnet/txs/heavy.txt`), and
   the `parent` pattern of a list next to it as `<list>_parent.jsonl`. A `latest` pattern drifts with the
   state, so it is generated from its list when needed rather than saved. A historical pattern of a block
   range goes in `perf/pattern/<network>/`, next to the other patterns, as `<api>_<block>.jsonl` with the
   full first block (e.g. `eth_getLogs_24100000.jsonl`), and is not changed afterwards.
