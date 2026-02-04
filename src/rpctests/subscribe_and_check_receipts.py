#!/usr/bin/env python3
"""
This script connects to an Ethereum node via WebSocket, sends an eth_subscribe JSON-RPC request,
and listens for new block notifications until interrupted with Ctrl+C.
When a new block notification is received, it queries the block receipts and verifies the receipts root.
"""

import argparse
import asyncio
import enum
import logging
import signal
import sys
import web3
import web3.exceptions
import web3.types
import web3.utils

from .common import websocket
from .eth.trie.receipt import compute_receipts_root


# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s %(levelname).4s - %(message)s')
logger = logging.getLogger(__name__)


class FetchingMethod(enum.Enum):
    ETH_GET_BLOCK_RECEIPTS = 1
    ETH_GET_TRANSACTION_RECEIPT_SEQUENCE = 2
    ETH_GET_TRANSACTION_RECEIPT_BATCH = 3


async def fetch_receipts(
    client: websocket.Client,
    block: web3.types.BlockData,
    fetching_method: FetchingMethod
) -> web3.types.BlockReceipts:
    """Fetch the execution receipts for the passed block using the specified fetching method"""
    receipts: web3.types.BlockReceipts = []
    match fetching_method:
        case FetchingMethod.ETH_GET_BLOCK_RECEIPTS:
            receipts = await client.w3.eth.get_block_receipts(block.hash)
        case FetchingMethod.ETH_GET_TRANSACTION_RECEIPT_SEQUENCE:
            for txn_hash in block.transactions:
                tx_receipt = await client.w3.eth.get_transaction_receipt(txn_hash)
                receipts.append(tx_receipt)
        case FetchingMethod.ETH_GET_TRANSACTION_RECEIPT_BATCH:
            async with client.w3.batch_requests() as batch:
                for txn_hash in block.transactions:
                    batch.add(client.w3.eth.get_transaction_receipt(txn_hash))
                get_receipt_results = await batch.async_execute()
                for receipt in get_receipt_results:
                    receipts.append(receipt)
    return receipts


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
    client = websocket.Client(args.websocket_url, args.ca_file)

    # Status tracking: 0 = success, 1 = receipt verification failed
    status = {"code": 0}
    shutdown_event = asyncio.Event()

    # Setup signal handler for graceful shutdown
    async def signal_handler():
        print("")
        logger.info("🏁 Received interrupt signal (Ctrl+C)")
        await client.unsubscribe()

    loop = asyncio.get_running_loop()
    for sig in [signal.SIGINT, signal.SIGTERM]:
        loop.add_signal_handler(sig, lambda: asyncio.create_task(signal_handler()))

    # Prepare the subscription event handlers
    async def new_heads_handler(handler_context: web3.utils.subscriptions.NewHeadsSubscriptionContext) -> None:
        header = handler_context.result
        block_number = header.number
        logger.info(f"✅ New head: {block_number} {header.hash.hex()}")

        try:
            # Get the block receipts and verify the receipts root
            header_receipts_root = header.receiptsRoot

            receipts = await fetch_receipts(client, header, FetchingMethod.ETH_GET_BLOCK_RECEIPTS)
            computed_receipts_root = compute_receipts_root(receipts)
            if computed_receipts_root == header_receipts_root:
                logger.info(f"✅ Block {block_number}: receipts root verified ({len(receipts)} receipts)")
            else:
                logger.critical(f"🚨 Block {block_number}: receipt root mismatch detected 🚨")
                logger.critical(f"Expected header root: {header_receipts_root.hex()}")
                logger.critical(f"Actual computed root: {computed_receipts_root.hex()}")
                # Set failure status and trigger shutdown
                status["code"] = 1
                shutdown_event.set()
                await client.unsubscribe()

        except web3.exceptions.BlockNotFound as bnfe:
            logger.error(f"❌ Block {block_number}: block not found - {bnfe}")
            await asyncio.sleep(1)
        except web3.exceptions.TransactionNotFound as tnfe:
            logger.error(f"❌ Block {block_number}: transaction not found - {tnfe}")
            await asyncio.sleep(1)
        except web3.exceptions.TimeExhausted as te:
            logger.error(f"❌ Block {block_number}: request timeout - {te}")
            await asyncio.sleep(1)
        except (ConnectionError, ConnectionResetError, BrokenPipeError) as ce:
            logger.error(f"❌ Block {block_number}: connection error - {ce}")
            await asyncio.sleep(1)
        except Exception as e:
            # Catch any other unexpected errors
            logger.error(f"❌ Block {block_number}: unexpected error - {type(e).__name__}: {e}")
            await asyncio.sleep(1)

    try:
        # Connect to Ethereum node
        await client.connect()
        logger.info(f"✅ Successfully connected to Ethereum node at {client.node_url}")

        # Subscribe to event notifications for new headers
        subscriptions = await client.subscribe(
            web3.utils.subscriptions.NewHeadsSubscription(
                label="new-heads",
                handler=new_heads_handler),
        )
        logger.info(f"✅ Handle subscriptions started: {len(subscriptions)}")

        # Listen for incoming subscription and shutdown events
        try:
            handle_task = asyncio.create_task(client.handle_subscriptions(), name="handle_subscriptions")
            shutdown_task = asyncio.create_task(shutdown_event.wait(), name="shutdown_event")

            # Wait for either the handler to complete or shutdown signal
            done, pending = await asyncio.wait([handle_task, shutdown_task], return_when=asyncio.FIRST_COMPLETED)
            for task in done:
                logger.info(f"✅ Task: {task.get_name()} done")
            for task in pending:
                task.cancel()
                try:
                    await task
                except asyncio.CancelledError:
                    pass
                logger.info(f"✅ Task: {task.get_name()} cancelled")
        except asyncio.CancelledError:
            logger.info("Subscription handling cancelled")

        logger.info(f"✅ Handle subscriptions terminated")
    except KeyboardInterrupt:
        logger.info("Interrupted by user")
    except Exception as e:
        logger.error(f"❌ Unexpected error: {e}")
        status["code"] = 1
    finally:
        # Cleanup
        await client.disconnect()
        sys.exit(status["code"])


if __name__ == "__main__":
    """ Usage: python subscriptions.py """
    asyncio.run(main())

