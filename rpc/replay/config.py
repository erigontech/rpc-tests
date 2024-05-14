""" Configuration options for JSON RPC API replay """

# import getopt
from typing import List
from typing import Self


class OptionError(BaseException):
    """ """


class Options:
    """ Replay configuration options """

    @staticmethod
    def usage(argv: List[str]):
        """ Print usage information """
        print("Usage: " + argv[0] + ":")

    def __init__(self: Self, argv: List[str]):
        """ Parse from command-line arguments """
        self.interface_log_file = ""
        self.interface_log_file_path = "/Users/tullio/Library/SilkwormSepolia/logs/engine_rpc_api.log"
        self.url = "http://localhost:8551"
        self.jwt_secret_file = "/Users/tullio/Workspace/prysm/jwt.hex"
