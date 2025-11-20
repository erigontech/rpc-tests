""" JSON-RPC utilities """

import ssl
import urllib.parse
import web3
import web3.providers
import web3.utils

from typing import Any, List


def is_valid_jsonrpc_object(obj: Any, allow_missing_result_and_error: bool = False) -> bool:
    """
    Performs a basic sanity check on a Python object to determine if it conforms to the core JSON-RPC 2.0 object
    structure (Request, Response, or Error).
    A valid JSON-RPC object MUST be a dictionary (or a list of dictionaries) and MUST contain a 'jsonrpc' member with a
    string value of "2.0".
    We support allow_missing_result_and_error flag to consider an object without both "result" and "error" still valid
    because our JSON-RPC response files use such response as "don't care if it's positive or negative response"
    Args:
        obj: The object to check (e.g., from json.loads()).
        allow_missing_result_and_error: flag to consider an object without both "result" and "error" still valid
    Return:
        True if the object is a conformant JSON-RPC object, False otherwise.
    """
    # 1. Handle Batch Request/Response (List of objects)
    if isinstance(obj, list):
        if not obj:
            # An empty list is not a valid batch request
            return False
        # Every item in the list must be a valid single JSON-RPC object
        return all(is_valid_jsonrpc_object(item) for item in obj)

    # 2. Handle Single Request/Response (Dictionary)
    if not isinstance(obj, dict):
        return False

    # 3. All JSON-RPC objects MUST have a "jsonrpc" member with value "2.0"
    if obj.get("jsonrpc") != "2.0":
        return False

    # 4. Check for the mandatory core members (id and either method/error/result)

    # All objects MUST contain an "id" member (it may be None for Notifications)
    if "id" not in obj:
        return False

    # Request/Notification object MUST contain a "method" member.
    if "method" in obj:
        # A Request/Notification is valid if it has "method" and "jsonrpc":"2.0"
        return True

    # Response/Error object MUST contain either a "result" or an "error" member.
    if "result" in obj or "error" in obj or allow_missing_result_and_error:
        # A Response/Error is valid if it has "result" OR "error" and "jsonrpc":"2.0"
        return True

    # If it has "jsonrpc":"2.0" and "id" but neither "method", "result", nor "error", it's invalid
    return False


class Client:
    """ JSON-RPC client """
    def __init__(self, node_url: str, provider: web3.providers.AsyncBaseProvider):
        """ Initialize the JSON-RPC client.
            node_url (str): endpoint URL of the Ethereum node
            provider (web3.providers.AsyncBaseProvider): web3 transport provider
        """
        self.node_url = node_url
        self.w3 = web3.AsyncWeb3(provider)

    @staticmethod
    def parse_url(url: str, allowed_schemes: List[str]) -> urllib.parse.ParseResult:
        """ Parse URL and check validity with reference to the allowed schemes"""
        parsed_url = urllib.parse.urlparse(url)
        if parsed_url.scheme not in allowed_schemes:
            raise ValueError(f"Invalid URL scheme: {parsed_url.scheme}. Must be one of {allowed_schemes}.")
        return parsed_url

    @staticmethod
    def ssl_context(server_ca_file: str):
        """ Create SSL context based on specified CA file"""
        if server_ca_file is None:
            raise ValueError(f"You must specify a non-empty server CA file.")
        ssl_context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        ssl_context.load_verify_locations(cafile=server_ca_file)
        ssl_context.check_hostname = False
        return ssl_context

    async def subscribe(self, subscriptions):
        """ """
        return await self.w3.subscription_manager.subscribe(subscriptions)

    async def unsubscribe(self):
        """ """
        return await self.w3.subscription_manager.unsubscribe(self.w3.subscription_manager.subscriptions)

    async def handle_subscriptions(self, run_forever: bool = False):
        """ """
        return await self.w3.subscription_manager.handle_subscriptions(run_forever)
