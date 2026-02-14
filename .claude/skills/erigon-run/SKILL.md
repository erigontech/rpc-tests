---
name: erigon-run
description: Use to run Erigon on an existing datadir. Use when the user wants to exercise the `rpc-tests` binaries (`rpc_int`, `rpc_perf`) against real server.
allowed-tools: Bash, Read, Glob
---

# Erigon Run

## Overview
The `erigon` command runs Erigon on an existing Erigon datadir.

## Command Syntax

```bash
cd <erigon_home> && ./build/bin/erigon --datadir=<path> --http.api admin,debug,eth,parity,erigon,trace,web3,txpool,ots,net --ws [other-flags]
```

## Required Flags

- `--datadir`: Path to the Erigon datadir (required)

## Usage Patterns

### Change HTTP port
```bash
cd <erigon_home_path> && ./build/bin/erigon --datadir=<datadir_path> --http.port=8546
```

### WebSocket support
```bash
cd <erigon_home_path> && ./build/bin/erigon --datadir=<datadir_path> --ws
```

## Important Considerations

### Before Running
1. **Ask for Erigon home**: Ask the user which Erigon home folder to use if not already provided
2. **Stop Erigon and RpcDaemon**: Ensure Erigon and/or RpcDaemon are not running on the target datadir
3. **Ensure Erigon binary is built**: run `make erigon` to build it

### After Running
1. Wait until the HTTP port (value provided with --http.port or default 8545) is reachable


## Workflow

When the user wants to run Erigon:

1. **Confirm parameters**
    - Ask for Erigon home path to use if not provided or know in context
    - Ask for target datadir path

2. **Safety checks**
    - Verify Erigon home <erigon_home_path> exists
    - Verify datadir <datadir_path> exists
    - Check if Erigon and/or RpcDaemon are running (should not be)


## Error Handling

Common issues:
- **"datadir not found"**: Verify the path is correct
- **"database locked"**: Stop Erigon process first


## Examples

### Example 1: All API namespaces and WebSocket enabled
```bash
cd ../erigon_devel && ./build/bin/erigon --datadir=~/Library/erigon-eth-mainnet --http.api admin,debug,eth,parity,erigon,trace,web3,txpool,ots,net --ws
```


## Tips

- If building from source, use `make erigon` within <erigon_home_path> to build the binary at `build/bin/erigon`
