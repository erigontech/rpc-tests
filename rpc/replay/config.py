""" Configuration options for JSON RPC API replay """

import getopt
import os
import pathlib
import platform
from typing import List
from typing import Optional
from typing import Self


class Options:
    """ Replay configuration options """

    SHORT_LIST = 'hi:m:j:p:u:'
    LONG_LIST = ['help', 'index=', 'method=', 'jwt=', 'path=', 'url=']

    @staticmethod
    def usage(argv: List[str]):
        """ Print usage information """
        print('Usage: ' + argv[0])
        print('')
        print('Replay JSON-RPC request(s) extracted from Engine API interface log')
        print('')
        print('-h,--help: print this help')
        print('-i,--index: ordinal index of JSONRPC method to replay, -1 means all [default: 1]')
        print('-m,--method: JSONRPC method to replay, empty means all [default: engine_newPayloadV3]')
        print('-j,--jwt: path of JWT secret file [default: $HOME/prysm/jwt.hex]')
        print('-p,--path: path of Engine API interface log file to extract from [default: logs/engine_rpc_api.log]')
        print('-u,--url: HTTP URL of Engine API endpoint to send request to [default: http://localhost:8551]')

    @staticmethod
    def get_home_folder() -> str:
        """ Get user home folder path """
        if 'XDG_DATA_HOME' in os.environ:
            home_dir = os.environ['XDG_DATA_HOME']
        else:
            system_name = platform.system()
            env = os.environ['APPDATA' if system_name == 'Windows' else 'HOME']
            if env:
                home_dir = env
            else:
                home_dir = pathlib.Path().resolve()  # current path
        return home_dir

    @staticmethod
    def get_default_base_dir_path() -> str:
        """ Compute default path for blockchain data """
        base_dir_path = Options.get_home_folder()
        if platform.system() == 'Darwin':
            base_dir_path += '/Library'
        base_dir_path += '/Silkworm'
        return base_dir_path

    def __init__(self: Self):
        """ Parse from command-line arguments """
        self.interface_log_file_path = Options.get_default_base_dir_path() + '/logs/engine_rpc_api.log'
        self.url = 'http://localhost:8551'
        self.jwt_secret_file = Options.get_home_folder() + '/prysm/jwt.hex'
        self.method = 'engine_newPayloadV3'
        self.method_index = 1

    def parse(self: Self, argv: List[str]) -> Optional[str]:
        try:
            opts, _ = getopt.getopt(argv[1:], Options.SHORT_LIST, Options.LONG_LIST)
            for opt, opt_arg in opts:
                if opt in ('-h', '--help'):
                    return ''
                elif opt in ("-i", "--index"):
                    self.method_index = int(opt_arg)
                elif opt in ('-m', '--method'):
                    self.method = opt_arg
                elif opt in ('-j', '--jwt'):
                    self.jwt_secret_file = opt_arg
                elif opt in ('-p', '--path'):
                    self.interface_log_file_path = opt_arg
                elif opt in ('-u', '--url'):
                    self.url = opt_arg
                else:
                    assert False, "getopt.GetoptError not raised for option: " + opt
        except getopt.GetoptError as err:
            return f'{err.msg}\n'
