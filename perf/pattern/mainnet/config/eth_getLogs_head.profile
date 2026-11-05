# eth_getLogs on one block at a time, from the 100 most recent blocks, every filter shape (address,
# address and topics, topics only): indexers and bots following the head of the chain
#
# Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_head.profile --counts N --verify --out eth_getLogs_head.jsonl
#   rpc_perf -p eth_getLogs_head.jsonl -y eth_getLogs ...

[methods]
eth_getLogs  100%  range=1  blocks=100
