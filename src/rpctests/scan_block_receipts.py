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
import hexbytes
import logging
import signal
import rlp
import sys
import trie
import web3.types

from .common import http

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


def compute_receipts_root(receipts: web3.types.BlockReceipts) -> bytes:
    """Compute the modified Merkle-Patricia Trie (MPT) root hash from a list of receipts."""
    # Create a new empty hexary MPT
    receipt_trie = trie.HexaryTrie(db={})

    # Iterate over the receipts and add them to the trie
    for i, receipt in enumerate(receipts):
        key = rlp.encode(i)  # Key is the RLP-encoded index

        # Re-construct the RLP-serializable receipt list (the structure depends on Byzantium vs. pre-Byzantium fork)

        # Convert logs from AttributeDict to the required list format
        logs_list = []
        for log in receipt['logs']:
            logs_list.append([
                hexbytes.HexBytes(log['address']),  # address
                [hexbytes.HexBytes(topic) for topic in log['topics']],  # list of topics
                hexbytes.HexBytes(log['data'])  # data
            ])

        # Create the RLP-serializable list
        if 'status' in receipt:
            # Post-Byzantium: [status, cumulativeGasUsed, logsBloom, logs]
            value_list = [
                receipt['status'],
                receipt['cumulativeGasUsed'],
                receipt['logsBloom'],
                logs_list
            ]
        else:
            # Pre-Byzantium: [stateRoot, cumulativeGasUsed, logsBloom, logs]
            # The 'root' field (state root) is expected in pre-Byzantium receipts
            if 'root' not in receipt:
                block_number = receipt['blockNumber']
                logger.error(
                    f"Cannot compute root for pre-Byzantium block {block_number} without 'root' field in receipt.")
                raise ValueError(f"Receipt for block {block_number} has no 'status' or 'root' field.")

            value_list = [
                receipt['root'],  # stateRoot
                receipt['cumulativeGasUsed'],
                receipt['logsBloom'],
                logs_list
            ]

        # RLP-encode the value and insert it into the trie
        value = rlp.encode(value_list)
        receipt_trie[key] = value

    # The trie root_hash is the final receiptsRoot
    return receipt_trie.root_hash


async def scan_block_range(client, start_block, end_block, shutdown_event):
    """
    Scans a fixed range of blocks and verifies their receipts roots.
    Returns status: 0 on success, 1 on failure.
    """
    status = 0
    logger.info(f"Scanning block receipts from {start_block} to {end_block}...")

    for block_number in range(start_block, end_block + 1):
        if shutdown_event.is_set():
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

            # 4. Compare the roots
            if computed_receipts_root != header_receipts_root:
                logger.critical(f"🚨 Receipt roo mismatch detected at block {block_number} 🚨")
                logger.critical(f"- expected header root: {header_receipts_root.hex()}")
                logger.critical(f"- actual computed root: {computed_receipts_root.hex()}")
                status = 1
                break  # Stop the script as requested
            else:
                logger.info(f"✅ Block {block_number}: Receipts root verified ({len(receipts)} receipts).")

        except Exception as e:
            # Log any error during get_block or get_receipts and continue
            logger.error(f"❌ Error processing block {block_number}: {e}. Skipping this block.")

    if not shutdown_event.is_set() and status == 0:
        logger.info(f"✅ Successfully scanned and verified all blocks from {start_block} to {end_block}.")
    elif status == 1:
        logger.warning("Scan stopped due to receipts root mismatch.")

    return status


async def scan_latest_blocks(client, shutdown_event):
    """
    Scans the latest blocks, checks for reorgs, and verifies receipts roots.
    Runs until shutdown. Returns status: 0 on success, 1 on failure.
    """
    status = 0
    logger.info("Scanning latest blocks... Press Ctrl+C to stop.")

    previous_block_hash = None
    current_block_number = -1

    while not shutdown_event.is_set():
        try:
            # Get the latest block header
            block = await client.w3.eth.get_block("latest", full_transactions=False)

            if block.number == current_block_number:
                await asyncio.sleep(0.5)  # Wait for a new block
                continue

            current_block_number = block.number
            reorg_detected = False  # Flag for this block

            # Check for chain reorg (only in this mode)
            if previous_block_hash is not None and block.parentHash != previous_block_hash:
                logger.critical(f"🚨 REORG DETECTED at block {current_block_number} 🚨")
                logger.critical(f"  Block {current_block_number} parentHash: {block.parentHash.hex()}")
                logger.critical(f"  Expected (block {current_block_number - 1} hash): {previous_block_hash.hex()}")
                status = 1
                reorg_detected = True  # Mark reorg, but continue to receipt check
                # Do not break yet

            header_receipts_root = block.receiptsRoot

            # 3. Get the block receipts
            receipts = await client.w3.eth.get_block_receipts(current_block_number)

            # 4. Compute the receipts root
            computed_receipts_root = compute_receipts_root(receipts)

            # 5. Compare the roots
            if computed_receipts_root != header_receipts_root:
                logger.critical(f"🚨 Receipt roo mismatch detected at block {current_block_number} 🚨")
                logger.critical(f"- expected header root: {header_receipts_root.hex()}")
                logger.critical(f"- actual computed root: {computed_receipts_root.hex()}")
                status = 1
                break  # Stop the script on mismatch (this will also catch mismatch-during-reorg)
            else:
                # Log success only if no reorg was detected
                if not reorg_detected:
                    logger.info(f"✅ Block {current_block_number}: Receipts root verified ({len(receipts)} receipts).")
                else:
                    logger.warning(f"⚠️ Block {current_block_number}: Reorg detected, but receipts root IS valid.")

            # Store this block's hash to check against the next block's parentHash
            previous_block_hash = block.hash

            # 6. Now, if we detected a reorg, stop the scan (as receipts check has passed)
            if reorg_detected:
                logger.info("Stopping scan due to reorg detection (receipts were checked).")
                break

        except Exception as e:
            # Log any error during get_block or get_receipts and continue
            logger.error(f"❌ Error processing block {current_block_number}: {e}.")
            await asyncio.sleep(1)  # Avoid spamming on repeated errors

    if status == 1:
        logger.warning("Scan stopped due to receipts root mismatch or reorg detection.")
    else:
        logger.info("Scan terminated by user.")

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
            status = await scan_latest_blocks(client, shutdown_event)

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
    python scan_block_receipts.py [--node_url] [--ca_file]

    or (if part of a package):
    python -m your_package_name.scan_block_receipts ...
    """
    asyncio.run(main())
