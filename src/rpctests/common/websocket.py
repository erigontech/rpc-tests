""" WebSocket utilities """

import web3
import web3.utils

from . import jsonrpc


class Client(jsonrpc.Client):
    """ WebSocket client """
    def __init__(self, node_url: str, server_ca_file: str | None = None):
        """ Initialize the WebSocket client.
            node_url (str): WebSocket URL of the Ethereum node
            server_ca_file (str | None): *public* certificate file of the WSS server, if present
        """
        # Create WebSocket provider
        parsed_url = Client.parse_url(node_url, ['ws', 'wss'])
        websocket_kwargs = {"max_size": 10 * 1024 * 1024}  # 10MB
        if parsed_url.scheme == 'wss':
            websocket_kwargs["ssl"] = Client.ssl_context(server_ca_file)
        provider = web3.WebSocketProvider(node_url, websocket_kwargs, max_connection_retries=1)

        jsonrpc.Client.__init__(self, node_url, provider)

    async def connect(self):
        """Set up the WebSocket connection to the Ethereum node."""
        try:
            await self.w3.provider.connect()
            if not await self.w3.is_connected():
                raise ConnectionError("Failed to connect to Ethereum node")
        except Exception as e:
            raise ConnectionError(f"Connection failed: {e}")

    async def disconnect(self):
        """Tear down the WebSocket connection to the Ethereum node."""
        try:
            await self.w3.provider.disconnect()
        except Exception as e:
            raise ConnectionError(f"Disconnection failed: {e}")
