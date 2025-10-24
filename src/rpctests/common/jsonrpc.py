""" JSON-RPC utilities """

from typing import Any


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
