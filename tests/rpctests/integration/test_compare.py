""" Tests for comparison utilities """

import unittest

from rpctests.integration.compare import should_ignore_diff


class TestShouldIgnoreDiff(unittest.TestCase):
    """Unit tests for the should_ignore_diff function."""

    # Identity

    def test_identity_check_identical_responses(self):
        response = {"result": 42, "id": 1}
        self.assertTrue(should_ignore_diff(response, response, False, False))

    # Case 1: Specific Expected Error

    def test_case1_false_expected_specific_actual_no_error(self):
        expected = {"error": "Specific Error"}
        actual = {"result": "A value"}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    def test_case1_false_expected_specific_actual_null_error(self):
        """Covers: C1 True branch (actual error is null)."""
        expected = {"error": "Specific Error"}
        actual = {"error": None}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    def test_case1_false_expected_specific_actual_different_error(self):
        """Covers: C1 True branch (actual error is different)."""
        expected = {"error": "Specific Error"}
        actual = {"error": "Different Error"}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    def test_case1_fallthrough_expected_specific_actual_matching_error(self):
        """Covers: C1 False branch (actual error matches expected), falls to final False."""
        expected = {"error": "Specific Error"}
        actual = {"error": "Specific Error", "id": 1}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    # Case 2: Expected Result (Not Null) and Actual Error

    def test_case2_false_expected_not_null_result_actual_error(self):
        """Covers: C2 True branch, inner condition False (expected result not null), returns False."""
        expected = {"result": 100}
        actual = {"error": "Some error"}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    def test_case2_true_expected_null_result_actual_error_file_ref(self):
        """Covers: C2 True branch, inner condition True, returns True."""
        expected = {"result": None}
        actual = {"error": "Some error"}
        # This is a specific failure case where a null result from a file allows an error.
        self.assertTrue(should_ignore_diff(actual, expected, True, False))

    def test_case2_false_expected_null_result_actual_error_not_file_ref(self):
        """Covers: C2 True branch, inner condition False (not from file), returns False."""
        expected = {"result": None}
        actual = {"error": "Some error"}
        # Fails the inner 'if' check
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    # Case 3: Expected Null Result and Reference From File

    def test_case3_true_expected_null_result_actual_has_result_file_ref(self):
        """Covers: C3 True branch."""
        expected = {"result": None}
        actual = {"result": 42, "id": 1}
        self.assertTrue(should_ignore_diff(actual, expected, True, False))

    def test_case3_fallthrough_expected_null_result_actual_has_result_not_file_ref(self):
        """Covers: C3 False branch (not from file), falls to final False."""
        expected = {"result": None}
        actual = {"result": 42}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    # Case 4: Expected Null Error

    def test_case4_true_expected_null_error_actual_specific_error(self):
        """Covers: C4 True branch."""
        expected = {"error": None}
        actual = {"error": "A specific error message"}
        self.assertTrue(should_ignore_diff(actual, expected, False, False))

    def test_case4_fallthrough_expected_null_error_actual_no_error(self):
        """Covers: C4 False branch (not has_actual_error), falls to final False."""
        expected = {"error": None}
        actual = {"result": 42}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    # Case 5: Empty Expected Response

    def test_case5_true_expected_empty_actual_result(self):
        """Covers: C5 True branch (actual has 'result')."""
        expected = {"id": 1}
        actual = {"result": 42, "id": 1}
        self.assertTrue(should_ignore_diff(actual, expected, False, False))

    def test_case5_true_expected_empty_actual_error(self):
        """Covers: C5 True branch (actual has 'error')."""
        expected = {"id": 1}
        actual = {"error": "Any error", "id": 1}
        self.assertTrue(should_ignore_diff(actual, expected, False, False))

    def test_case5_true_expected_empty_actual_empty(self):
        """Covers: C5 True branch (actual is also empty but not identical)."""
        expected = {"id": 1}
        actual = {"jsonrpc": "2.0", "id": 1}
        self.assertTrue(should_ignore_diff(actual, expected, False, False))

    # Final Default Fallthrough

    def test_final_fallthrough_different_results(self):
        """Covers: Final return False branch for differing results."""
        expected = {"result": 100}
        actual = {"result": 42}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    def test_final_fallthrough_expected_result_actual_no_result(self):
        """Covers: Final return False branch for missing actual result."""
        expected = {"result": 100}
        actual = {"id": 1}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))

    def test_final_fallthrough_expected_null_result_actual_no_result(self):
        """Covers: Final return False branch when expected null result, actual no result (not C3)."""
        expected = {"result": None}
        actual = {"id": 1}
        self.assertFalse(should_ignore_diff(actual, expected, False, False))
