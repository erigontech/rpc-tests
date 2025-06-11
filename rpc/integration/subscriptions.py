#!/usr/bin/env python3
"""
Ethereum WebSocket Subscription Script

This script connects to an Ethereum node via WebSocket, sends an eth_subscribe
JSON-RPC request, and listens for notifications until interrupted with Ctrl+C.

Requirements:
    pip install asyncio eth_typing web3
"""

import asyncio
import eth_typing
import logging
import signal
import sys
import web3
import web3.utils

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


class EthereumWebSocketSubscriber:
    def __init__(self, websocket_url):
        """ Initialize the WebSocket subscriber.
            websocket_url (str): WebSocket URL of the Ethereum node
        """
        self.websocket_url = websocket_url
        self.w3 = None
        self.subscription_id = None
        self.running = False

    async def connect(self):
        """Establish WebSocket connection to Ethereum node."""
        try:
            # Create WebSocket provider
            provider = web3.WebSocketProvider(self.websocket_url)
            self.w3 = web3.AsyncWeb3(provider)

            # Connect to the provider
            await provider.connect()

            # Test connection
            if not await self.w3.is_connected():
                raise ConnectionError("Failed to connect to Ethereum node")

            latest_block = await self.w3.eth.block_number
            logger.info(f"Connected to Ethereum node at {self.websocket_url}")
            logger.info(f"Latest block: {latest_block}")

        except Exception as e:
            logger.error(f"Connection failed: {e}")
            raise

    async def subscribe(self, subscriptions):
        """ """
        return await self.w3.subscription_manager.subscribe(subscriptions)

    async def unsubscribe(self):
        """ """
        return await self.w3.subscription_manager.unsubscribe(self.w3.subscription_manager.subscriptions)

    async def handle_subscriptions(self, run_forever=False):
        """ """
        return await self.w3.subscription_manager.handle_subscriptions(run_forever)

    async def disconnect(self):
        """Close the WebSocket connection."""
        if self.w3 and self.w3.provider:
            try:
                await self.w3.provider.disconnect()
                logger.info("WebSocket connection closed")
            except Exception as e:
                logger.error(f"Error during disconnect: {e}")


async def main():
    """Main function to run the WebSocket subscription."""

    # Configuration - Replace with your actual WebSocket URL
    # - Infura: wss://mainnet.infura.io/ws/v3/YOUR_PROJECT_ID
    # - Alchemy: wss://eth-mainnet.ws.alchemyapi.io/v2/YOUR_API_KEY
    # - Local: ws://localhost:8546
    websocket_url = "ws://127.0.0.1:8546"
    subscriber = EthereumWebSocketSubscriber(websocket_url)

    # Setup signal handler for graceful shutdown
    async def signal_handler():
        logger.info("Received interrupt signal (Ctrl+C)")
        await subscriber.unsubscribe()

    loop = asyncio.get_running_loop()
    for sig in [signal.SIGINT, signal.SIGTERM]:
        loop.add_signal_handler(sig, lambda: asyncio.create_task(signal_handler()))

    # Prepare the subscription event handlers
    async def new_heads_handler(handler_context: web3.utils.subscriptions.NewHeadsSubscriptionContext) -> None:
        header = handler_context.result
        print(f"New block header: {header}\n")

    async def log_handler(handler_context: web3.utils.subscriptions.LogsSubscriptionContext) -> None:
        log_receipt = handler_context.result
        print(f"Log receipt: {log_receipt}\n")

    status = 0
    try:
        # Connect to Ethereum node
        await subscriber.connect()

        # Subscribe to event notifications for new headers and USDT logs
        subscriptions = await subscriber.subscribe(
            [
                web3.utils.subscriptions.NewHeadsSubscription(
                    label="new-heads-mainnet",
                    handler=new_heads_handler),
                web3.utils.subscriptions.LogsSubscription(
                    label="USDT transfers",
                    address=subscriber.w3.to_checksum_address("0xdac17f958d2ee523a2206206994597c13d831ec7"),
                    topics=[eth_typing.HexStr("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")],
                    handler=log_handler),
            ]
        )
        logger.info(f"Handle subscriptions started: {len(subscriptions)}")

        # Listen for incoming subscription events
        await subscriber.handle_subscriptions()
        logger.info(f"Handle subscriptions terminated")
    except KeyboardInterrupt:
        logger.info("Interrupted by user")
    except Exception as e:
        logger.error(f"Unexpected error: {e}")
        status = e
    finally:
        # Cleanup
        await subscriber.disconnect()
        sys.exit(status)


if __name__ == "__main__":
    """ Usage: python eth_websocket_subscribe.py """
    asyncio.run(main())
