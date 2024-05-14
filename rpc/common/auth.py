""" JSON Web Token (JWT) utilities """

from datetime import datetime
from typing import Optional
import jwt
import pytz


def encode_jwt_token(jwt_file: str) -> Optional[bytes]:
    """ """
    if jwt_secret := read_jwt_key(jwt_file):
        jwt_secret_bytes = bytes.fromhex(jwt_secret)
        return jwt.encode({'iat': datetime.now(pytz.utc)}, jwt_secret_bytes, algorithm='HS256')
    return None


def read_jwt_key(jwt_file: str) -> Optional[str]:
    """ Parse JWT secret from JWT key file """
    try:
        with open(jwt_file, encoding='utf8') as file:
            contents = file.readline()
            secret = contents[2:]
    except FileNotFoundError:
        secret = None
    return secret
