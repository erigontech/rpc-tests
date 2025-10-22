#!/usr/bin/env python3
"""
This script connects to an Ethereum node via WebSocket, sends an eth_subscribe JSON-RPC request,
and listens for notifications until interrupted with Ctrl+C.
"""

import argparse
import asyncio
import eth_typing
import logging
import signal
import sys
import web3
import web3.utils

from .common import websocket

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


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
        print(f"New block header: {header}\n")

    async def log_handler(handler_context: web3.utils.subscriptions.LogsSubscriptionContext) -> None:
        log_receipt = handler_context.result
        print(f"Log receipt: {log_receipt}\n")

    status = 0
    try:
        # Connect to Ethereum node
        await client.connect()
        logger.info(f"✅ Successfully connected to Ethereum node at {client.node_url}")

        # Subscribe to event notifications for new headers and USDT logs
        subscriptions = await client.subscribe(
            [
                web3.utils.subscriptions.NewHeadsSubscription(
                    label="new-heads-mainnet",
                    handler=new_heads_handler),
                web3.utils.subscriptions.LogsSubscription(
                    label="USDT transfers",
                    address=client.w3.to_checksum_address("0xdac17f958d2ee523a2206206994597c13d831ec7"),
                    topics=[eth_typing.HexStr("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")],
                    handler=log_handler),
            ]
        )
        logger.info(f"✅ Handle subscriptions started: {len(subscriptions)}")

        # Listen for incoming subscription events
        await client.handle_subscriptions()
        logger.info(f"✅ Handle subscriptions terminated")
    except KeyboardInterrupt:
        logger.info("Interrupted by user")
    except Exception as e:
        logger.error(f"❌ Unexpected error: {e}")
        status = e
    finally:
        # Cleanup
        await client.disconnect()
        sys.exit(status)


if __name__ == "__main__":
    """ Usage: python subscriptions.py """
    asyncio.run(main())
