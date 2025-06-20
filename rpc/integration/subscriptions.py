#!/usr/bin/env python3
"""
Ethereum WebSocket Subscription Script

This script connects to an Ethereum node via WebSocket, sends an eth_subscribe
JSON-RPC request, and listens for notifications until interrupted with Ctrl+C.

Requirements:
    pip install asyncio eth_typing web3
"""

import argparse
import asyncio
import eth_typing
import logging
import signal
import ssl
import sys
import urllib.parse
import web3
import web3.utils

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


class EthereumWebSocketSubscriber:
    def __init__(self, websocket_url: str, server_ca_file: str):
        """ Initialize the WebSocket subscriber.
            websocket_url (str): WebSocket URL of the Ethereum node
            server_ca_file (str): *public* certificate file of the WSS server
        """
        self.websocket_url = websocket_url
        self.server_ca_file = server_ca_file
        self.w3 = None

    async def connect(self):
        """Establish WebSocket connection to Ethereum node."""
        try:
            # Create WebSocket provider
            parsed_url = urllib.parse.urlparse(self.websocket_url)
            if parsed_url.scheme not in ['ws', 'wss']:
                raise ValueError(f"Invalid WebSocket URL scheme: {parsed_url.scheme}. Must be 'ws' or 'wss'.")
            if parsed_url.scheme == 'wss':
                if self.server_ca_file is None:
                    raise ValueError(f"You must specify a non-empty server CA file as second parameter.")
                logger.info(f"Server CA file: {self.server_ca_file}")
                ssl_context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
                ssl_context.load_verify_locations(cafile=self.server_ca_file)
                ssl_context.check_hostname = False
                websocket_kwargs = {"ssl": ssl_context}
            else:
                websocket_kwargs = None
            provider = web3.WebSocketProvider(self.websocket_url, websocket_kwargs, max_connection_retries=1)
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

    # Parse command-line arguments
    parser = argparse.ArgumentParser(
        description="Connects to an Ethereum node via WebSocket and subscribes to events."
    )
    parser.add_argument(
        "websocket_url",
        type=str,
        nargs='?',  # Make the argument optional
        default="ws://127.0.0.1:8545",  # Set the default value
        help="The WebSocket URL of the Ethereum node (default: ws://127.0.0.1:8545)",
    )
    parser.add_argument(
        "ca_file",
        type=str,
        nargs='?',  # Make the argument optional
        default=None,  # Set the default value
        help="The path to your WSS server's *public* certificate file .pem or .crt (default: None)",
    )
    args = parser.parse_args()

    # Create the WebSocket event subscriber
    subscriber = EthereumWebSocketSubscriber(args.websocket_url, args.ca_file)

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
    """ Usage: python subscriptions.py """
    asyncio.run(main())
