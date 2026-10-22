# Performance Tests

`rpc_perf` replays a pattern file, a list of JSON-RPC requests, against a node at fixed rates with
[Vegeta](https://github.com/tsenart/vegeta/), and reports the latencies and the errors of each run.
The patterns are generated with `rpc_pattern_gen` (see [`pattern/README.md`](pattern/README.md)), or by
the `bench*` commands of Erigon's `cmd/rpctest` (e.g. `benchEthCall`), which also write the requests they
send as `<method>.jsonl` (see [Pattern files](#pattern-files)).

## Quick start

```bash
make rpc_pattern_gen rpc_perf

# 1. one eth_call per tx of 1000 random blocks of 13M-14M, kept only if it works on the node
./build/bin/rpc_pattern_gen eth_call --start-block 13000000 --end-block 13999999 --sample-blocks 1000 \
    --per-block 1 --block-tag parent --verify --out eth_call_13000000.jsonl

# 2. replay it at 500 and then 1000 requests per second, 30 s each, against the node on localhost:8545
./build/bin/rpc_perf -p eth_call_13000000.jsonl -Z -r 1 -t 500:30,1000:30 -v
```

## Requirements

Build the runner:
```bash
make rpc_perf
# or
go build -o ./build/bin/rpc_perf ./cmd/perf
```

## System Configuration

The following system configuration steps shall be performed:

* increase core dump size
```
ulimit -c unlimited
```
* increase max file descriptors
```
ulimit -n 999999
```
* check current IP local port range and increase it (writing permanently in /etc/sysctl.conf)

### Linux
```
cat /proc/sys/net/ipv4/ip_local_port_range
sudo sysctl -w "net.ipv4.ip_local_port_range=5000 65000"
```
### macOS
```
sudo sysctl net.inet.ip.portrange
sudo sysctl net.inet.ip.portrange.first=5000 net.inet.ip.portrange.last=65535
```

## Node under test

Any node serving JSON-RPC over HTTP, e.g. Erigon with its embedded RPC server or a separate rpcdaemon:

```bash
./build/bin/erigon --datadir <datadir> --http.api=eth,debug,net,web3,trace,erigon,ots
./build/bin/rpcdaemon --datadir <datadir> --private.api.addr=localhost:9090 --http.api=eth,debug,net,web3,trace,erigon,ots
```

After each test `rpc_perf` checks that the server is still alive by looking for a process named `-n`
(default: the value of `-D`, `rpcdaemon`) with `pgrep -x`. When no process has that name, e.g. Erigon with
its embedded RPC server or a node on another host, the run fails with `server is Dead`: pass `-Z` to
skip the check.

## Pattern files

`-p` takes either format:

- **Vegeta tar** (`.tar`, `.tar.gz`, ...): holds `erigon_stress_test/vegeta_erigon_<test-type>.txt`, one Vegeta
  target per line. `-y` selects the file in the tar, and `localhost` in the target URLs is replaced with `-d`.
- **JSONL** (`.jsonl`): one JSON-RPC request per line, as written by `rpc_pattern_gen --out <file>.jsonl`
  (see [`pattern/README.md`](pattern/README.md#jsonl-output)). The file is read in place; each request is
  posted to `http://<-d>:8545`, or to the port given in `-d` as `host:port`. `-y` is optional: it names
  the test in the reports (result files, CSV, JSON report) and defaults to the method of the requests, or
  `mixed` when they use several methods.

The patterns come from:

- `rpc_pattern_gen`, from the blocks of a node, a block range, a profile with a mix of methods, or a list
  of chosen txs: see [`pattern/README.md`](pattern/README.md).
- the `bench*` commands of Erigon's `cmd/rpctest` (e.g. `benchEthCall`), which write the requests they
  send to `$TMPDIR/erigon_stress_test/` both as Vegeta targets (`vegeta_erigon_<method>.txt`) and as
  `<method>.jsonl`.
- the historical tars in `pattern/mainnet/`.

## Options

### Pattern and node

| Option | Default | Description |
|--------|---------|-------------|
| `-p`, `--pattern-file` | | The pattern to replay: a Vegeta tar or a `.jsonl`. Required |
| `-y`, `--test-type` | `eth_getLogs`; for a `.jsonl`, its method or `mixed` | Test name in the output and the reports; with a tar, also the Vegeta file to replay |
| `-d`, `--rpc-client-address` | `localhost` | Node to load: `host`, or `host:port` for a `.jsonl` (port 8545 otherwise) |
| `-b`, `--blockchain` | `mainnet` | Network name, used in the report paths and file names |

```bash
./build/bin/rpc_perf -p eth_call.jsonl -d localhost:8545 -Z -t 1000:30 -r 1   # the host and port of the node
./build/bin/rpc_perf -p perf/pattern/mainnet/stress_test_eth_call_001_14M.tar -y eth_call -Z -t 1000:30 -r 1
```

### Load

| Option | Default | Description |
|--------|---------|-------------|
| `-t`, `--test-sequence` | `50:30,1000:30,2500:20,10000:20` | The tests to run, in order, as `qps:seconds,...` |
| `-r`, `--repetitions` | `10` | How many times each test of the sequence runs |
| `-w`, `--wait-after-test-sequence` | `5` | Seconds to wait between two tests |
| `-C`, `--max-connections` | `9000` | Maximum HTTP connections to the node |
| `-T`, `--response-timeout` | `300s` | Timeout of each request; a request that takes longer counts as failed |
| `-O`, `--disable-http-compression` | off | Send the requests without `Accept-Encoding: gzip` |
| `-e`, `--empty-cache` | off | Drop the OS page cache before each test, to measure cold reads (root only) |

The requests of the pattern are sent in a loop: a test at 1000 qps for 30 s sends 30000 requests,
repeating the pattern when it holds fewer. A test at 0 qps is a pause: `0:60` waits 60 s.

```bash
./build/bin/rpc_perf -p mixed.jsonl -Z -t 1000:30 -r 1                       # one test, once
./build/bin/rpc_perf -p mixed.jsonl -Z -t 100:30,500:30,1000:30 -r 3 -w 10   # three rates, three times each, 10 s apart
./build/bin/rpc_perf -p eth_getLogs.jsonl -Z -t 200:60 -r 1 -T 30s -C 500    # 30 s timeout, at most 500 connections
sudo ./build/bin/rpc_perf -p eth_call.jsonl -Z -t 500:30 -e                  # cold OS cache before each test
```

### Liveness check

| Option | Default | Description |
|--------|---------|-------------|
| `-Z`, `--not-verify-server-alive` | off | Skip the check that the server process is still running after each test |
| `-n`, `--process-name` | the value of `-D` | Process looked up with `pgrep -x` for the check |
| `-H`, `--halt-on-vegeta-error` | off | Stop at the first test where Vegeta reports a failed request |

A test with 0% success always stops the run.

```bash
./build/bin/rpc_perf -p eth_call.jsonl -Z -t 1000:30 -r 1             # no check
./build/bin/rpc_perf -p eth_call.jsonl -n erigon -H -t 1000:30 -r 1   # stop if erigon dies or a request fails
```

### Output

| Option | Default | Description |
|--------|---------|-------------|
| `-v`, `--verbose` | off | After each test, print a line per method: requests, share, result/empty/error rates, p50, p99, max |
| `-P`, `--more-percentiles` | off | Print p50, p90, p95 and p99 too, in the test line (also of the OK responses, as `lat_ok`) and in the `-v` table |
| `-I`, `--instant-report` | off | Print the full Vegeta report of each test (latencies, bytes, status codes, errors) |
| `-x`, `--tracing` | off | As `-v`, plus the steps of the run (pattern copy and extraction) |
| `-i`, `--by-id` | off | Print the `-v` table by request `id` instead of by method, ordered by the number the ids start with: one line per tx of a pattern built with `rpc_pattern_gen --txs` |

```bash
./build/bin/rpc_perf -p mixed.jsonl -Z -t 1000:30 -r 1 -v -P
./build/bin/rpc_perf -p mixed.jsonl -Z -t 1000:30 -r 1 -I
./build/bin/rpc_perf -p slow_parent.jsonl -Z -t 50:30 -r 1 -i      # one line per tx of the list
```

### Reports

| Option | Default | Description |
|--------|---------|-------------|
| `-R`, `--tmp-test-report` | off | Write a CSV report to `/tmp/run_tests_<n>/<network>/<machine>/<test-type>_<date>_<client>_perf.csv` |
| `-u`, `--test-report` | off | Write the CSV report to `./perf/reports/<network>/<machine>/`, ready to commit |
| `-j`, `--json-report` | | Also write a JSON report to this path. Needs `-R` or `-u`: without them it is not written |
| `-D`, `--testing-client` | `rpcdaemon` | Client name in the output, the report file names and the JSON report |
| `-g`, `--client-build-dir` | | Client source folder: its git commit goes into the report |
| `-c`, `--run-vegeta-on-core` | `-:-` | Taskset written in the report; `rpc_perf` does not pin itself, run it under `taskset` to do so |
| `-M`, `--max-body-rsp` | `1500` | Bytes of each response body kept in the Vegeta binary file of the report (`-1`: the whole body) |

The CSV has one row per test: the latencies and the success ratio of all the responses, then `RpcRatio`,
`50ok`, `90ok`, `95ok`, `99ok` and `MaxOk` of the OK responses. The JSON report has, for each repetition,
the Vegeta report (`vegetaReport`), `rpcSuccessRatio` and `latenciesOk` (ns, with the keys of the Vegeta
report).

With a report, the Vegeta binary file of each test (every request with its latency, status and the
first `-M` bytes of the response) is kept in `/tmp/run_tests_<n>/bin/` (`./perf/reports/bin/` with `-u`).

```bash
./build/bin/rpc_perf -p eth_call.jsonl -Z -t 500:30 -r 3 -R -j eth_call.json -D erigon -g /path/to/erigon
./build/bin/rpc_perf -p perf/pattern/mainnet/stress_test_eth_call_001_14M.tar -y eth_call -Z -g /path/to/erigon \
    -r 3 -u -b mainnet -t 50:30,1000:30,2500:20,5000:20
```

## Reading the output

An example test at 500 qps for 30 s, with `-v`:

```
[1.1] rpcdaemon: executes test qps: 500 time: 30 -> success=100.00% lat=[max= 39.06ms] rpc_errors=12 (0.08%) top=execution reverted (x12)
      api                               reqs  share   result    empty    error       p50       p99       max
      eth_call                         15000 100.0%    99.9%     0.0%     0.1%    4.80ms   38.40ms   39.06ms  top=execution reverted (x12)
```

- `[1.1]`: test 1 of the sequence, repetition 1.
- `success`: requests answered with an HTTP status below 400. A JSON-RPC error comes with HTTP 200, so it
  counts as a success here; `rpc_errors` counts them apart, with the most frequent message.
- `result`, `error` (`-v`): requests with a result, and requests failed by HTTP or with a JSON-RPC error.
- `empty` (`-v`): results that are `null` or `[]`, valid for some methods, counted apart.
- `rpc_success`, `p99_ok`: the rate and the p99 of the OK responses, HTTP 200 and no JSON-RPC error (`null`
  and `[]` count as OK). With `-P`, `lat_ok=[...]` gives all their percentiles.

The two series differ when calls fail. An `eth_call` pattern generated without `--verify`, with `-P`:

```
[1.1] erigon: executes test qps: 100 time: 10 -> success=100.00% lat=[p50=   478µs  p90=   803µs  p95=  1.22ms  p99=  6.36ms  max= 27.97ms] rpc_errors=493 (49.30%) top=execution reverted (x239) (+3 more) rpc_success= 50.70% lat_ok=[p50=   454µs  p90=   806µs  p95=  1.44ms  p99=  7.44ms  max= 27.97ms]
```

Half of the calls fail with a JSON-RPC error, so `success` stays at 100% while `rpc_success` is 50.70%; the
errors are fast, so the p99 of all the responses (6.36 ms) is below the p99 of the OK ones (7.44 ms).
