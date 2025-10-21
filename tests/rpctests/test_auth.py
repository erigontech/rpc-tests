""" Tests for JSON Web Token (JWT) utilities """

import unittest
from unittest.mock import patch, mock_open, MagicMock
from datetime import datetime, timezone

from rpctests.common.auth import encode_jwt_token, read_jwt_key


class TestAuth(unittest.TestCase):
    # Define a fixed time for 'datetime.now(pytz.utc)' to return
    MOCK_NOW_UTC = datetime(2025, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
    MOCK_KEY_HEX = "aabbccddeeff00112233445566778899"
    MOCK_SECRET_BYTES = bytes.fromhex(MOCK_KEY_HEX)
    MOCK_TOKEN = b'mocked.jwt.token'

    @patch('builtins.open', new_callable=mock_open, read_data=f"0x{MOCK_KEY_HEX}")
    def test_read_jwt_key_success(self, mock_file):
        """Test successful reading and parsing of the JWT key file."""
        key = read_jwt_key("/path/to/keyfile")
        self.assertEqual(key, self.MOCK_KEY_HEX)
        mock_file.assert_called_once_with("/path/to/keyfile", encoding='utf8')

    @patch('builtins.open', side_effect=FileNotFoundError)
    def test_read_jwt_key_file_not_found(self, mock_file):
        """Test case where the JWT key file is not found."""
        key = read_jwt_key("/path/to/nonexistent_file")
        self.assertIsNone(key)
        mock_file.assert_called_once_with("/path/to/nonexistent_file", encoding='utf8')

    @patch('rpctests.common.auth.read_jwt_key')
    @patch('rpctests.common.auth.datetime', MagicMock(now=MagicMock(return_value=MOCK_NOW_UTC)))
    @patch('rpctests.common.auth.jwt.encode', return_value=MOCK_TOKEN)
    def test_encode_jwt_token_success(self, mock_jwt_encode, mock_read_key):
        """Test successful token encoding with a valid secret."""
        # Set up the mocked secret key
        mock_read_key.return_value = self.MOCK_KEY_HEX

        token = encode_jwt_token("some_file")
        self.assertEqual(token, self.MOCK_TOKEN)

        # Check that the jwt.encode function was called with
        expected_payload = {
            'iat': self.MOCK_NOW_UTC  # The mocked datetime value
        }

        mock_jwt_encode.assert_called_once_with(
            expected_payload,
            self.MOCK_SECRET_BYTES,
            algorithm='HS256'
        )
        mock_read_key.assert_called_once_with("some_file")

    @patch('rpctests.common.auth.read_jwt_key', return_value=None)
    def test_encode_jwt_token_no_secret(self, mock_read_key):
        """Test case where no secret key is found."""
        token = encode_jwt_token("missing_file")
        self.assertIsNone(token)
        mock_read_key.assert_called_once_with("missing_file")


if __name__ == '__main__':
    unittest.main()
