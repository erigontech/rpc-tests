# debug_traceBlockByNumber with the callTracer
#
# Geth and Nethermind keep the state of the recent blocks only, so pass --max-blocks to stay inside it. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/debug_traceBlockByNumber_callTracer.profile --max-blocks 32 --counts N --verify --out debug_traceBlockByNumber_callTracer.jsonl
#   rpc_perf -p debug_traceBlockByNumber_callTracer.jsonl -y debug_traceBlockByNumber ...

[methods]
debug_traceBlockByNumber  100%  tracer=callTracer
