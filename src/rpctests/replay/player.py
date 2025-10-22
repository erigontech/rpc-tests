""" Player for JSON RPC API replay """

import pathlib
import requests
from time import sleep
from typing import Optional
from typing import Self

from ..common import auth
from .config import Options


class Player:
    """ Player for JSON-RPC requests """

    def __init__(self: Self, options: Options):
        """ """
        self.options = options
        self.headers = {'content-type': 'application/json'}
        if encoded_jwt_secret := auth.encode_jwt_token(self.options.jwt_secret_file):
            self.headers['authorization'] = 'Bearer ' + str(encoded_jwt_secret)

    def replay(self: Self, method: str, interval_secs: int = 12):
        """ Replay all requests matching the specified method """
        method_index = 1
        while self.replay_request(method, method_index):
            method_index = method_index + 1
            sleep(interval_secs)

    def replay_request(self: Self, method: str, method_index: int = 1) -> Optional[str]:
        """ Replay first request matching the specified method """
        response = None
        if jsonrpc_request := self.__find_jsonrpc_request(method, method_index):
            try:
                print(f"Request {method} found [{method_index}]")
                if self.options.verbose:
                    print(f"{jsonrpc_request}")
                if self.options.pretend:
                    return response
                response = requests.post(self.options.url, data=jsonrpc_request, headers=self.headers)
                print(f"Response got: {response.json()}")
            except requests.ConnectionError as ce:
                print(f"Post failed: {ce}")
        return response

    def __find_jsonrpc_request(self: Self, method: str, method_index: int) -> Optional[str]:
        """ Find the method_index-th occurrence of the specified method, if any """
        method_count = 0
        interface_log_file_path = pathlib.Path(self.options.interface_log_file_path)
        interface_log_dir_path = interface_log_file_path.parent
        if self.options.verbose:
            print(f"interface_log_dir_path: {interface_log_dir_path}")
        interface_log_file_paths = [f for f in interface_log_dir_path.glob('*engine_rpc_api*log')]
        interface_log_file_paths.sort()
        for log_file_path in interface_log_file_paths:
            if self.options.verbose:
                print(f"log_file_path: {log_file_path}")
            with open(log_file_path) as interface_log_file:
                for log_file_line in interface_log_file:
                    if log_file_line.find("REQ -> ") != -1:
                        method_position = log_file_line.find("method")
                        if self.options.verbose:
                            print(f"Method {log_file_line[method_position:method_position+40]} found {log_file_path}")
                    if log_file_line.find("REQ -> ") != -1 and log_file_line.find(method) != -1:
                        method_count = method_count + 1
                        if method_count == method_index:
                            return log_file_line.split("REQ -> ", 1)[1]
        return None
