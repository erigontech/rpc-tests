""" HTTP(S) utilities """

import logging
import web3
import web3.utils

from . import jsonrpc

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


class Client(jsonrpc.Client):
    """ HTTP(S) client """
    def __init__(self, node_url: str, server_ca_file: str | None = None):
        """ Initialize the HTTP(S) client.
            node_url (str): HTTP(S) URL of the Ethereum node
            server_ca_file (str | None): *public* certificate file of the HTTPS server, if present
        """
        # Create HTTP(S) provider
        parsed_url = Client.parse_url(node_url, ['http', 'https'])
        request_kwargs = {"ssl": Client.ssl_context(server_ca_file)} if parsed_url.scheme == 'https' else None
        provider = web3.AsyncHTTPProvider(node_url, request_kwargs)

        jsonrpc.Client.__init__(self, node_url, provider)
