# eth_getLogs over wide ranges, up to 1000 blocks, with the address and topics keeping the result
# small: the cost of walking many blocks. With --verify the requests returning more logs than the
# node allows are dropped. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_wide.profile --counts N --verify --out eth_getLogs_wide.tar
#   rpc_perf -p eth_getLogs_wide.tar -y eth_getLogs ...

[methods]
eth_getLogs  100%  filter=address+topics  range=1000
