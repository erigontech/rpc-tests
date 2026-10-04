# Mixed mainnet load: method shares of a provider's RPC traffic (from erigon cmd/rpctest/mixedLoad.txt).
# Weights are relative. Usage:
#   rpc_pattern_gen --profile perf/pattern/mainnet/config/mixed.profile --counts N --verify --out mixed.tar
#   rpc_perf -p mixed.tar -y mixed ...

[methods]
eth_call                   35.8%
eth_getBlockByNumber       19.9%
eth_getLogs                6.42%
eth_getTransactionReceipt  5.74%
eth_blockNumber            4.26%
debug_traceBlockByHash     3.91%   tracer=callTracer
eth_getBalance             3.73%
net_listening              3.42%
eth_getTransactionByHash   2.62%
eth_getBlockReceipts       2.25%
eth_getBlockByHash         1.45%
eth_chainId                1.22%
eth_getTransactionCount    1.07%

# Most called contracts (tx "to") in 500 recent mainnet blocks (24677092..24677591, 194456 txs).
# Only these txs, and the blocks holding them, are used; eth_getLogs filters on these addresses.
[contracts]
0xdac17f958d2ee523a2206206994597c13d831ec7   USDT                       14.4% of txs
0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48   USDC                        7.6%
0x66a9893cc07d91d95644aedd05d03f95e1dba8af   Uniswap Universal Router    0.6%
0x7a250d5630b4cf539739df2c5dacb4c659f2488d   Uniswap V2 Router 02        0.5%
0x0000000071727de22e5e9d8baf0edac6f37da032   ERC-4337 EntryPoint v0.7    0.5%
0x6b175474e89094c44da98b954eedeac495271d0f   DAI                         0.3%
