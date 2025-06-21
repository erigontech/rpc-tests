#!/usr/bin/env python3
"""
Ethereum WebSocket getBlockByNumber Script

This script connects to an Ethereum node via WebSocket and queries latest/safe/finalized blocks
until interrupted with Ctrl+C.

Requirements:
    pip install asyncio web3
"""

import argparse
import asyncio
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


class EthereumWebSocketClient:
    def __init__(self, websocket_url: str, server_ca_file: str):
        """ Initialize the WebSocket subscriber.
            websocket_url (str): WebSocket URL of the Ethereum node
            server_ca_file (str): *public* certificate file of the WSS server
        """
        self.websocket_url = websocket_url
        self.server_ca_file = server_ca_file
        self.w3 = None
        self.running = False

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

    async def query_blocks(self, delay: float):
        """ """
        self.running = True
        try:
            while self.running:
                # Get the latest/safe/finalized blocks just to extract the number but whatever
                latest_block = await self.w3.eth.get_block("latest", full_transactions=False)
                safe_block = await self.w3.eth.get_block("safe", full_transactions=False)
                finalized_block = await self.w3.eth.get_block("finalized", full_transactions=False)
                latest_number = latest_block.number
                safe_number = safe_block.number
                finalized_number = finalized_block.number

                logger.info(f"Block latest: {latest_number} safe: {safe_number} finalized: {finalized_number}")

                # Wait for some time before iterating
                await asyncio.sleep(delay)
        except Exception as e:
            logger.error(f"query_blocks failed: {e}")
            raise

    async def stop(self):
        """ """
        self.running = False

    async def disconnect(self):
        """Close the WebSocket connection."""
        if self.w3 and self.w3.provider:
            try:
                await self.w3.provider.disconnect()
                logger.info("WebSocket connection closed")
            except Exception as e:
                logger.error(f"Error during disconnect: {e}")


async def main():
    """Main function to query the blocks via WebSocket."""

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

    # Create the WebSocket client
    client = EthereumWebSocketClient(args.websocket_url, args.ca_file)

    # Setup signal handler for graceful shutdown
    async def signal_handler():
        logger.info("Received interrupt signal (Ctrl+C)")
        await client.stop()

    loop = asyncio.get_running_loop()
    for sig in [signal.SIGINT, signal.SIGTERM]:
        loop.add_signal_handler(sig, lambda: asyncio.create_task(signal_handler()))

    status = 0
    try:
        # Connect to Ethereum node
        await client.connect()

        # Run a timed loop of eth_blockByNumber queries for latest/safe/finalized blocks
        logger.info(f"Query blocks started")

        await client.query_blocks(2)

        logger.info(f"Query blocks terminated")
    except KeyboardInterrupt:
        logger.info("Interrupted by user")
    except Exception as e:
        logger.error(f"Unexpected error: {e}")
        status = 1
    finally:
        # Cleanup
        await client.disconnect()
        sys.exit(status)


if __name__ == "__main__":
    """ Usage: python block_by_number.py """
    asyncio.run(main())
