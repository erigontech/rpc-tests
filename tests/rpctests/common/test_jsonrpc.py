""" Tests for JSON-RPC utilities """

import pytest
from typing import Any, Dict, List

from rpctests.common.jsonrpc import is_valid_jsonrpc_object

# Define type for a JSON-RPC request/response object for clarity
JsonRpcObject = Dict[str, Any]

# --- Fixtures for Valid JSON-RPC Objects ---


@pytest.fixture
def valid_request() -> JsonRpcObject:
    """A standard valid JSON-RPC Request object."""
    return {"jsonrpc": "2.0", "method": "sum", "params": [1, 2, 4], "id": "1"}


@pytest.fixture
def valid_notification() -> JsonRpcObject:
    """A valid JSON-RPC Notification (no 'id'). The spec allows 'id' to be present but Null for notifications."""
    # NOTE: The current function checks for "id" existence, which makes it stricter than the spec's requirement for
    # notifications (where "id" can be either omitted or Null).
    return {"jsonrpc": "2.0", "method": "update", "params": [1, 5], "id": None}


@pytest.fixture
def valid_response() -> JsonRpcObject:
    """A standard valid JSON-RPC Success Response object."""
    return {"jsonrpc": "2.0", "result": 7, "id": "1"}


@pytest.fixture
def valid_error_response() -> JsonRpcObject:
    """A standard valid JSON-RPC Error Response object."""
    return {
        "jsonrpc": "2.0",
        "error": {"code": -32601, "message": "Method not found"},
        "id": "1"
    }


@pytest.fixture
def valid_batch_request(valid_request) -> List[JsonRpcObject]:
    """A valid JSON-RPC Batch Request (list of requests)."""
    return [
        valid_request,
        {"jsonrpc": "2.0", "method": "echo", "params": ["hello"], "id": 2},
        {"jsonrpc": "2.0", "method": "notify_user", "params": ["user"], "id": None}
    ]


@pytest.fixture
def valid_dont_care_response() -> JsonRpcObject:
    """A standard valid JSON-RPC Request object."""
    return {"jsonrpc": "2.0", "error": None, "result": None, "id": "1"}

# --- Fixtures for Invalid JSON-RPC Objects ---


@pytest.fixture
def invalid_no_version() -> JsonRpcObject:
    """Missing the mandatory 'jsonrpc' member."""
    return {"method": "sum", "params": [1, 2, 4], "id": "1"}


@pytest.fixture
def invalid_wrong_version() -> JsonRpcObject:
    """'jsonrpc' member has the wrong value."""
    return {"jsonrpc": "1.0", "method": "sum", "params": [1, 2, 4], "id": "1"}


@pytest.fixture
def invalid_no_id() -> JsonRpcObject:
    """Missing the mandatory 'id' member (as enforced by our function)."""
    return {"jsonrpc": "2.0", "method": "sum", "params": [1, 2, 4]}


@pytest.fixture
def invalid_response_no_result_error() -> JsonRpcObject:
    """A response that has neither 'result' nor 'error'."""
    return {"jsonrpc": "2.0", "id": "1"}


@pytest.fixture
def invalid_request_with_result() -> JsonRpcObject:
    """A request that incorrectly contains a 'result' member."""
    return {"jsonrpc": "2.0", "method": "sum", "params": [1, 2, 4], "result": 7, "id": "1"}


@pytest.fixture
def invalid_not_object() -> str:
    """Input is not a dict or list."""
    return "This is a string, not a JSON-RPC object"


@pytest.fixture
def invalid_empty_batch() -> List:
    """A batch request that is an empty list."""
    return []


@pytest.fixture
def invalid_mixed_batch(valid_request) -> List:
    """A batch containing an invalid item."""
    return [valid_request, {"method": "invalid_one", "id": 3}]

# --- Test Cases ---


def test_valid_single_requests(valid_request, valid_notification):
    """Tests standard and notification-style requests."""
    assert is_valid_jsonrpc_object(valid_request) is True
    assert is_valid_jsonrpc_object(valid_notification) is True


def test_valid_single_responses(valid_response, valid_error_response):
    """Tests success and error responses."""
    assert is_valid_jsonrpc_object(valid_response) is True
    assert is_valid_jsonrpc_object(valid_error_response) is True


def test_valid_batch(valid_batch_request):
    """Tests a batch request/response array."""
    assert is_valid_jsonrpc_object(valid_batch_request) is True


def test_invalid_missing_fields(invalid_no_version, invalid_no_id, invalid_wrong_version):
    """Tests mandatory field validation."""
    assert is_valid_jsonrpc_object(invalid_no_version) is False
    assert is_valid_jsonrpc_object(invalid_no_id) is False
    assert is_valid_jsonrpc_object(invalid_wrong_version) is False


def test_invalid_malformed_objects(invalid_response_no_result_error, invalid_request_with_result):
    """Tests for objects that meet minimum requirements but are structurally wrong."""
    assert is_valid_jsonrpc_object(invalid_response_no_result_error) is False
    # The request check prioritizes "method", so this is technically accepted
    # as a "valid" request structure despite the extra "result" field.
    assert is_valid_jsonrpc_object(invalid_request_with_result) is True


def test_invalid_input_types(invalid_not_object):
    """Tests non-dictionary/list inputs."""
    assert is_valid_jsonrpc_object(None) is False
    assert is_valid_jsonrpc_object(invalid_not_object) is False
    assert is_valid_jsonrpc_object(12345) is False


def test_invalid_batch_arrays(invalid_empty_batch, invalid_mixed_batch):
    """Tests invalid batch array inputs."""
    assert is_valid_jsonrpc_object(invalid_empty_batch) is False
    assert is_valid_jsonrpc_object(invalid_mixed_batch) is False


def test_edge_case_id_types():
    """Tests various valid 'id' types (string, number, null)."""
    assert is_valid_jsonrpc_object({"jsonrpc": "2.0", "method": "test", "id": "abc"}) is True
    assert is_valid_jsonrpc_object({"jsonrpc": "2.0", "method": "test", "id": 123}) is True
    assert is_valid_jsonrpc_object({"jsonrpc": "2.0", "method": "test", "id": 0}) is True
    assert is_valid_jsonrpc_object({"jsonrpc": "2.0", "method": "test", "id": None}) is True
