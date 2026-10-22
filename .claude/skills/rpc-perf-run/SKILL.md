---
name: rpc-perf-run
description: Use to run RPC performance tests (rpc_perf) against a running node with an existing pattern, read the latencies and errors, and compare runs, e.g. a branch against main or one node against another. To build a new pattern use the perf-pattern-add skill.
allowed-tools: Bash, Read, Glob, Grep
---

# Run the performance tests

## Overview

`rpc_perf` replays a pattern (a `.jsonl` with one JSON-RPC request per line, or a Vegeta tar) at fixed
rates and reports, for each test, the success rate, the JSON-RPC errors and the latencies. Full option
list with examples: `perf/README.md`.

## Build

```bash
make rpc_perf
```

## Command syntax

```bash
./build/bin/rpc_perf -p <pattern> -d <host[:port]> -Z -t <qps:seconds,...> -r <repetitions> [-v | -i] [options]
```

- `-p`: the pattern; `.jsonl` files, or the historical tars in `perf/pattern/mainnet/` (with `-y <test type>`).
- `-d`: the node, `host` or `host:port` (default `localhost`, port 8545).
- `-Z`: skip the check that a process named `rpcdaemon` is still running after each test. Use it unless
  the node runs as a separate `rpcdaemon` (then the check catches a crash: `-n <process name>`).
- `-t`, `-r`: the rates and durations, and how many times each test runs. The default sequence goes up
  to 10000 qps, 10 times each: always set both.

## Usage patterns

```bash
./build/bin/rpc_perf -p slow_parent.jsonl -Z -t 50:30 -r 1 -i                    # one line per tx of a tx-list pattern
./build/bin/rpc_perf -p mixed.jsonl -Z -t 1000:30 -r 1 -v                        # one line per method
./build/bin/rpc_perf -p mixed.jsonl -Z -t 100:30,500:30,1000:30 -r 3 -w 10 -v -P # a ramp, 3 times each, more percentiles
./build/bin/rpc_perf -p perf/pattern/mainnet/stress_test_eth_call_001_14M.tar -y eth_call -Z -t 1000:30 -r 1
./build/bin/rpc_perf -p eth_call_13000000.jsonl -Z -t 500:30 -r 3 -R -j eth_call.json -D erigon   # CSV and JSON report
```

## Reading the results

```
[1.1] rpcdaemon: executes test qps: 50 time: 30 -> success=100.00% lat=[max= 39.04ms]
      id                                      reqs  share   result    empty    error       p50       p99       max
      2-5a158d08-div-loop-28M                  100   6.7%   100.0%     0.0%     0.0%   36.21ms   38.77ms   39.04ms
      13-c0b6ab3a-storage-b9c2                 100   6.7%   100.0%     0.0%     0.0%     961µs    1.88ms    2.14ms
```

- `[1.1]`: test 1 of the sequence, repetition 1.
- `success` counts HTTP 200 only: a JSON-RPC error is a success there. Look at `rpc_errors=` on the
  same line and at the `error` column of `-v`/`-i`.
- `empty`: results that are `null` or `[]`, valid for some methods.
- A test with 0% success stops the run.

## Use cases

### A branch against main

1. Build the node from main and from the branch. Start one at a time on the same datadir and machine
   (see the erigon-run skill), and stop it before starting the other.
2. On each, run a short warm-up (`-t 50:10 -r 1`), then the same pattern with the same `-t` and `-r`,
   saving the console output; the per-line tables of `-i` and `-v` are only there, not in the reports:

   ```bash
   ./build/bin/rpc_perf -p slow_parent.jsonl -Z -t 50:30 -r 3 -i -D erigon-main | tee main.txt
   ./build/bin/rpc_perf -p slow_parent.jsonl -Z -t 50:30 -r 3 -i -D erigon-branch | tee branch.txt
   ```

3. Compare the two files: for each id (or method), the p50 and p99 of every repetition of each build.
   Report a table with, per line, the median and the range of the repetitions of each build and the
   change in percent. Call a difference only when it is larger than the range between the repetitions
   of the same build; otherwise report it as noise. The p99 of a line is noisy when it has few requests
   (at 50 qps for 30 s, 15 txs get 100 each, so their p99 is about the slowest one): compare the p50 first,
   and for the p99 run longer or more times, so that each line gets thousands of requests.
4. To check that the responses did not change, run the integration tests on the branch too (see the
   rpc-int-run skill).

### Track a few txs over time

Replay the pattern of a tx list with `-i` after each change: every tx keeps its own line, so the gain or
loss of one tx is not hidden in the p99 of the group.

### p99 much worse than p50

```bash
./build/bin/rpc_perf -p slow_parent.jsonl -Z -t 50:30,500:30 -r 1 -i -P
```

Compare each line between the low and the high rate. A p99 that grows much more than the p50 points to
contention (locks) or to disk reads, not to the cost of the call itself.

### Another client

Run the same `latest` pattern with the same `-t` and `-r` against each client in turn (`-d` for its
host and port) and compare per line. Patterns pinned to old blocks fail on clients that keep only the
recent state.

## Workflow

1. **Confirm the inputs**: the pattern, the node, the rates and durations, and what is compared.
2. **Safety checks**
   - The node answers `eth_blockNumber` on `-d`.
   - The pattern fits the node: `parent` and historical patterns need the state of their blocks (an
     archive node for old blocks).
   - Ask before high rates (thousands of qps) on a shared node.
3. **Run** a short test first (`-t 50:10 -r 1`) to check the success rate, then the real sequence.
4. **Report** the success rate, the JSON-RPC errors and the p50/p99 per method or per tx, and for a
   comparison the difference per line with the spread of the repetitions.
