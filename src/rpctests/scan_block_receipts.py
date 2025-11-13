#!/usr/bin/env python3
"""
This script connects to an Ethereum node via HTTP and scans blocks to verify the receipts root.

It operates in two modes:
1.  Range Mode: Scans a specified range of blocks (--start_block to --end_block).
2.  Latest Block Mode: Continuously scans the 'latest' block, checking for reorgs and
    receipts root mismatches.

In both modes, it computes the receipts root from eth_getBlockReceipts and compares it
to the receiptsRoot hash in the block header. It stops if a mismatch is found.
In Latest Block Mode, it also stops if a chain reorg is detected.
"""

import argparse
import asyncio
import logging
import signal
import sys

from .common import http
from .eth.trie.receipt import compute_receipts_root

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


async def scan_block_range(client: http.Client, start_block, end_block: int, shutdown: asyncio.Event):
    """
    Scans a fixed range of blocks and verifies their receipts roots.
    Returns status: 0 on success, 1 on failure.
    """
    logger.info(f"🔍 Scanning block receipts from {start_block} to {end_block}...")

    status = 0

    for block_number in range(start_block, end_block + 1):
        if shutdown.is_set():
            logger.info("Scan terminated by user.")
            break

        try:
            # 1. Get the block header
            block = await client.w3.eth.get_block(block_number, full_transactions=False)
            if not block:
                logger.warning(f"Block {block_number} not found. Skipping.")
                continue

            header_receipts_root = block.receiptsRoot

            # 2. Get the block receipts
            receipts = await client.w3.eth.get_block_receipts(block_number)

            # 3. Compute the receipts root
            computed_receipts_root = compute_receipts_root(receipts)

            # 4. Compare the actual vs expected roots
            if computed_receipts_root == header_receipts_root:
                logger.info(f"✅ Block {block_number}: Receipts root verified ({len(receipts)} receipts).")
            else:
                logger.critical(f"🚨 Receipt root mismatch detected at block {block_number} 🚨")
                logger.critical(f"- expected header root: {header_receipts_root.hex()}")
                logger.critical(f"- actual computed root: {computed_receipts_root.hex()}")
                status = 1
                break
        except Exception as e:
            # Log any error during get_block or get_receipts and continue
            logger.error(f"❌ Error processing block {block_number}: {e}. Skipping this block.")

    if not shutdown.is_set():
        if status == 0:
            logger.info(f"✅ Successfully scanned and verified all receipts from {start_block} to {end_block}.")
        else:
            logger.info("Scan stopped due to root mismatch.")

    return status


async def scan_latest_blocks(client: http.Client, sleep_time: float, stop_at_reorg: bool, shutdown: asyncio.Event):
    """
    Scans the latest blocks, checks for reorgs, and verifies receipts roots.
    Runs until shutdown. Returns status: 0 on success, 1 on failure.
    """
    logger.info("🔍 Scanning latest blocks... Press Ctrl+C to stop.")

    status = 0
    previous_block_hash = None
    start_block_number = 0
    current_block_number = 0
    reorg_detected = False

    while not shutdown.is_set():
        try:
            # 1. Get the latest block header
            block = await client.w3.eth.get_block("latest", full_transactions=False)
            if block.number == current_block_number:
                await asyncio.sleep(sleep_time)  # Wait for a new block
                continue

            if start_block_number == 0:
                start_block_number = block.number
            current_block_number = block.number

            # 2. Check for chain reorg
            if previous_block_hash is not None and block.parentHash != previous_block_hash:
                logger.warning(f"⚠️ REORG DETECTED at block {current_block_number} ⚠️")
                logger.warning(f"- block {current_block_number} parentHash: {block.parentHash.hex()}")
                logger.warning(f"- expected (block {current_block_number - 1} hash): {previous_block_hash.hex()}")
                reorg_detected = True

            header_receipts_root = block.receiptsRoot

            # 3. Get the block receipts
            receipts = await client.w3.eth.get_block_receipts(current_block_number)

            # 4. Compute the receipts root
            computed_receipts_root = compute_receipts_root(receipts)

            # 5. Compare the actual vs expected roots
            if computed_receipts_root == header_receipts_root:
                if not reorg_detected:
                    logger.info(f"✅ Block {current_block_number}: Receipts root verified ({len(receipts)} receipts).")
                else:
                    logger.info(f"✅ Block {current_block_number}: Reorg detected, but receipts root IS valid.")
            else:
                logger.critical(f"🚨 Receipt roo mismatch detected at block {current_block_number} 🚨")
                logger.critical(f"- expected header root: {header_receipts_root.hex()}")
                logger.critical(f"- actual computed root: {computed_receipts_root.hex()}")
                status = 1
                break  # Stop the script on mismatch (this will also catch mismatch-during-reorg)

            # Store this block's hash to check against the next block's parentHash
            previous_block_hash = block.hash

            # 6. If we detected a reorg, stop the scan if requested
            if reorg_detected and stop_at_reorg:
                logger.info("Stopping scan due to reorg detection (receipts were checked).")
                break
            else:
                reorg_detected = False

        except Exception as e:
            # Log any error during get_block or get_receipts and continue
            logger.error(f"❌ Error processing block {current_block_number}: {e}.")
            await asyncio.sleep(1)

    if not shutdown.is_set():
        if status == 0:
            logger.info(f"✅ Successfully verified all receipts from {start_block_number} to {current_block_number}.")
        else:
            logger.info("Scan stopped due to " + "reorg detection" if reorg_detected else "root mismatch" + ".")

    return status


async def main():
    """Main function to scan block receipts via HTTP."""

    # Parse command-line arguments
    parser = argparse.ArgumentParser(
        description="Connects to an Ethereum node via HTTP and verifies receipts root for a block range."
    )
    parser.add_argument(
        "--start_block",
        type=int,
        default=None,
        help="The starting block number to scan (inclusive).",
    )
    parser.add_argument(
        "--end_block",
        type=int,
        default=None,
        help="The ending block number to scan (inclusive).",
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
    parser.add_argument(
        "--stop_at_reorg",
        action="store_true",
        default=False,
        help="Flag indicating that execution must be stopped at first re-org encountered",
    )
    args = parser.parse_args()

    # Mode validation
    is_range_mode = args.start_block is not None and args.end_block is not None
    is_latest_mode = args.start_block is None and args.end_block is None

    if not is_range_mode and not is_latest_mode:
        logger.error("Invalid arguments: You must specify --start_block AND --end_block, or neither.")
        sys.exit(1)

    if is_range_mode and args.end_block < args.start_block:
        logger.error(f"End block {args.end_block} must be greater than or equal to start block {args.start_block}")
        sys.exit(1)

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

    status = 0
    try:
        if is_range_mode:
            status = await scan_block_range(client, args.start_block, args.end_block, shutdown_event)
        elif is_latest_mode:
            status = await scan_latest_blocks(client, args.sleep_time, args.stop_at_reorg, shutdown_event)
    except Exception as e:
        logger.error(f"❌ Unexpected application error: {e}")
        status = 1
    finally:
        sys.exit(status)


if __name__ == "__main__":
    """ 
    Usage (Range Scan):
    python scan_block_receipts.py --start_block <num> --end_block <num> [--node_url] [--ca_file]

    Usage (Latest Block Scan): 
    python scan_block_receipts.py [--node_url] [--ca_file] [--sleep_time] [--stop_at_reorg]

    or (if part of a package):
    python -m your_package_name.scan_block_receipts ...
    """
    asyncio.run(main())
