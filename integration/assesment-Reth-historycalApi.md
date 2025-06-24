

```
# Reth vs. Erigon API Test Report

Summary of the execution of Reth tests against the answers returned by Erigon without considering the possible error strings in case of failures:

Test time-elapsed:            0:02:13.974115
TAvailable tests:              1243
Available tested api:         110
Number of loop:               1
Number of executed tests:     1206
Number of NOT executed tests: 38
Number of success tests:      462
Number of failed tests:       744


This report summarizes the discrepancies and issues found during API testing between Reth and Erigon on historical blocks, focusing on failed tests, total tests, and specific notes for each api.

| API Name | Failed Tests | Total Tests | Notes |
|---|---|---|---|
| debug_accountAt | 18 | 18 | not implemented |
| debug_accountRange | 20 | 20 | requires an additional parameter "incompletes", after new parameter returns null in any case |
| debug_getModifiedAccountByHash | 19 | 19 | returns null |
| debug_getModifiedAccountByNumber | 19 | 19 | after fix on parameter type from hex string to u64 now returns in any case null |
| debug_getRawHeader | 1 | 6 | test_06 on non-existent block Erigon returns an error, Reth returns 0x |
| debug_getRawTransaction | 1 | 6 | test_06 on non-existent hash Erigon returns 0x, Reth null |
| debug_storageRangeAt | 15 | 15 | Reth returns always null |
| debug_traceBlockByHash | 12 | 20 | Reth memory section contains data while Erigon contains null;d many storage fields are missing |
| debug_traceBlockByNumber | 24 | 29 | bn needs to be converted to u64  |
| debug_traceCall | 18 | 25 | all memory section and many storage fields are missing |
| debug_traceCallMany | 10 | 12 | all memory section and many storage fields are missing |
| debug_traceTransaction | 120 | 128 | all memory section and many storage fields are missing, there's an extra 'refund' field and an 'error' field that sometimes appears |
| erigon_blockNumber | 6 | 6 | not implemented |
| erigon_cacheCheck | 1 | 1 | not implemented |
| erigon_forks | 1 | 1 | not implemented |
| erigon_getBalanceChangesInBlock | 10 | 10 | not implemented |
| erigon_getBlockByTimestamp | 7 | 7 | not implemented |
| erigon_getBlockReceiptsByBlockHash | 11 | 11 | not implemented |
| erigon_getHeaderByHash | 6 | 6 | not implemented |
| erigon_getHeaderByNumber | 8 | 8 | has fewer fields and one extra field (size) |
| erigon_getLatestLogs | 38 | 38 | not implemented |
| erigon_getLogsByHash | 10 | 10 | not implemented |
| erigon_nodeInfo | 1 | 1 | not implemented |
| eth_call | 1 | 25 | one test Erigon retuns error, Reth 0x |
| eth_callBundle | 15 | 15 | error on request parameter |
| eth_callMany | 4 | 15 | Erigon value field is "", Reth 0x; some transaction are OK in Erigon fails in Reth |
| eth_createAccessList | 12 | 18 | "transactionType not supported"; some gasUsed are different; some fields missing |
| eth_feeHistory | 3 | 21 | added empty reward list, possibly also in new blocks |
| eth_getBalance | 1 | 25 | invalid account on invalid block, Erigon returns error, Reth 0x00 |
| eth_getBlockReceipt | 7 | 10 | Reth has an extra blockTimestamp field |
| eth_getLogs | 13 | 20 | Reth has an extra blockTimestamp field |
| eth_getRawTransactionByBlockHashAndIndex | 2 | 10 | in case of error Erigon returns 0x, Reth null |
| eth_getRawTransactionByBlockNumberAndIndex | 2 | 11 | in case of error Erigon returns 0x, Reth null |
| eth_getTransactionCount | 2 | 6 | does not support safe and finalized |
| eth_getTransactionReceipt | 3 | 10 | Reth added blockTimestamp field |
| eth_getUncleCountByBlockNumber | 3 | 6 | Erigon returns 0x0, Reth null |
| eth_protocolVersion | 1 | 1 | protocolVersion 43 vs 5 |
| eth_submitHashrate | 1 | 1 | Erigon returns error, Reth false |
| ots_getBlockDetails | 6 | 7 | Reth issuance and reward are 0x0, Erigon logsBloom is always null, in case of error Erigon string, Reth null |
| ots_getBlockDetailsByHash | 4 | 4 | Reth issuance and reward are 0x0, Erigon logsBloom is always null |
| ots_getBlockTransactions | 7 | 7 | Reth has an extra timestamp field, logsBloom valid; Erigon logsBloom is null, in case of error Erigon string, Reth null |
| ots_getInternalOperations | 11 | 15 | one more or one less operation indicated, error handling is different (Reth empty response, Erigon error message) |
| ots_getTransactionError | 6 | 15 | In 5 cases Erigon returns 0x while Reth null, also in case of error Erigon message and Reth null |
| ots_hasCode | 2 | 10 | In case of error Erigon returns error message, Reth true/false |
| ots_searchTransactionsAfter | 19 | 20 | Reth not implemented |
| ots_searchTransactionsBefore | 19 | 20 | Reth not implemented |
| ots_traceTransaction | 19 | 22 | there are extra transactions, some values are 0x00, Erigon instead marks as null |
| parity_listStorageKeys | 23 | 23 | not implemented |
| trace_block | 21 | 24 | Reth input parameter error |
| trace_call | 25 | 26 | Reth and Erigon are discordant for some (ok and fail); the mem section which is set to null in Erigon instead contains valid data, some stateDiff balances do not match, creationMethod added, some gasLeft do not match, some errors do not match, some codes are different (0x vs valid code) |
| trace_callMany | 13 | 15 | Reth and Erigon are discordant for some (ok and fail); the mem field which is set to null in Erigon instead contains valid data, some stateDiff balances do not match, some gasLeft do not match, some errors do not match |
| trace_filter | 13 | 24 | CreationMethod added, some error strings, in case block not found Reth returns empty, Erigon error message |
| trace_get | 27 | 34 | For many tests from/to/gas/input/tracedAddress do not match, while txHash and txBlock match |
| trace_replayBlockTransactions | 31 | 35 | Reth for many tests says response too big, mem field Erigon null, Reth has value, gasLeft do not match, contract codes 0x on one side and valid code on the other |
| trace_replayTransaction | 51 | 51 | Reth has extra txHash field, mem not null, gasLeft fields are often different by 3/10 units, sometimes stateDiffs are different |
| trace_transaction | 9 | 47 | CreationMethod added and some errors do not match |
```
