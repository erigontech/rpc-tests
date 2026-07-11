# testing_commitBlockV1

Tests for the `testing_commitBlockV1` RPC method proposed in
[ethereum/execution-apis#801](https://github.com/ethereum/execution-apis/pull/801).

The method builds a block from the supplied payload attributes on top of the current
canonical head, inserts it into the chain, and sets it as the new head — skipping the
`engine_newPayload` + `engine_forkchoiceUpdated` round-trip.

Params: `[payloadAttributes, transactions, extraData]`
- `transactions == []` → the block MUST be empty
- `transactions == null` → the client MAY build from its local mempool
- `transactions == [tx1, ...]` → the block MUST contain exactly these transactions, in order
- on failure the client MUST return a JSON-RPC error and MUST NOT move the head

## Test layout

These tests are **stateful and order-dependent**: each `testing_commitBlockV1` call
advances the canonical head, and later tests build on the head produced by earlier ones.
Run them **serially and all together**:

```bash
./build/bin/rpc_int -b hive -A testing_commitBlockV1 -S -c -f -E
```

`-E` (`--do-not-compare-error`) compares only the error `code` and ignores the
client-specific error message wording: with it, the suite passes on both geth and
erigon. Drop `-E` to also diff the exact error messages (then tests 02/06 only
match the client the fixtures were recorded with, i.e. geth).

Each fixture is one self-contained hive scenario: an array of JSON-RPC commands run
sequentially by the runner (the test fails at the first command whose response
mismatches):

| Test | Scenario (PR .io file) |
|------|------------------------|
| 01 | `commit-block-empty-transactions`: read head, commit empty block, verify new head |
| 02 | `commit-block-invalid-transaction`: commit with a bad-nonce tx → error, head unchanged |
| 03 | `commit-block-with-extra-data`: commit with non-empty `extraData`, verify it on the head |
| 04 | `commit-block-with-transactions`: commit with an explicit raw tx list, verify block content |
| 05 | `commit-block-z-from-mempool`: send tx to mempool, commit with `transactions=null`, verify |
| 06 | invalid params (empty `params`) → `-32602`, stateless |

Note: where the PR verifies via `eth_getBlockByHash`, these fixtures use
`eth_getBlockByNumber("latest")` instead, so no request depends on a previously
recorded block hash and requests survive re-recording unchanged.

## Recording workflow (geth → erigon)

The expected responses checked in are the execution-apis PR #801 reference values,
verified byte-for-byte against geth v1.17.5-unstable (testing_commitBlockV1 merged in
[go-ethereum#34995](https://github.com/ethereum/go-ethereum/pull/34995)) on the default
hive test chain (head `0x2d` at timestamp `0x1c2`, hash `0xe27a3e81…`). The only
exception is test 05 (from-mempool): the PR recorded it inside a full hive session
where other suites' leftover txs sat in the pool, so its commit and verify steps are
re-recorded here for a standalone run (block contains exactly the tx sent in step 1).
To reproduce or re-record:

1. Reset the chain to the initial state and start the node. For geth:
   `cd ~/silkworm/go-ethereum && ./start_hive_test.sh init && ./start_hive_test.sh`
2. Only if the chain differs from the default hive chain (head timestamp != `0x1c2`),
   rebase the commit timestamps on the node's head with `./rebase_timestamps.py` and
   expect to re-record every response. On the default chain the checked-in PR
   timestamps (`0x1ce`–`0x1f2`) are already correct — do NOT rebase.
3. Run the suite (command above). To re-record, copy each
   `integration/hive/results/testing_commitBlockV1/test_NN-response.json` into the
   `response` field of the matching fixture.
4. Reset the chain again and repeat with the other client (e.g. **erigon**): any diff
   is a behavioral difference between the two implementations.

Caveats:
- The tests mutate the head (4 blocks committed per run), so the chain MUST be reset
  to the same starting state before every run, or nothing reproduces — including
  `eth_sendRawTransaction` in test 05, whose sender nonce must be 0.
- The node must be started with the `testing` RPC namespace enabled
  (geth: `--http.api=...,testing`).
- geth must run with `--miner.gasprice=1`: the default floor (1e6 wei) is higher than
  the 500 wei priority fee of the recorded mempool tx, and test 05 would silently
  commit an empty block instead of including it.
- Test 05 (mempool build) is `speconly` in hive: block content depends on the mempool,
  so it is the most fragile against leftover state.
- Error messages (tests 02 and 06) are client-specific; a message-only diff between
  geth and erigon is expected and acceptable as long as an error is returned and the
  head does not move (run with `-E` to ignore the message).
