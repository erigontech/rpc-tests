# eth_getLogs with the address and topics: one event of one contract, the address and topic indexes
# intersected; each request matches a real log, over up to 100 blocks, so none returns empty
#
# Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_address_topics.profile --counts N --verify --out eth_getLogs_address_topics.jsonl
#   rpc_perf -p eth_getLogs_address_topics.jsonl -y eth_getLogs ...

[methods]
eth_getLogs  100%  filter=address+topics  range=100
