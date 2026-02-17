# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`rpc-tests` is a collection of JSON-RPC black-box testing tools for Ethereum node implementations. It sends JSON-RPC requests to a running RPC daemon and compares responses against expected results stored as JSON test fixtures. The codebase has both Go (primary, being actively developed) and Python (legacy) implementations.

## Build & Run

```bash
# Build the integration test binary
go build -o ./build/bin/rpc_int ./cmd/integration/main.go

# Run all Go tests
go test ./...

# Run a single package's tests
go test ./internal/eth/
go test ./internal/tools/

# Lint
golangci-lint run

# Run Python unit tests
pytest

# Run integration tests (requires a running RPC daemon on localhost:8545)
./build/bin/rpc_int -c -f                          # All tests, continue on fail, show only failures
./build/bin/rpc_int -t 246                          # Single test by global number
./build/bin/rpc_int -A eth_getLogs -t 3             # Single test by API + test number
./build/bin/rpc_int -A eth_call                     # All tests for one API
./build/bin/rpc_int -a eth_ -c -f -S               # APIs matching pattern, serial mode
./build/bin/rpc_int -b sepolia -c -f                # Different network

# Run subcommands (ported from Python scripts in src/rpctests/)
./build/bin/rpc_int block-by-number --url ws://127.0.0.1:8545
./build/bin/rpc_int empty-blocks --url http://localhost:8545 --count 10
./build/bin/rpc_int filter-changes --url ws://127.0.0.1:8545
./build/bin/rpc_int latest-block-logs --url http://localhost:8545
./build/bin/rpc_int subscriptions --url ws://127.0.0.1:8545
./build/bin/rpc_int graphql --http-url http://127.0.0.1:8545/graphql --query '{block{number}}'
./build/bin/rpc_int replay-request --path /path/to/logs --url http://localhost:8551 --jwt /path/to/jwt
./build/bin/rpc_int replay-tx --start 1000000:0 --method 0
./build/bin/rpc_int scan-block-receipts --url http://localhost:8545 --start-block 100 --end-block 200
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

**Subcommands** — `rpc_int` also serves as a host for standalone tool subcommands (ported from Python scripts in `src/rpctests/`). Dispatch is at the top of `cmd/integration/main.go`: if `os.Args[1]` matches a known subcommand, it delegates to a `urfave/cli/v2` app; otherwise falls through to the existing flag-based test runner. Subcommand implementations live in `internal/tools/`, one file per subcommand.

**Internal packages** under `internal/`:
- `internal/archive/` — Extract test fixtures from tar/gzip/bzip2 archives
- `internal/jsondiff/` — Pure Go JSON diff with colored output
- `internal/rpc/` — HTTP/WebSocket JSON-RPC client with JWT auth and compression support. Includes `wsconn.go` for persistent WebSocket connections (send/receive/call JSON-RPC).
- `internal/compare/` — Response comparison (exact match, JSON diff, external diff)
- `internal/config/` — Configuration, CLI flag parsing, JWT secret management
- `internal/filter/` — Test filtering (API name, pattern, exclusion, latest block)
- `internal/runner/` — Parallel test orchestration (worker pool, scheduling, stats)
- `internal/testdata/` — Test discovery, fixture loading, types
- `internal/perf/` — Performance test support (Vegeta integration, reporting)
- `internal/tools/` — Subcommand implementations (block-by-number, empty-blocks, filter-changes, latest-block-logs, subscriptions, graphql, replay-request, replay-tx, scan-block-receipts)
- `internal/eth/` — Ethereum primitives: RLP encoding, Keccak256, MPT (Modified Merkle-Patricia Trie) for computing receipts root hashes

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

Go 1.24. Key libraries: `gorilla/websocket` (WebSocket transport), `tsenart/vegeta/v12` (load testing), `urfave/cli/v2` (CLI framework for subcommands), `golang-jwt/jwt/v5` (JWT auth), `dsnet/compress` (bzip2), `golang.org/x/crypto` (Keccak256 for MPT).

**Constraint: `github.com/ethereum/go-ethereum` must NOT be added as a dependency.** Ethereum primitives (RLP, Keccak256, MPT) are implemented from scratch in `internal/eth/`.

Python 3.10+ with `requirements.txt` for legacy runner and standalone tools in `src/rpctests/`.

## Known Issues & Gotchas

- **RLP encoding of already-encoded items**: When building RLP lists containing items that are already RLP-encoded (e.g., logs from `encodeLog()`), use `rlpEncodeListFromRLP()` which treats items as pre-encoded. Do NOT use `rlpEncodeBytes()` on them — that wraps the RLP list as a byte string, double-encoding it.
- **Erigon old-block receipts**: On some Erigon nodes, `eth_getBlockReceipts` for old blocks (e.g., sepolia block 999991) returns receipt data that doesn't match the block header's `receiptsRoot`. This is an Erigon issue (confirmed: go-ethereum's own `types.DeriveSha` also fails on the same data). Recent blocks work correctly.
- **WebSocket subscriptions shutdown**: When using `eth_subscribe`, don't send `eth_unsubscribe` during shutdown — it races with the notification read loop. Instead, signal a done channel then close the connection to break `RecvJSON`.
- **GraphQL content type**: Erigon's GraphQL endpoint requires `application/json` with `{"query":"..."}` body, not `application/graphql`.