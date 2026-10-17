# eth_getLogs with topics only, no address: the topic index alone, where events such as Transfer are
# dense. Each request matches a real log of the window of recent blocks (--max-blocks, default 3000),
# over up to 10 blocks, so none returns empty. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_topics.profile --counts N --verify --out eth_getLogs_topics.tar
#   rpc_perf -p eth_getLogs_topics.tar -y eth_getLogs ...

[methods]
eth_getLogs  100%  filter=topics  range=10
