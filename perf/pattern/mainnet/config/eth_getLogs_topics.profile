# eth_getLogs with topics only, no address: the topic index alone, where events such as Transfer are
# dense; each request matches a real log, over up to 10 blocks, so none returns empty
#
# Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_topics.profile --counts N --verify --out eth_getLogs_topics.jsonl
#   rpc_perf -p eth_getLogs_topics.jsonl -y eth_getLogs ...

[methods]
eth_getLogs  100%  filter=topics  range=10
