# debug_traceBlockByNumber with the default struct logger on recent blocks: large responses. Geth and
# Nethermind keep the state of the recent blocks only, so pass --max-blocks to stay inside it. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/debug_traceBlockByNumber_structLog.profile --max-blocks 32 --counts N --verify --out debug_traceBlockByNumber_structLog.jsonl
#   rpc_perf -p debug_traceBlockByNumber_structLog.jsonl -y debug_traceBlockByNumber ...

[methods]
debug_traceBlockByNumber  100%
