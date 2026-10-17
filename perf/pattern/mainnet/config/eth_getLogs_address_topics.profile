# eth_getLogs with the address and topics: one event of one contract, the address and topic indexes
# intersected. Each request matches a real log of the window of recent blocks (--max-blocks,
# default 3000), over up to 100 blocks, so none returns empty. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_address_topics.profile --counts N --verify --out eth_getLogs_address_topics.tar
#   rpc_perf -p eth_getLogs_address_topics.tar -y eth_getLogs ...

[methods]
eth_getLogs  100%  filter=address+topics  range=100
