---
name: rpc-int-run
description: Use to run the RPC integration tests (rpc_int) against a running node, the whole suite or some APIs or tests, and to read the failures. Use when the user wants to check a node or a branch against the expected JSON-RPC responses.
allowed-tools: Bash, Read, Glob, Grep
---

# Run the integration tests

## Overview

`rpc_int` sends the request of each test fixture in `integration/<network>/` to a node and compares the
response with the expected one. Tests are numbered globally across all APIs. Full option list:
`./build/bin/rpc_int -h` and `integration/README.md`.

## Build

```bash
make rpc_int
```

## Command syntax

```bash
./build/bin/rpc_int -c -f -k <jwt file> -b <network> [selection] [options]
```

- `-c`: run every test even after a failure (default: stop at the first).
- `-f`: print only the failed tests.
- `-k`: the JWT secret of the node, for the `engine_*` tests on port 8551 (Erigon: `<datadir>/jwt.hex`).
- `-b`: the network, a folder of `integration/` (default `mainnet`).
- `-H`, `-p`: host and port of the node (default `localhost:8545`).

## Usage patterns

```bash
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex                         # the whole suite, in parallel
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex -A eth_getLogs          # one API, full name
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex -a eth_getBlock         # the APIs whose name contains the string
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex -A eth_call -t 3        # test 3 of eth_call
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex -x trace_ -M 0          # without the trace_ APIs, no failure limit
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex -A eth_getLogs -S       # in serial, e.g. to measure or debug
./build/bin/rpc_int -k /path/to/datadir/jwt.hex -A eth_call -t 3 -v 2         # print request and response
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex -A eth_getLogs -R report.csv   # summary in a file
```

- Without `-A`, `-t` is the global test number shown in the output (e.g. `0653.`).
- `-M` stops the run after that many failures (default 100); `-M 0` never stops.
- `-T websocket` needs the node started with `--ws`.

## Reading the results

```
0653. http           ::eth_createAccessList/test_16.json                              failed: diff mismatch
...
Number of executed tests:     1238
Number of success tests:      1138
Number of failed tests:       100
```

For each failed test the runner writes, in `integration/<network>/results/<api>/` (ignored by git):
`test_NN-response.json` (the node), `test_NN-expResponse.json` (the fixture) and `test_NN-diff.json`.
Read the diff first; the expected response is in the fixture `integration/<network>/<api>/test_NN.json`.

## Use cases

### A change must not alter the responses

E.g. a method moved to another code path (eth_call on parallel execution): run the tests of the APIs it
touches on the node built from the branch. Every fixture is a full expected response, so a passing run
means the bodies did not change.

```bash
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex -A eth_call
./build/bin/rpc_int -c -f -k /path/to/datadir/jwt.hex -A eth_call,eth_estimateGas
```

### Many tests of one API fail

Look at one diff: when every test fails on the same field, the fixtures and the node disagree on the
format (e.g. a field added or removed on one side), not on the data. Check that the fixtures are the
current ones (`git log integration/<network>/<api>`) and that the node is built from the matching code.
Some responses depend on the build itself, e.g. `engine_exchangeCapabilities` lists the methods of the
node: a node newer than the fixtures fails them until the fixtures are updated.

## Important considerations

- The fixtures were recorded on a synced archive node: run them against a node of the same network that
  holds the blocks they ask for, or many tests fail for missing data, not for a bug.
- Some tests are skipped unless asked for, e.g. `-C` for those that need commitment history.
- Results of the previous run in `integration/<network>/results/` are overwritten.

## Workflow

1. **Confirm the inputs**: the network, the node (host, port), its JWT file, and which APIs or tests.
2. **Safety checks**
   - The node answers `eth_blockNumber`.
   - The JWT file exists if `engine_*` tests run.
3. **Run** the narrowest selection that answers the question: one API (`-A`) or one test (`-t`) before
   the whole suite.
4. **Report**: the summary counts and, for each failure, the API, the test and the gist of its diff.
   Group the failures by API; a whole API failing usually points to the node setup, not to a bug.
