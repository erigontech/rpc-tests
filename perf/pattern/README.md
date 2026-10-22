# Vegeta patterns and `rpc_pattern_gen`

`rpc_perf` replays a Vegeta pattern: a tar file holding the JSON-RPC requests to send. This directory
keeps the patterns of each network (`<network>/stress_test_*.tar`) and the profiles used to generate new
ones (`<network>/config/*.profile`).

`rpc_pattern_gen` builds a pattern from the recent blocks of a running node, or from a range of blocks
(see [Block range](#block-range)): it reads blocks from the head, turns their transactions, blocks or logs into requests, optionally runs each request on the node
to keep only the working ones, and writes the tar that `rpc_perf --pattern-file` reads, or a
[JSONL file](#jsonl-output) with the plain requests.

## Build

```bash
make rpc_pattern_gen rpc_perf rpc_pattern_convert
# or
go build -o ./build/bin/rpc_pattern_gen ./cmd/patterngen
go build -o ./build/bin/rpc_perf ./cmd/perf
go build -o ./build/bin/rpc_pattern_convert ./cmd/patternconv
```

## Workflow

```bash
# 1. Generate the pattern from a synced node
./build/bin/rpc_pattern_gen --profile perf/pattern/mainnet/config/mixed.profile \
    --url http://localhost:8545 --counts 400000 --verify --out mixed.tar

# 2. Replay it with rpc_perf: -y is the name of the vegeta file in the tar (see --name)
./build/bin/rpc_perf -p mixed.tar -y mixed -Z -t 1000:30,2000:30
```

The tar holds a single file, `erigon_stress_test/vegeta_erigon_<name>.txt`, one Vegeta target per
request. `<name>` is `--name` when set, otherwise the method for a single method or `mixed` for several,
and is the value to pass to `rpc_perf --test-type` (`-y`). Each target posts to `--target-url`
(default `http://localhost:8545`); `rpc_perf` replaces `localhost` with its `--rpc-client-address`.

## JSONL output

When `--out` ends in `.jsonl`, `rpc_pattern_gen` writes the same requests, in the same order, as plain
JSONL: one JSON-RPC request per line, with no Vegeta target, URL, header or base64. The file can be read,
grepped and diffed as it is, and `rpc_perf -p` replays it directly (see [`../README.md`](../README.md#pattern-files)),
as can any other load tool.

```bash
rpc_pattern_gen eth_call --counts 400000 --block-tag parent --verify --out eth_call.jsonl
rpc_pattern_gen --profile perf/pattern/mainnet/config/mixed.profile --counts 400000 --verify --out mixed.jsonl
./build/bin/rpc_perf -p mixed.jsonl -Z -t 1000:30,2000:30
```

```
{"jsonrpc":"2.0","method":"eth_call","params":[{"from":"0x...","to":"0x...","gas":"0x5208","value":"0x0"},"0x12a05f1"],"id":1}
{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x12a05f2",false],"id":1}
```

`--target-url` and `--name` describe the Vegeta tar only, so they are rejected with a `.jsonl` output.

## Two ways to run it

### Subcommands: one method, from the head downwards

```bash
rpc_pattern_gen eth_call    --counts 400000 --block-tag latest --verify --out eth_call.tar
rpc_pattern_gen eth_getLogs --counts 400000 --range 100        --verify --out eth_getLogs.tar
```

The blocks are read from the head downwards, each one once, until `--counts` requests are written or
`--blocks` blocks are read. `eth_getLogs` takes `--range`, the widest block range of a request (default
100); the other eth_getLogs options are available through a profile only.

Every tx (or log, for eth_getLogs) of a block becomes a request, so consecutive requests share the block
and its state, and a low `--counts` uses only a few blocks. `--per-block N` takes at most N txs or logs of
each block, picked at random with the block number as seed, so the same command writes the same pattern.
With `--sample-blocks` it spreads the requests over the whole range, e.g. one `eth_call` on each of 1000
random blocks between 13M and 14M:

```bash
rpc_pattern_gen eth_call --start-block 13000000 --end-block 13999999 --sample-blocks 1000 --per-block 1 \
    --block-tag parent --verify --out eth_call_13000000.jsonl
```

The blocks are read from the highest down and the run stops at `--counts`, so to cover the whole range
set `--sample-blocks` to the number of blocks wanted and leave `--counts` unset. A profile already draws
its requests at random from the whole window, so `--per-block` is rejected with `--profile`.

### Profiles: a mix of methods drawn from a window of recent blocks

```bash
rpc_pattern_gen --profile perf/pattern/mainnet/config/mixed.profile --counts 400000 --verify --out mixed.tar
```

The `--max-blocks` most recent blocks (default 3000) are read once; then each method gets its share of
`--counts` and draws its requests from that window, with repetitions when the window holds fewer
requests than its share. A profile always uses the window, even with a single method, so it needs
`--counts`: `--blocks` is rejected.

## Block range

By default the blocks are read from the head. `--start-block` reads a fixed range instead, e.g. to build a
historical dataset that does not change with the head:

```bash
rpc_pattern_gen eth_call --start-block 20000000 --end-block 20000999 --block-tag parent --verify --out eth_call.tar
rpc_pattern_gen --profile perf/pattern/mainnet/config/mixed.profile --start-block 20000000 --max-blocks 1000 \
    --counts 100000 --block-tag parent --verify --out mixed.tar
# the txs of 10000 blocks drawn at random from 1M, e.g. to defeat the block caches of the nodes
rpc_pattern_gen eth_call --start-block 19000000 --end-block 19999999 --sample-blocks 10000 \
    --block-tag parent --verify --out eth_call.tar
```

| Flags | Blocks read |
|-------|-------------|
| `--start-block M --end-block N` | M to N |
| `--start-block M --max-blocks K` | M to M+K-1 |
| `--start-block M --end-block N --max-blocks K` | M to the first of N and M+K-1 |

- `--start-block` needs `--end-block` or `--max-blocks`; `--end-block` and `--sample-blocks` need
  `--start-block`.
- The range must end at or below the head, and `--end-block` too when `--max-blocks` ends first.
- The blocks are read from the end of the range downwards. A subcommand reads the whole range, or stops
  earlier at `--counts` requests; `--blocks` is rejected. A profile draws from the range as its window
  and still needs `--counts`.
- `--sample-blocks K` reads only K distinct blocks of the range, drawn at random with a fixed seed, so the
  same command on the same range writes the same pattern. Only the K blocks are downloaded, whatever the
  size of the range. K must not exceed the range.
- `--block-tag latest` is rejected: with old transactions it would run them on the state at replay. Use
  `parent`, or `head` to pin every request to the end of the range.

## Tx lists

`eth_call --txs <file>` builds the requests from a list of chosen txs instead of reading blocks, e.g. the
txs where a node is slow, so their speed can be tracked over time. The file holds one `<name> <hash>` per
line; `#` starts a comment:

```
# eth_call slower than on other clients
1-div-loop-28M 0x5a158d08067feeffe06fb1930c50c9f82b0c9175a22d6d110c190fe0dd47d1aa
2-sload-storm  0xea93481b5f1faf0c45f4569755b5cd8059df3fe2e47f89bca4ef25acaa1ebd7e
```

To start from candidates, `tx-list` writes such a list with the successful txs that use the most gas in
some blocks, the heaviest first, named after their rank and gas. It reads the blocks as the other
commands do: `--blocks` from the head, or `--start-block` with `--end-block`, `--max-blocks` and
`--sample-blocks`. Failed txs are left out, since their eth_call would revert.

```bash
# the last 100 blocks
rpc_pattern_gen tx-list --top-gas 20 --blocks 100 --out candidates.txt
# every block of a range
rpc_pattern_gen tx-list --top-gas 20 --start-block 26200000 --end-block 26200999 --out candidates.txt
# 1000 blocks drawn at random from a range, the same ones on every run
rpc_pattern_gen tx-list --top-gas 20 --start-block 19000000 --end-block 19999999 --sample-blocks 1000 --out candidates.txt
```

```
# the 20 txs that use the most gas in 100 blocks between 26249958 and 26250057
1-13.37Mgas 0x038cd6e51bf211b225a5a594938440ef089460197adadc182b877ef73838cd7e
2-13.37Mgas 0xb1caa7ecf91b2f464b0d4af01eb03ead8c9bf8d5777b52f2039488240ec76a72
3-10.59Mgas 0x7b6b049de27ddd4f67bd02cf7e86026d359d7b7c5c269595d6b72e5e3287eeef
...
```

Rename a tx after its bottleneck once it proves interesting, e.g. `5-sload-storm`. The gas is the one of
the original run: with `--block-tag latest` a call can take a shorter branch on the current state, so to
measure the heavy txs as they ran, generate their pattern with `parent`.

From the heaviest txs of the last blocks to the latency of each one, at 50 requests per second for 30 s:

```bash
./build/bin/rpc_pattern_gen tx-list --top-gas 30 --blocks 100 --out heavy.txt
./build/bin/rpc_pattern_gen eth_call --txs heavy.txt --block-tag latest --verify --out heavy_latest.jsonl
./build/bin/rpc_perf -p heavy_latest.jsonl -Z -t 50:30 -r 1 -i
```

```
tx-list: 100 blocks scanned (26249982..26250081), 30 txs written to heavy.txt

1-13.37Mgas: execution reverted
...
eth_call: 30 txs, 23 requests written to heavy_latest.jsonl

[1.1] rpcdaemon: executes test qps: 50 time: 30 -> success=100.00% lat=[max= 72.10ms]
      id                                reqs  share   result    empty    error       p50       p99       max
      2-11.92Mgas                         66   4.4%   100.0%     0.0%     0.0%    1.67ms    6.44ms    7.17ms
      3-9.16Mgas                          66   4.4%   100.0%     0.0%     0.0%    2.74ms    3.58ms    3.66ms
      4-9.12Mgas                          66   4.4%   100.0%     0.0%     0.0%    1.33ms    1.95ms    1.97ms
      5-8.95Mgas                          66   4.4%   100.0%     0.0%     0.0%   67.87ms   71.87ms   72.10ms
      6-8.87Mgas                          66   4.4%   100.0%     0.0%     0.0%    1.29ms    1.58ms    1.58ms
      ...
      13-5.54Mgas                         65   4.3%   100.0%     0.0%     0.0%    6.46ms   11.00ms   11.16ms
      ...
      30-4.97Mgas                         65   4.3%   100.0%     0.0%     0.0%     918µs    1.72ms    1.76ms
```

Seven of the 30 txs fail on the current state and are dropped. `5-8.95Mgas` stands out with a p50 of
68 ms, where the others take 1 to 7 ms: it is the one to profile, and to rename after its bottleneck.

```bash
rpc_pattern_gen eth_call --txs slow.txt --block-tag latest --verify --out slow_latest.jsonl
rpc_pattern_gen eth_call --txs slow.txt --block-tag parent --verify --out slow_parent.jsonl
```

- Each tx gives one `eth_call` with its call arguments, in the order of the list. The request `id` is the
  name of the tx, so `rpc_perf -i` prints the latencies of each tx:

  ```bash
  ./build/bin/rpc_perf -p slow_parent.jsonl -Z -t 50:30 -r 1 -i
  ```
- The name is a free label, unique in the file. A short summary of the bottleneck, after a sequence
  number, is easy to find in a long output: `3-sload-storm`.
- `latest` runs the calls on the state at replay: the same code and input on the current storage, so the
  pattern works on any node, also without archive, but some calls revert or take another branch as the
  state moves. `parent` pins each call to the block before its tx, so the pattern never changes, but it
  needs a node that still holds that state (an archive node for old txs). It is not the original run: the
  call sees the state and the block (number, time, base fee) of the previous block, without the txs before
  it in its own block, so a tx that depends on them, or on the block, can revert.
- A tx that the node does not know fails the run. With `--verify`, the txs whose call fails are dropped
  and printed with their error.
- `--counts`, `--blocks`, the block range flags and `--per-block` are rejected: the list sets the requests.

## Converting Vegeta patterns to JSONL

`rpc_pattern_convert` turns a Vegeta pattern into JSONL: one JSON-RPC request per line, compacted, in the
order of the Vegeta targets, with no URL, header or base64. No node is needed, and the output can be read,
grepped and compared with `diff`.

```bash
# a pattern tar: the vegeta_erigon_<name>.txt file inside is converted
rpc_pattern_convert --in perf/pattern/mainnet/stress_test_eth_getLogs_15M.tar --out eth_getLogs_15000000.jsonl
# a tar holding several patterns: select one by its name
rpc_pattern_convert --in perf/pattern/mainnet/stress_test_debug_storageRangeAt_20M.tar --name debug_storageRangeAt \
    --out debug_storageRangeAt_20000000.jsonl
# a Vegeta target file, e.g. one written by Erigon's cmd/rpctest
rpc_pattern_convert --in /tmp/erigon_stress_test/vegeta_erigon_eth_call.txt --out eth_call.jsonl
```

- `--in` is a tar, plain, gzip or bzip2 (detected from the content, whatever the extension), or any other
  file, read as Vegeta targets.
- In a tar only the non-empty `vegeta_erigon_<name>.txt` files count: `vegeta_geth_*`, `results_*.csv` and
  the other files are ignored. With several of them, `--name` selects one; otherwise the run fails listing
  them.
- The output is written only when the whole pattern converts; a target that is not JSON fails the run with
  its line number. The number of requests written is printed.

## Common flags

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | `http://localhost:8545` | Node used to read the blocks and verify the requests |
| `--target-url` | `http://localhost:8545` | URL written in each Vegeta target (tar output only) |
| `--counts` | | Number of requests to write (exclusive with `--blocks`) |
| `--blocks` | | Read this many blocks from the head (subcommands only) |
| `--max-blocks` | 3000 | With a profile, the window of recent blocks the requests are drawn from; with `--start-block`, the length of the range |
| `--start-block` | | First block of a [block range](#block-range), read instead of the blocks below the head |
| `--end-block` | | With `--start-block`, the last block of the range |
| `--sample-blocks` | | With `--start-block`, read only this many distinct blocks of the range, drawn at random |
| `--block-tag` | `latest` | Block parameter of the methods that take one: `latest` (resolved by the node at replay), `head` (head number pinned at generation, or the end of the block range, the same state on every node) or `parent` (the block before the transaction) |
| `--fees` | `drop` | Gas price of eth_call/eth_estimateGas: `drop` (no fee, the node skips the base fee and balance checks) or `keep` (the tx gas price) |
| `--verify` | off | Run every request on the node and count the failures |
| `--keep` | `ok` | With `--verify`, the requests to write: `ok` (only the working ones) or `all` |
| `--min-count` | | Fail if fewer requests are written |
| `--name` | method or `mixed` | Name of the Vegeta file in the tar, the `rpc_perf -y` value (tar output only) |
| `--per-block` | all | Subcommands only: take at most this many txs (or logs for eth_getLogs) of each block, picked at random with the block number as seed |
| `--workers` | 16 | Concurrent requests to the node |
| `--out` | | Output file (required): a Vegeta tar, gzipped for `.tar.gz`/`.tgz`, or [JSONL](#jsonl-output) for `.jsonl` |
| `-v`, `--verbose` | off | Print the verification statistics per method and error kind |
| `--profile` | | Root command only: the [profile](#profile-format) with the mix of methods to generate |
| `--range` | 100 | `eth_getLogs` only: the widest block range of a request |
| `--txs` | | `eth_call` only: a [tx list](#tx-lists), one request per tx instead of reading blocks |

```bash
rpc_pattern_gen eth_call --url http://localhost:8545 --blocks 100 --verify -v --out eth_call.jsonl
rpc_pattern_gen eth_call --counts 50000 --block-tag head --keep all --workers 32 --out eth_call.jsonl
rpc_pattern_gen eth_call --counts 50000 --fees keep --min-count 40000 --out eth_call.jsonl
rpc_pattern_gen eth_getLogs --counts 100000 --range 1000 --verify --out eth_getLogs.jsonl
rpc_pattern_gen --profile perf/pattern/mainnet/config/mixed.profile --counts 400000 --max-blocks 1000 --out mixed.jsonl
rpc_pattern_gen eth_call --counts 10000 --target-url http://localhost:8546 --name eth_call_8546 --out eth_call.tar
```

With `--verify`, per-tx and per-log requests are checked one by one until each method has enough working
ones; an eth_getLogs request returning no log counts as a failure. For per-block and constant methods a
sample request is checked.

## Profile format

```ini
# Comments start with #, also at the end of a line.
[methods]
# <method>                 <weight>  [option=value ...]
eth_call                   35.8%
eth_getBlockByNumber       19.9%     full=true
eth_getLogs                6.42%     filter=address range=100
debug_traceBlockByHash     3.91%     tracer=callTracer blocks=50

[contracts]
# <address>                                  free text
0xdac17f958d2ee523a2206206994597c13d831ec7   USDT
```

- **`[methods]`** (required): one method per line. The weight is relative (the `%` is optional and the
  weights need not sum to 100); `--counts` is split among the methods by weight.
- **`blocks=N`** (any method): draw this method only from the N most recent blocks of the window, e.g.
  for traces on nodes that keep the state of the recent blocks only.
- **`[contracts]`** (optional): keep only the transactions sent to these addresses, the blocks holding
  them and the logs these contracts emit. Without it every transaction, block and log is used.
- The same method can appear on several lines with different options; it is then reported as one method
  per option set.

## Methods and options

| Method | Built from | Options |
|--------|-----------|---------|
| `eth_call` | each transaction: its from, to, gas, value, input, on `--block-tag` | |
| `eth_estimateGas` | each transaction, as eth_call (the tx gas caps the search) | |
| `eth_getTransactionReceipt` | each transaction hash | |
| `eth_getTransactionByHash` | each transaction hash | |
| `eth_getBalance` | each transaction sender, on `--block-tag` | |
| `eth_getTransactionCount` | each transaction sender, on `--block-tag` | |
| `eth_getBlockByNumber` | each block | `full=true\|false` (full transactions, default false) |
| `eth_getBlockByHash` | each block | `full=true\|false` |
| `eth_getBlockReceipts` | each block | |
| `debug_traceBlockByHash` | each block | `tracer=<name>`, e.g. `callTracer` (default: the struct logger) |
| `eth_getLogs` | each log of a block | `range=N`, `filter=...`, `or=N`, see below |
| `eth_blockNumber`, `eth_chainId`, `net_listening`, `eth_gasPrice`, `eth_maxPriorityFeePerGas` | nothing: the same request repeated | |

### eth_getLogs

Every request is built from a real log of a block, so it returns at least that log. The choices below
are drawn per log with a seed taken from the log, so a pattern is reproducible on the same chain.

- **`range=N`** (default 100): block range of the request, from 1 to N blocks around the log's block,
  log-uniform (mostly narrow, a few up to N) and never past the head. The range is in block numbers, so
  the request keeps its result when replayed some blocks later.
- **`filter=`**: the filter shapes, one drawn at random per log among those the log can match:

  | `filter` | Shapes |
  |----------|--------|
  | (unset) | all of the below |
  | `address` | address |
  | `address+topics` | address+topic0, address+topic0+topic1, address+topic0+topic2 |
  | `topics` | topic0+topic1, topic0+topic2 (no address) |

  topic0 alone is never used: an event signature such as Transfer matches too many logs across
  contracts. The logs with too few topics for the chosen family are skipped (e.g. a log with topic0 only
  under `filter=topics`).
- **`or=N`** (default 1): above 1, the address and each topic of the filter become a list of up to N
  alternatives: the log's own value plus other distinct values found at the same position in the logs
  of the same block. With `[contracts]` the other values come only from the logs of those contracts.
  The list lengths are log-uniform between 1 and N, mostly short, a few up to N
  (fewer when the block has fewer distinct values); null topic positions stay null.

## Mainnet profiles

All in `mainnet/config/`. Use the method name as `rpc_perf -y` for the single-method profiles, `mixed`
for `mixed.profile`.

| Profile | Load |
|---------|------|
| `mixed.profile` | Method shares of a provider's RPC traffic, on the txs and logs of the most called contracts; eth_getLogs by address, traces on the 50 most recent blocks |
| `eth_call.profile` | eth_call only, from the window (the `eth_call` subcommand reads from the head down instead) |
| `eth_gasPrice.profile` | eth_gasPrice only, a constant request |
| `eth_getLogs_mixed.profile` | eth_getLogs with every filter shape, on the logs of the `mixed.profile` contracts, range up to 100 |
| `eth_getLogs_address_topics.profile` | `filter=address+topics`, range up to 100: address and topic indexes intersected |
| `eth_getLogs_topics.profile` | `filter=topics`, range up to 10: topic index alone, where events such as Transfer are dense |
| `eth_getLogs_wide.profile` | `filter=address+topics`, range up to 1000: the cost of walking many blocks |
| `eth_getLogs_head.profile` | range=1 on the 100 most recent blocks: indexers following the head |
| `eth_getLogs_or.profile` | `or=16`: lists of addresses and topics, as indexers watching many contracts or events |

Example:

```bash
./build/bin/rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_or.profile \
    --counts 200000 --verify --out eth_getLogs_or.tar
./build/bin/rpc_perf -p eth_getLogs_or.tar -y eth_getLogs -Z -t 500:30,1000:30
```
