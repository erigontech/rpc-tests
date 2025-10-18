#!/usr/bin/env python3
"""
This script connects to an Ethereum node via WebSocket and finds last N empty blocks starting from latest
until interrupted with Ctrl+C.
"""

import argparse
import asyncio
import logging
import signal
import sys
import web3
import web3.exceptions
import web3.utils

from .common import websocket

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


async def main():
    """ Main asynchronous function to run the search of empty blocks. """

    # Parse command-line arguments
    parser = argparse.ArgumentParser(
        description="Connects to an Ethereum node via WebSocket and search last N empty blocks."
    )
    parser.add_argument(
        "num_empty_blocks",
        type=int,
        nargs='?',  # Make the argument optional
        default=10,
        help="The number of empty blocks to search for before stopping (default: 10)",
    )
    parser.add_argument(
        "ignore_withdrawals",
        type=bool,
        nargs='?',  # Make the argument optional
        default=False,
        help="Flag indicating to ignore withdrawals in determining if a block is empty (default: False)",
    )
    parser.add_argument(
        "websocket_url",
        type=str,
        nargs='?',  # Make the argument optional
        default="ws://127.0.0.1:8545",
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
    client = websocket.Client(args.websocket_url, args.ca_file)
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
        await client.connect()
        logger.info(f"✅ Successfully connected to Ethereum node at {client.node_url}")

        n, ignore_withdrawals = args.num_empty_blocks, args.ignore_withdrawals
        logger.info(f"Searching for the last {n} empty blocks...")

        # Get the latest block number
        latest_block_number = await client.w3.eth.block_number
        logger.info(f"Latest block number: {latest_block_number}")

        empty_blocks = []

        # Iterate backwards, fetching blocks asynchronously in parallel batches
        batch_size = 100

        current_block_number = latest_block_number
        while not shutdown_event.is_set() and len(empty_blocks) < n and current_block_number >= 0:
            # Determine the range of blocks for the current batch
            start_block = max(0, current_block_number - batch_size + 1)
            end_block = current_block_number

            # Create a list of asynchronous block fetching tasks for the current batch
            tasks = [
                client.w3.eth.get_block(block_num, full_transactions=False)
                for block_num in range(start_block, end_block + 1)
            ]

            # Execute the tasks concurrently
            batch_results = await asyncio.gather(*tasks)

            # Process results backward to maintain chronological order in the search
            for block in reversed(batch_results):
                if isinstance(block, web3.exceptions.Web3Exception) or isinstance(block, Exception):
                    # Handle any RPC errors gracefully
                    logger.warning(f"⚠️ Failed to fetch a block. Error: {block}")
                    continue

                # Check for empty blocks: no transactions and (after Shanghai HF) possibly no withdrawals
                no_transactions = len(block.transactions) == 0
                pre_shanghai = 'withdrawals' not in block
                if no_transactions and (not ignore_withdrawals or pre_shanghai or len(block.withdrawals) == 0):
                    empty_blocks.append(block.number)
                    logger.info(f"➡️ Block {block.number} is empty. Total found: {len(empty_blocks)}/{n}")
                    parent = await client.w3.eth.get_block(block.number - 1, full_transactions=False)
                    if parent is not None:
                        if block.stateRoot == parent.stateRoot:
                            logger.debug(f"➡️ stateRoot: {block.stateRoot.hex()} MATCHES")
                        else:
                            logger.debug(f"➡️ stateRoot: {block.stateRoot.hex()} DOES NOT MATCH [parent stateRoot: {parent.stateRoot.hex()}]")

                    if len(empty_blocks) == n:
                        logger.info(f"✅ Found last {n} empty blocks!")
                        break  # Exit the inner loop once N blocks are found

            if len(empty_blocks) == n:
                break  # Exit the outer loop

            # Set the next block number to search from
            current_block_number = start_block - 1

            if current_block_number % 100_000 == 0:
                logger.info(f"Reached block {current_block_number}...")
            if current_block_number < 0:
                logger.info("Reached genesis block (0). Stopping search.")
                break

        if not empty_blocks:
            logger.warning(f"⚠️ Could not find {n} empty blocks within the chain history.")
    except KeyboardInterrupt:
        logger.info("Interrupted by user")
    except Exception as e:
        logger.error(f"❌ Unexpected error: {e}")
        status = 1
    finally:
        await client.disconnect()
        sys.exit(status)

if __name__ == "__main__":
    asyncio.run(main())
