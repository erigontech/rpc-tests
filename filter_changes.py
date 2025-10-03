#!/usr/bin/env python3
"""
This script connects to an Ethereum node via WebSocket and queries filter changes/logs
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
        description="Connects to an Ethereum node via WebSocket and create a state change filter."
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

        # Register a new filter with some topic and periodically pull changes
        delay = 2
        f = await client.w3.eth.filter({"topics": ["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3e0"]})
        logger.info(f"✅ State change filter registered")
        while not shutdown_event.is_set():
            changes = await client.w3.eth.get_filter_changes(f.filter_id)
            if changes:
                logger.info(f"Changes: {changes}")
            else:
                logger.info(f"No change received")
            logs = await client.w3.eth.get_filter_logs(f.filter_id)
            if logs:
                logger.info(f"Logs: {logs}")
            else:
                logger.info(f"No log received")
            # Wait for some time before iterating
            await asyncio.sleep(delay)
        await client.w3.eth.uninstall_filter(f.filter_id)

        logger.info(f"✅ State change filter unregistered")
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
    """ Usage: python state_change_filter.py """
    asyncio.run(main())
