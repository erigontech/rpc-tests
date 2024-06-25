
""" JSON RPC API scans DB """

import requests
import json
import os
import sys
import shutil
import getopt


SILK_TARGET="http://127.0.0.1:51515"
RPCDAEMON_TARGET="http://localhost:8545"
OUTPUT_DIR="./output/"


def make_request(json_rpc_cmd: str, target: str):
    """ make request to the silk server """
    response = None
    try:
        #print("Request:",json_rpc_cmd)
        cmd = '''curl --silent -X POST -H "Content-Type: application/json" ''' + ''' --data \'''' + json_rpc_cmd + '''\' ''' + target
        response = os.popen(cmd).read()
        #print("Response:",response)
        
        if len(response) == 0:
           print("empty response, request: ", json_rpc_cmd, " on: ", target)
           sys.exit(-1)
        #response = requests.post(target, data=json_rpc_cmd)
        #if response.status_code != 200:  
        #   print("Response got: ",response.status_code)
        #   sys.exit(-1)
    except requests.ConnectionError as ce:
        print("Post failed: ",ce)
    response_json = json.loads(response)
    return response_json


def create_get_block_by_number(block):
    """ create JSONRPC getBlockByNumber
    """
    return "{\"jsonrpc\":\"2.0\", \"method\":\"eth_getBlockByNumber\", \"params\":[" + "\""+str(block) + "\", true], \"id\":1}"

def create_trace_transaction(txn_hash):
    """ create JSONRPC trace_replayTransaction
    """
    return "{\"jsonrpc\":\"2.0\", \"method\":\"trace_replayTransaction\", \"params\":[" + "\"" + txn_hash + "\", [ \"vmTrace\" ] ], \"id\":1}"
    

def usage(argv):
    """ Print script usage """
    print("Usage: " + argv[0] + " [options]")
    print("")
    print("Launch a compare test on Silkrpc and RPCDaemon")
    print("")
    print("-h,--help:                            print this help")
    print("-s,--start:                           block::txn_number")
    print("-c,--continue:                        continue scanning. Doesn't stop at first diff")

def dump_jsons(silk_file, rpcdaemon_file, silk_response, rpcdaemon_response: str):
    """ dump jsons on result dir """
    if silk_file != "":
        with open(silk_file, 'w', encoding='utf8') as json1_file_ptr:
                json1_file_ptr.write(json.dumps(silk_response, indent=6, sort_keys=True))
    if rpcdaemon_file != "":
        with open(rpcdaemon_file, 'w', encoding='utf8') as json2_file_ptr:
                json2_file_ptr.write(json.dumps(rpcdaemon_response, indent=6, sort_keys=True))


def compare_trace_replayTransaction(block, tx_index, hash: str):
    filename = "bn_" + str(block) + "_txn_" +  str(tx_index) + "_hash_" + str(hash)
    silk_filename = OUTPUT_DIR + filename + ".silk"
    rpcdaemon_filename = OUTPUT_DIR + filename + ".rpcdaemon"
    diff_filename = OUTPUT_DIR + filename + ".diffs"

    request = create_trace_transaction(hash)

    silk_response = make_request(request, SILK_TARGET)
    rpcdaemon_response = make_request(request, RPCDAEMON_TARGET)

    dump_jsons(silk_filename, rpcdaemon_filename, silk_response, rpcdaemon_response)
    cmd = "json-diff -s " + silk_filename + " " + rpcdaemon_filename + " > " + diff_filename
    os.system (cmd)
    diff_file_size = os.stat(diff_filename).st_size
    if diff_file_size != 0:
       return 1
    else:
       os.remove(diff_filename)
       os.remove(silk_filename)
       os.remove(rpcdaemon_filename)
       return 0

    
#
# main
#
def main(argv) -> int:
    """ scans DB and found trace_replyTransactions response that differs from rpcdaemon and silk
    """
    opts, _ = getopt.getopt(argv[1:], "s:ch", ['help', 'start=', 'continue'])
    continue_test = False 
    start_block = 0
    start_tx = 0

    for option, optarg in opts:
       if option in ("-h", "--help"):
          usage(argv)
          return(1);
       elif option in ("-s", "--start"):
          start_block = optarg.split(':')[0]
          start_tx = optarg.split(':')[1]
       elif option in ("-c", "--continue"):
          continue_test = True 
       
    print ("Starting scans from: ", start_block, " tx-index: ", start_tx)

    if os.path.exists(OUTPUT_DIR) == 1:
        shutil.rmtree(OUTPUT_DIR)

    if os.path.exists(OUTPUT_DIR) == 0:
        os.mkdir(OUTPUT_DIR)

    for block in range(int(start_block), 18000000):  
       print(f"{block:09d}\r", end='', flush=True)
       req = create_get_block_by_number(hex(block))
       response = make_request(req, SILK_TARGET)
       if "error" in response:
          continue
       if response['result'] == 0 or len(response['result']['transactions']) == 0:
          #print ("skipped: ",response)
          continue
       transactions = response['result']['transactions'];
       for txn in range(int(start_tx), len(transactions)):  
          data = transactions[txn]['input']
          if len(data) < 2:
             continue
          hash = transactions[txn]['hash']
          res = compare_trace_replayTransaction(block, txn, hash)
          if res == 1:
             print ("Diff on block: ", block, " tx-index: ", txn, " Hash: ", hash)
             if continue_test == 0:
                return(1);
    return(0);
          

#
# module as main
#
if __name__ == "__main__":
    sys.exit(main(sys.argv))

