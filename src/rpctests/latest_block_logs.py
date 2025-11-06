#!/usr/bin/env python3
"""
This script repeatedly queries an Ethereum node for the latest block using eth_getBlockByNumber, and calls eth_getLogs
with the latest block hash to check if the logs list is empty or an error occurs. Runs until interrupted with Ctrl+C.
"""

import argparse
import asyncio
import hexbytes
import logging
import signal
import sys

from .common import http

EMPTY_ROOT = hexbytes.HexBytes("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


async def wait_for(fut, timeout):
    try:
        # Wait for the future event OR the timeout, whichever comes first
        await asyncio.wait_for(fut, timeout)
    except asyncio.TimeoutError:
        pass


async def main():
    """Main function to query latest block logs via HTTP."""

    # Parse command-line arguments
    parser = argparse.ArgumentParser(
        description="Connects to an Ethereum node via HTTP and checks eth_getLogs for the latest block."
    )
    parser.add_argument(
        "--node_url",
        type=str,
        nargs='?',  # Make the argument optional
        default="http://127.0.0.1:8545",  # Set the default value
        help="The HTTP URL of the Ethereum node (default: http://127.0.0.1:8545)",
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

    client = http.Client(args.node_url, args.ca_file)

    # Setup signal handler for graceful shutdown
    shutdown_event = asyncio.Event()

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
        logger.info("Query latest block logs started... Press Ctrl+C to stop.")

        block_number = 0
        while not shutdown_event.is_set():
            try:
                # Get the latest block (header only)
                latest_block = await client.w3.eth.get_block("latest", full_transactions=False)
                if latest_block.number == block_number:
                    await wait_for(shutdown_event.wait(), sleep_time)
                    continue

                logger.info(f"Latest block is {latest_block.number}")
                block_number = latest_block.number
                block_hash = latest_block.hash
                receipts_root = latest_block.receiptsRoot

                # Immediately call eth_getLogs with the block hash
                logs = await client.w3.eth.get_logs({"blockHash": block_hash})

                # Break when result is an empty list and block receiptsRoot indicates there are some receipts
                if logs:
                    logger.info(f"Block {block_number}: eth_getLogs returned {len(logs)} log(s).")
                elif receipts_root is not EMPTY_ROOT:
                    receipts = await client.w3.eth.get_block_receipts(block_number)
                    num_logs = sum(len(receipt.logs) for receipt in receipts)
                    logger.warning(f"⚠️ Block {block_number}: eth_getLogs returned 0 logs but there are {num_logs}")
                    break
            except Exception as e:
                # Log any error during get_block or get_logs
                logger.error(f"❌ get_block/get_logs for block {block_number} failed: {e}")
                if not shutdown_event.is_set():
                    await wait_for(shutdown_event.wait(), sleep_time)

        logger.info("Query latest block logs terminated.")
    except Exception as e:
        logger.error(f"❌ Unexpected application error: {e}")
        status = 1
    finally:
        sys.exit(status)


if __name__ == "__main__":
    """ 
    Usage: 
    python latest_block_logs.py [--node_url] [--ca_file] [--sleep_time]
    or (if part of a package):
    python -m your_package_name.latest_block_logs [--node_url] [--ca_file] [--sleep_time]
    """
    asyncio.run(main())
