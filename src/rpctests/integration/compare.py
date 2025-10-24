""" Comparison utilities """

from typing import Any


def should_ignore_diff(actual_response, expected_response: Any, expected_from_file, do_not_compare_error: bool) -> bool:
    """ Determines if a difference between actual and expected responses should be ignored. """
    # If responses are identical, no need to ignore anything as there's no diff
    if actual_response == expected_response:
        return True

    has_actual_error = "error" in actual_response
    has_expected_error = "error" in expected_response
    has_actual_result = "result" in actual_response
    has_expected_result = "result" in expected_response

    actual_error_is_null = has_actual_error and actual_response["error"] is None
    expected_error_is_null = has_expected_error and expected_response["error"] is None
    # actual_result_is_null = has_actual_result and actual_response["result"] is None
    expected_result_is_null = has_expected_result and expected_response["result"] is None

    # Case 1: Expected response has a specific error, and actual response has a different error or no error.
    # This implies a significant difference that should not be ignored.
    if has_expected_error and not expected_error_is_null and (not has_actual_error or actual_error_is_null or actual_response["error"] != expected_response["error"]):
        return False

    # Case 2: Expected response has a result, but actual response has an error.
    # This is generally a failure unless the expected result is null and the reference is from a file.
    if has_expected_result and has_actual_error:
        if not expected_result_is_null:
            return False
        if expected_result_is_null and expected_from_file:
            return True

    # Case 3: Both have 'result', expected 'result' is null, and we're using a reference response file.
    if has_actual_result and expected_result_is_null and expected_from_file:
        return True

    # Case 4: Both have 'error', and expected 'error' is null ().
    # This means the test expects *an* error, but the specific message doesn't matter.
    if has_actual_error and has_expected_error and (expected_error_is_null or do_not_compare_error):
        return True

    # Case 5: Expected has neither 'error' nor 'result' (empty JSON or only 'jsonrpc'/'id').
    # This means the test is very lenient and any valid JSON RPC response is fine.
    if not has_expected_error and not has_expected_result:
        return True

    return False
