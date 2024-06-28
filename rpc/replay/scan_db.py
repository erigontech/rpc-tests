""" ScanDb scans all db and make query """


import json
import os
import sys
import requests
from rpc.replay.jsonrpc import JsonRpc


SILK_TARGET="http://127.0.0.1:51515"
RPCDAEMON_TARGET="http://localhost:8545"
OUTPUT_DIR="./output/"


class ScanDb:
    """ Scan-DB """

    def __init__(self, make_json_command_func, continue_test, max_failed_compare):
        self.make_json_command_func = make_json_command_func
        self.continue_test = continue_test
        self.max_failed_compare = max_failed_compare
        self.headers = {'content-type': 'application/json'}

    def make_request(self, json_rpc_cmd, target):
        """ make request to the target server """
        try:
            #print("Request:",json_rpc_cmd)
            response = requests.post(target, data=json_rpc_cmd, headers=self.headers)
            #print("Response:",response)
            if response.status_code != 200:
                print("Response got: ",response.status_code)
                sys.exit(-1)
        except requests.ConnectionError as connection_error:
            print("Post failed: ",connection_error)
            sys.exit(-1)
        return response.json()

    @staticmethod
    def dump_jsons(silk_file, rpcdaemon_file, silk_response, rpcdaemon_response: str):
        """ dump jsons response """
        if silk_file != "":
            with open(silk_file, 'w', encoding='utf8') as json1_file_ptr:
                json1_file_ptr.write(json.dumps(silk_response, indent=6, sort_keys=True))
        if rpcdaemon_file != "":
            with open(rpcdaemon_file, 'w', encoding='utf8') as json2_file_ptr:
                json2_file_ptr.write(json.dumps(rpcdaemon_response, indent=6, sort_keys=True))

    def compare_trace_replay_transaction(self, block, tx_index, tx_hash: str):
        """ compare response from both servers """
        filename = "bn_" + str(block) + "_txn_" +  str(tx_index) + "_hash_" + str(tx_hash)
        silk_filename = OUTPUT_DIR + filename + ".silk"
        rpcdaemon_filename = OUTPUT_DIR + filename + ".rpcdaemon"
        diff_filename = OUTPUT_DIR + filename + ".diffs"

        request = self.make_json_command_func(tx_hash)
        #print(request)

        silk_response = self.make_request(request, SILK_TARGET)
        rpcdaemon_response = self.make_request(request, RPCDAEMON_TARGET)

        self.dump_jsons(silk_filename, rpcdaemon_filename, silk_response, rpcdaemon_response)
        cmd = "json-diff -s " + silk_filename + " " + rpcdaemon_filename + " > " + diff_filename
        os.system (cmd)
        diff_file_size = os.stat(diff_filename).st_size
        if diff_file_size != 0:
            return 1
        os.remove(diff_filename)
        os.remove(silk_filename)
        os.remove(rpcdaemon_filename)
        return 0


    def scan_all_tx(self, start_block, start_tx: str):
        """ scans all tx starting from block """

        failed_request = 0
        for block in range(int(start_block), 18000000):
            print(f"{block:09d}\r", end='', flush=True)
            req = JsonRpc.create_get_block_by_number(hex(block))
            response = self.make_request(req, SILK_TARGET)
            if "error" in response:
                continue
            if response['result'] == 0 or len(response['result']['transactions']) == 0:
                #print ("skipped: ",response)
                continue
            transactions = response['result']['transactions']
            for txn in range(int(start_tx), len(transactions)):
                data = transactions[txn]['input']
                if len(data) < 2:
                    continue
                tx_hash = transactions[txn]['hash']
                res = self.compare_trace_replay_transaction(block, txn, tx_hash)
                if res == 1:
                    print ("Diff on block: ", block, " tx-index: ", txn, " Hash: ", tx_hash)
                    if self.continue_test == 0:
                        return 1
                    if self.max_failed_compare != 0:
                        failed_request += 1
                        if failed_request >= self.max_failed_compare:
                            return 1

        return 0
