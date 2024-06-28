#!/usr/bin/python3
""" JSON RPC API scans DB """

import getopt
import os
import shutil
import sys

from rpc.replay.jsonrpc import JsonRpc
from rpc.replay.scan_db import ScanDb

SILK_TARGET="http://127.0.0.1:51515"
RPCDAEMON_TARGET="http://localhost:8545"
OUTPUT_DIR="./output/"



class Config:
    # pylint: disable=too-many-instance-attributes
    """ This class manage User options params """

    def __init__(self):
        """ init the configuration params """
        self.continue_test = False
        self.start_block = 0
        self.start_tx = 0
        self.max_failed_request = 0
        self.request_json_func = JsonRpc.create_trace_transaction

    @staticmethod
    def usage(argv):
        """ Print script usage """
        print("Usage: " + argv[0] + " [options]")
        print("")
        print("Launch a compare test on Silkrpc and RPCDaemon")
        print("")
        print("-h,--help:          print this help")
        print("-s,--start:         block::txn_number")
        print("-n,--number:        number of failed txs")
        print("-c,--continue:      continue scanning. Doesn't stop at first diff")
        print("-m,--method:        0: trace_transaction, 1: debug_traceTransaction")

    def select_user_options(self, argv):
        """ process user command """
        try:
            opts, _ = getopt.getopt(argv[1:], "s:chn:m:", ['help', 'start=', 'continue',
                                                           'number=', 'method='])
            for option, optarg in opts:
                if option in ("-h", "--help"):
                    self.usage(argv)
                    return 1
                if option in ("-s", "--start"):
                    startpoint = optarg.split(':')
                    if len(startpoint) != 2:
                        print ("bad start field definition: block:tx")
                        self.usage(argv)
                        return 1
                    self.start_block = optarg.split(':')[0]
                    self.start_tx = optarg.split(':')[1]
                elif option in ("-c", "--continue"):
                    self.continue_test = True
                elif option in ("-n", "--number"):
                    self.max_failed_request = int(optarg)
                    self.continue_test = True
                elif option in ("-m", "--method"):
                    if int(optarg) == 0:
                        self.request_json_func = JsonRpc.create_trace_transaction
                    elif int(optarg) == 1:
                        self.request_json_func = JsonRpc.create_debug_trace_transaction
                    else:
                        print ("wrong method id")
                        self.usage(argv)
                        return 1
        except getopt.GetoptError as err:
            # print help information and exit:
            print(err)
            self.usage(argv)
            return 1
        return 0

#
# main
#
def main(argv) -> int:
    """ scans DB and found trace_replyTransactions() response that differs from rpcdaemon and silk
    """
    config = Config()
    if config.select_user_options(argv) == 1:
        return 1

    print ("Starting scans from: ", config.start_block, " tx-index: ", config.start_tx)
    if os.path.exists(OUTPUT_DIR) == 1:
        shutil.rmtree(OUTPUT_DIR)
    if os.path.exists(OUTPUT_DIR) == 0:
        os.mkdir(OUTPUT_DIR)

    scan_db = ScanDb(config.request_json_func, config.continue_test, config.max_failed_request)
    return scan_db.scan_all_tx(config.start_block, config.start_tx)

#
# module as main
#
if __name__ == "__main__":
    sys.exit(main(sys.argv))
