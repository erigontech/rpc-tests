#!/usr/bin/env python3
"""
This script connects to an Ethereum node via WebSocket and queries latest/safe/finalized blocks
until interrupted with Ctrl+C.
"""

import argparse
import asyncio
import logging
import signal
import sys

import rpc.common.websocket

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


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
    client = rpc.common.websocket.Client(args.websocket_url, args.ca_file)
    shutdown_event = asyncio.Event()

    # Setup signal handler for graceful shutdown
    async def signal_handler():
        print("")
        logger.info("🏁 Received interrupt signal (Ctrl+C)")
        shutdown_event.set()

    loop = asyncio.get_running_loop()
    for sig in [signal.SIGINT, signal.SIGTERM]:
        loop.add_signal_handler(sig, lambda: asyncio.create_task(signal_handler()))

    status = 0
    try:
        # Connect to Ethereum node
        await client.connect()
        logger.info(f"✅ Successfully connected to Ethereum node at {client.node_url}")

        # Run a timed loop of eth_blockByNumber queries for latest/safe/finalized blocks
        delay = 2
        logger.info(f"Query blocks started delay: {delay}sec")
        while not shutdown_event.is_set():
            # Get the latest/safe/finalized blocks just to extract the number but whatever
            latest_block = await client.w3.eth.get_block("latest", full_transactions=False)
            safe_block = await client.w3.eth.get_block("safe", full_transactions=False)
            finalized_block = await client.w3.eth.get_block("finalized", full_transactions=False)
            latest_number = latest_block.number
            safe_number = safe_block.number
            finalized_number = finalized_block.number

            logger.info(f"Block latest: {latest_number} safe: {safe_number} finalized: {finalized_number}")

            # Wait for some time before iterating
            await asyncio.sleep(delay)

        logger.info(f"Query blocks terminated")
    except KeyboardInterrupt:
        logger.info("Interrupted by user")
    except Exception as e:
        logger.error(f"❌ Unexpected error: {e}")
        status = 1
    finally:
        # Cleanup
        await client.disconnect()
        sys.exit(status)


if __name__ == "__main__":
    """ Usage: python block_by_number.py """
    asyncio.run(main())
