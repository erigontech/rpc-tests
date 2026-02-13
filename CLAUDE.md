# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`rpc-tests` is a collection of JSON-RPC black-box testing tools for Ethereum node implementations. It sends JSON-RPC requests to a running RPC daemon and compares responses against expected results stored as JSON test fixtures. The codebase has both Go (primary, being actively developed) and Python (legacy) implementations.

## Build & Run

```bash
# Build the integration test binary
go build -o ./build/bin/rpc_int ./cmd/integration/main.go

# Run Go unit tests
go test ./cmd/integration/archive/
go test ./cmd/integration/jsondiff/

# Run Python unit tests
pytest

# Run integration tests (requires a running RPC daemon on localhost:8545)
./build/bin/rpc_int -c -f                          # All tests, continue on fail, show only failures
./build/bin/rpc_int -t 246                          # Single test by global number
./build/bin/rpc_int -A eth_getLogs -t 3             # Single test by API + test number
./build/bin/rpc_int -A eth_call                     # All tests for one API
./build/bin/rpc_int -a eth_ -c -f -S               # APIs matching pattern, serial mode
./build/bin/rpc_int -b sepolia -c -f                # Different network
```

## Architecture

**Three independent tools** under `cmd/`:
- `cmd/integration/` — RPC integration test runner (primary tool, ~2100 lines in main.go)
- `cmd/compat/` — RPC compatibility checker
- `cmd/perf/` — Load/performance testing (uses Vegeta)

**Integration test runner flow:**
1. Scans `integration/{network}/` for test fixture files (JSON or tar archives)
2. Tests are globally numbered across all APIs and filtered by CLI flags
3. Executes in parallel (worker pool, `runtime.NumCPU()` workers) by default
4. Sends JSON-RPC request from each test fixture to the daemon
5. Compares actual response against expected response using JSON diff
6. Reports results with colored output, saves diffs to `{network}/results/`

**Supporting packages:**
- `cmd/integration/archive/` — Extract test fixtures from tar/gzip/bzip2 archives
- `cmd/integration/jsondiff/` — Pure Go JSON diff with colored output
- `cmd/integration/rpc/` — HTTP JSON-RPC client with JWT auth and compression support

**Active v2 refactor** (branch `canepat/v2`): `integration/cli/` is a restructured version of the test runner using `urfave/cli/v2`, splitting the monolithic main.go into focused modules: `flags.go` (config), `test_runner.go` (orchestration), `test_execution.go` (per-test logic), `test_comparator.go` (response comparison), `test_filter.go` (filtering), `rpc.go` (client), `utils.go`.

**Test fixture format** — each test is a JSON file (or tarball containing JSON):
```json
{
  "request": [{"jsonrpc":"2.0","method":"eth_call","params":[...],"id":1}],
  "response": [{"jsonrpc":"2.0","id":1,"result":"0x..."}]
}
```

Test data lives in `integration/{network}/{api_name}/test_NN.json` across networks: mainnet, sepolia, gnosis, arb-sepolia, polygon-pos.

## Key CLI Flags

| Flag | Description |
|------|-------------|
| `-c` | Continue on test failure (default: exit on first failure) |
| `-f` | Display only failed tests |
| `-S` | Serial execution (default: parallel) |
| `-v 0/1/2` | Verbosity level |
| `-b <network>` | Blockchain: mainnet, sepolia, gnosis (default: mainnet) |
| `-H <host>` / `-p <port>` | RPC daemon address (default: localhost:8545) |
| `-A <apis>` | Filter by exact API name (comma-separated) |
| `-a <pattern>` | Filter by API name pattern |
| `-t <num>` | Run single test by number |
| `-x <apis>` | Exclude APIs |
| `-X <nums>` | Exclude test numbers |
| `-T <transport>` | Transport: http, http_comp, https, websocket, websocket_comp |
| `-k <file>` | JWT secret file for engine API auth |

## Dependencies

Go 1.24. Key libraries: `gorilla/websocket` (WebSocket transport), `josephburnett/jd/v2` (JSON diffing), `tsenart/vegeta/v12` (load testing), `urfave/cli/v2` (CLI framework for v2), `golang-jwt/jwt/v5` (JWT auth), `dsnet/compress` (bzip2).

Python 3.10+ with `requirements.txt` for legacy runner and standalone tools in `src/rpctests/`.