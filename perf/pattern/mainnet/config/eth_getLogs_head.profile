# eth_getLogs on one block at a time, every filter shape, from the 100 most recent blocks: indexers
# and bots following the head of the chain. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_head.profile --counts N --verify --out eth_getLogs_head.tar
#   rpc_perf -p eth_getLogs_head.tar -y eth_getLogs ...

[methods]
eth_getLogs  100%  range=1  blocks=100
