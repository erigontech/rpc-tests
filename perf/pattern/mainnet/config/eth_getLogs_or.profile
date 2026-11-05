# eth_getLogs with lists of alternatives: up to 16 addresses and up to 16 values per topic position,
# every filter shape, null positions included, over up to 100 blocks; the list lengths are mostly
# short, a few long, as indexers that watch many contracts or several events
#
# Each list holds the value of a real log and others taken from the logs of the same block, so none
# returns empty. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_or.profile --counts N --verify --out eth_getLogs_or.jsonl
#   rpc_perf -p eth_getLogs_or.jsonl -y eth_getLogs ...

[methods]
eth_getLogs  100%  or=16  range=100
