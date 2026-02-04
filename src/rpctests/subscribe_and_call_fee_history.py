import asyncio

from web3 import AsyncWeb3, WebSocketProvider


async def main():
    # rpc_url = "wss://rpc.gnosischain.com/wss"
    rpc_url = "ws://127.0.0.1:8545"

    async with AsyncWeb3(WebSocketProvider(rpc_url)) as w3:
        _ = await w3.eth.subscribe("newHeads")

        async for block_header in w3.socket.process_subscriptions():
            block_number = block_header.get("result").get("number")
            print(f"Block {block_number} announced via subscription")

            # Immediately try to call eth_feeHistory with this block number
            try:
                _ = await w3.eth.fee_history(
                    block_count=10,
                    newest_block=block_number,
                    reward_percentiles=[20],
                )
                print(f"✅ eth_feeHistory succeeded for block {block_number}")
            except Exception as e:
                error_msg = str(e)
                print(f"❌ eth_feeHistory FAILED for block {block_number} {error_msg}")


if __name__ == "__main__":
    asyncio.run(main())
