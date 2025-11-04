#!/usr/bin/env python3
"""
This script connects to an Ethereum node via WebSocket, repeatedly queries the latest block, and calls eth_getLogs
with the latest block hash to check if the logs list is empty or an error occurs. Runs until interrupted with Ctrl+C.
"""

import argparse
import asyncio
import logging
import signal
import sys

from .common import websocket

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


async def main():
    """Main function to query latest block logs via WebSocket."""

    # Parse command-line arguments
    parser = argparse.ArgumentParser(
        description="Connects to an Ethereum node via WebSocket and checks eth_getLogs for the latest block."
    )
    parser.add_argument(
        "--websocket_url",
        type=str,
        nargs='?',  # Make the argument optional
        default="ws://127.0.0.1:8545",  # Set the default value
        help="The WebSocket URL of the Ethereum node (default: ws://127.0.0.1:8545)",
    )
    parser.add_argument(
        "--ca_file",
        type=str,
        nargs='?',  # Make the argument optional
        default=None,  # Set the default value
        help="The path to your WSS server's *public* certificate file .pem or .crt (default: None)",
    )
    parser.add_argument(
        "--sleep_time",
        type=float,
        nargs='?',  # Make the argument optional
        default=0.1,  # Set the default value
        help="The sleep time interval between latest block queries in seconds (default: 0.1)",
    )
    args = parser.parse_args()

    # Create the WebSocket client
    client = websocket.Client(args.websocket_url, args.ca_file)
    shutdown_event = asyncio.Event()

    # Setup signal handler for graceful shutdown
    async def signal_handler():
        print("")
        logger.info("🏁 Received interrupt signal (Ctrl+C). Shutting down...")
        shutdown_event.set()

    loop = asyncio.get_running_loop()
    for sig in [signal.SIGINT, signal.SIGTERM]:
        loop.add_signal_handler(sig, lambda: asyncio.create_task(signal_handler()))

    sleep_time = args.sleep_time
    status = 0
    try:
        # Connect to Ethereum node
        await client.connect()
        logger.info(f"✅ Successfully connected to Ethereum node at {client.node_url}")

        logger.info("Query latest block logs started... Press Ctrl+C to stop.")

        block_number = 0
        while not shutdown_event.is_set():
            try:
                # Get the latest block (header only)
                latest_block = await client.w3.eth.get_block("latest", full_transactions=False)
                logger.info(f"Latest block is {latest_block.number}")
                if latest_block.number == block_number:
                    await asyncio.sleep(sleep_time)  # Avoid busy loop
                    continue

                block_number = latest_block.number
                block_hash = latest_block.hash

                if not block_hash:
                    logger.warning(f"Got block {block_number} but it has no hash (pending block?). Skipping.")
                    await asyncio.sleep(sleep_time)  # Avoid spamming pending blocks
                    continue

                # Immediately call eth_getLogs with the block hash
                logs = await client.w3.eth.get_logs({"blockHash": block_hash})

                # Check if result is an empty list
                if logs:
                    logger.info(f"Block {block_number}: eth_getLogs returned {len(logs)} log(s).")
                else:
                    logger.warning(f"✅ Block {block_number}: eth_getLogs returned an empty list (zero logs).")
            except Exception as e:
                # Log any error during get_block or get_logs
                logger.error(f"❌ Error while processing block {block_number}: {e}")
                # Add a small delay on error to avoid spamming
                if not shutdown_event.is_set():
                    await asyncio.sleep(sleep_time)

        logger.info("Query logs loop terminated.")

    except KeyboardInterrupt:
        # This is expected on Ctrl+C, but the signal handler already logs.
        logger.info("Interruption processed.")
    except Exception as e:
        logger.error(f"❌ Unexpected application error: {e}")
        status = 1
    finally:
        # Cleanup
        await client.disconnect()
        sys.exit(status)


if __name__ == "__main__":
    """ 
    Usage: 
    python latest_block_logs.py [websocket_url] [ca_file]
    or (if part of a package):
    python -m your_package_name.latest_block_logs [websocket_url] [ca_file] 
    """
    asyncio.run(main())
