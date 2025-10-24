""" WebSocket utilities """

import logging
import ssl
import urllib.parse
import web3
import web3.utils

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


class Client:
    """ WebSocket client """
    def __init__(self, node_url: str, server_ca_file: str | None = None):
        """ Initialize the WebSocket subscriber.
            node_url (str): WebSocket URL of the Ethereum node
            server_ca_file (str): *public* certificate file of the WSS server
        """
        self.node_url = node_url
        self.server_ca_file = server_ca_file
        self.w3 = None

    async def connect(self):
        """Establish WebSocket connection to Ethereum node."""
        try:
            # Create WebSocket provider
            parsed_url = urllib.parse.urlparse(self.node_url)
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
            provider = web3.WebSocketProvider(self.node_url, websocket_kwargs, max_connection_retries=1)
            self.w3 = web3.AsyncWeb3(provider)

            # Connect to the provider
            await provider.connect()

            # Test connection
            if not await self.w3.is_connected():
                raise ConnectionError("Failed to connect to Ethereum node")

            latest_block = await self.w3.eth.block_number
            logger.info(f"Connected to Ethereum node at {self.node_url}")
            logger.info(f"Latest block: {latest_block}")
        except Exception as e:
            raise ConnectionError(f"Connection failed: {e}")

    async def subscribe(self, subscriptions):
        """ """
        return await self.w3.subscription_manager.subscribe(subscriptions)

    async def unsubscribe(self):
        """ """
        return await self.w3.subscription_manager.unsubscribe(self.w3.subscription_manager.subscriptions)

    async def handle_subscriptions(self, run_forever: bool = False):
        """ """
        return await self.w3.subscription_manager.handle_subscriptions(run_forever)

    async def disconnect(self):
        """Close the WebSocket connection."""
        if self.w3 and self.w3.provider:
            try:
                await self.w3.provider.disconnect()
                logger.info("WebSocket connection closed")
            except Exception as e:
                raise ConnectionError(f"Error during disconnect: {e}")
