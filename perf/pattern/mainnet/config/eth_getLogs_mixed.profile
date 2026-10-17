# eth_getLogs only, as in mixed.profile: every filter shape (address, address+topics, topics), one at
# random per log, over up to 100 blocks around a real log of the contracts below, drawn from the
# window of recent blocks (--max-blocks, default 3000), so none returns empty. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/eth_getLogs_mixed.profile --counts N --verify --out eth_getLogs_mixed.tar
#   rpc_perf -p eth_getLogs_mixed.tar -y eth_getLogs ...

[methods]
eth_getLogs  100%  range=100

# The contracts of mixed.profile: only the logs they emit are used.
[contracts]
0xdac17f958d2ee523a2206206994597c13d831ec7   USDT
0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48   USDC
0x66a9893cc07d91d95644aedd05d03f95e1dba8af   Uniswap Universal Router
0x7a250d5630b4cf539739df2c5dacb4c659f2488d   Uniswap V2 Router 02
0x0000000071727de22e5e9d8baf0edac6f37da032   ERC-4337 EntryPoint v0.7
0x6b175474e89094c44da98b954eedeac495271d0f   DAI
