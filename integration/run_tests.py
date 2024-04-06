#!/usr/bin/python3
""" Run the JSON RPC API curl commands as integration tests """

from datetime import datetime
import getopt
import gzip
import json
import os
import shutil
import sys
import tarfile
import pytz
import jwt
from websockets.sync.client import connect
from websockets.extensions import permessage_deflate


SILK = "silk"
RPCDAEMON = "rpcdaemon"
EXTERNAL_PROVIDER = "external-provider"

tests_with_big_json = [
]

api_not_compared = [
    "goerli/trace_rawTransaction",  # erigon does not support raw tx but hash of tx
    "goerli/parity_getBlockReceipts",  # not supported by rpcdaemon
    "goerli/erigon_watchTheBurn",  # not supported by rpcdaemon
    "goerli/erigon_cumulativeChainTraffic",  # not supported by rpcdaemon
    "goerli/engine_exchangeCapabilities",  # not supported by silkrpc removed from ethbackend i/f
    "goerli/engine_forkchoiceUpdatedV1",  # not supported by silkrpc removed from ethbackend i/f
    "goerli/engine_forkchoiceUpdatedV2",  # not supported by silkrpc removed from ethbackend i/f
    "goerli/engine_getPayloadBodiesByHashV1",  # not supported by silkrpc removed from ethbackend i/f
    "goerli/engine_getPayloadBodiesByRangeV1",  # not supported by silkrpc removed from ethbackend i/f
    "goerli/engine_getPayloadV1",  # not supported by silkrpc removed from ethbackend i/f
    "goerli/engine_getPayloadV2",  # not supported by silkrpc removed from ethbackend i/f
    "goerli/engine_newPayloadV1",  # not supported by silkrpc removed from ethbackend i/f
    "goerli/engine_newPayloadV2"  # not supported by silkrpc removed from ethbackend i/f
]

tests_not_compared = [
    "goerli/debug_traceBlockByHash/test_02.tar",  # diff on gasCost
    "goerli/debug_traceBlockByHash/test_03.tar",  # diff on gasCost
    "goerli/debug_traceBlockByHash/test_04.tar",  # diff on gasCost

    "goerli/debug_traceBlockByNumber/test_02.tar",  # diff on gasCost
    "goerli/debug_traceBlockByNumber/test_09.tar",  # diff on gasCost
    "goerli/debug_traceBlockByNumber/test_10.tar",  # diff on gasCost
    "goerli/debug_traceBlockByNumber/test_11.tar",  # diff on gasCost
    "goerli/debug_traceBlockByNumber/test_12.tar",  # diff on gasCost
    "goerli/debug_traceBlockByNumber/test_14.tar",  # diff on gasCost

    "goerli/trace_replayBlockTransactions/test_01.tar",  # diff on gasCost
    "goerli/trace_replayBlockTransactions/test_02.tar",  # diff on gasCost

    "goerli/trace_replayTransaction/test_16.tar",  # diff on gasCost
    "goerli/trace_replayTransaction/test_23.tar",  # diff on gasCost

    "goerli/debug_traceCall/test_10.json",  # diff on gasCost
    "goerli/debug_traceCall/test_14.json",  # diff on gasCost
    "goerli/debug_traceCall/test_17.json",  # diff on gasCost

    "goerli/eth_getLogs/test_14.json",  # validator doesn't support earlist and latest
    "goerli/eth_getLogs/test_15.json",  # validator doesn't support earlist and latest

    "mainnet/debug_storageRangeAt/test_09.json",  # diff in storage entries
    "mainnet/debug_storageRangeAt/test_10.json",  # diff in storage entries

    "mainnet/debug_traceBlockByNumber/test_05.tar",  # json too big
    "mainnet/debug_traceBlockByNumber/test_06.tar",  # json too big
    "mainnet/debug_traceBlockByNumber/test_08.tar",  # json too big
    "mainnet/debug_traceBlockByNumber/test_09.tar",  # json too big
    "mainnet/debug_traceBlockByNumber/test_10.tar",  # json too big
    "mainnet/debug_traceBlockByNumber/test_11.tar",  # json too big
    "mainnet/debug_traceBlockByNumber/test_12.tar",  # json too big

    "mainnet/debug_traceBlockByHash/test_03.tar",  # diff on gasCost
    "mainnet/debug_traceTransaction/test_02.tar", # diff on gasCost

    "mainnet/debug_traceCall/test_02.json",  # diff on gasCost
    "mainnet/debug_traceCall/test_04.json", # diff on gasCost
    "mainnet/debug_traceCall/test_05.tar",  # diff on gasCost
    "mainnet/debug_traceCall/test_06.tar", # diff on gasCost
    "mainnet/debug_traceCall/test_08.tar",  # diff on gasCost
    "mainnet/debug_traceCall/test_10.tar", # diff on gasCost

    "mainnet/trace_block/test_01.json", # diff on error message
    "mainnet/trace_block/test_03.json", # diff on error message
    "mainnet/trace_block/test_04.tar", # diff on gasCost
    "mainnet/trace_block/test_05.tar", # diff on gasCost
    "mainnet/trace_block/test_06.tar", # diff on rewardType and author
    "mainnet/trace_block/test_15.tar", # diff on error message
    "mainnet/trace_block/test_17.tar", # diff on rewardType and author
    "mainnet/trace_block/test_18.tar", # diff on error message
    "mainnet/trace_block/test_19.tar", # diff on gasCost
    "mainnet/trace_block/test_20.tar" # diff on callType
]

tests_not_compared_result = [
    "goerli/trace_call/test_04.json",  # error message different invalidOpcode vs badInstructions
    "goerli/trace_call/test_11.json",  # error message different invalidOpcode vs badInstructions
    "goerli/trace_call/test_15.json",  # error message different invalidOpcode vs badInstructions
    "goerli/trace_call/test_17.json",  # error message different invalidOpcode vs badInstructions
    "goerli/trace_callMany/test_04.json",  # error message different invalidOpcode vs badInstructions
    "goerli/eth_callMany/test_04.json",  # error message different invalidOpcode vs badInstructions
    "goerli/trace_callMany/test_05.json",  # error message different invalidOpcode vs badInstructions
    "goerli/trace_callMany/test_13.json",  # error message different invalidOpcode vs badInstructions
    "goerli/trace_callMany/test_14.tar",  # error message different invalidOpcode vs badInstructions
    "goerli/trace_callMany/test_15.json"  # error message different invalidOpcode vs badInstructions
]

tests_not_compared_message = [
    "mainnet/eth_callMany/test_02.json",  # diff message
    "mainnet/eth_callMany/test_07.json",  # diff message
    "mainnet/eth_callMany/test_08.json",  # diff message
    "mainnet/eth_callMany/test_12.json",  # diff message
    "goerli/trace_callMany/test_10.json",  # silkrpc message contains also address
    "goerli/trace_callMany/test_11.json",  # silkrpc message contains also address
    "goerli/eth_callMany/test_08.json",  # silkrpc message contains few chars
    "goerli/trace_call/test_12.json",  # silkrpc message contains also address
    "goerli/trace_call/test_16.json",  # silkrpc message contains also address

    "mainnet/eth_callMany/test_04.json"  # diff on check order (precheck after check on have/want)
]

tests_not_compared_error = [
    "mainnet/eth_callMany/test_06.json",  # diff on opcode not defined (erigon print opcode in error message)
    "mainnet/eth_callMany/test_13.json",  # diff on opcode not defined (erigon print opcode in error message)
    "mainnet/eth_callMany/test_14.json",  # diff on stack underflow message (erigon print depth)
    "mainnet/eth_callMany/test_15.json"  # diff on opcode not defined (erigon print opcode in error message)
]

tests_message_lower_case = [
]


#
# usage
#
def usage(argv):
    """ Print script usage
    """
    print("Usage: " + argv[0] + ":")
    print("")
    print("Launch an automated test sequence on Silkworm RpcDaemon (aka Silkrpc) or Erigon RpcDaemon")
    print("")
    print("-h,--help: print this help")
    print("-f,--display-only-fail: shows only failed tests (not Skipped)")
    print("-v,--verbose: <verbose_level>")
    print("-c,--continue: runs all tests even if one test fails [default: exit at first test fail]")
    print("-l,--loops: <number of loops>")
    print("-b,--blockchain: [default: goerli]")
    print("-s,--start-from-test: <test_number>: run tests starting from input")
    print("-t,--run-single-test: <test_number>: run single test")
    print("-d,--compare-erigon-rpcdaemon: send requests also to the reference daemon e.g.: Erigon RpcDaemon")
    print("-T,--transport_type: <http,websocket>")
    print("-k,--jwt: authentication token file")
    print("-a,--api-list: <apis>: run all tests of the specified API (e.g.: eth_call,eth_getLogs,debug_)")
    print("-x,--exclude-api-list: exclude API list (e.g.: txpool_content,txpool_status,engine_)")
    print("-X,--exclude-test-list: exclude test list (e.g.: 18,22)")
    print("-o,--dump-response: dump JSON RPC response")
    print("-H,--host: host where the RpcDaemon is located (e.g.: 10.10.2.3)")
    print("-p,--port: port where the RpcDaemon is located (e.g.: 8545)")
    print("-r,--erigon-rpcdaemon: connect to Erigon RpcDaemon [default: connect to Silkrpc] ")
    print("-e,--verify-external-provider: <provider_url> send any request also to external API endpoint as reference")
    print("-i,--without-compare-results: send request without compare results")
    print("-C,--compression: enable compression")


def get_target_name(target_type: str):
    """ Return name server """
    if target_type == SILK:
        return "Silk"
    if target_type == RPCDAEMON:
        return "RpcDaemon"
    if target_type == EXTERNAL_PROVIDER:
        return "Infura"
    return "Undef"


def get_target(target_type: str, method: str, external_provider_url: str, host: str, port: int = 0):
    """ determine target
    """
    if "engine_" in method and target_type == SILK:
        return host + ":" + str(port if port > 0 else 51516)

    if "engine_" in method and target_type == RPCDAEMON:
        return host + ":" + str(port if port > 0 else 8551)

    if target_type == SILK:
        return host + ":" + str(port if port > 0 else 51515)

    if target_type == EXTERNAL_PROVIDER:
        return external_provider_url

    return host + ":" + str(port if port > 0 else 8545)


def get_json_filename_ext(target_type: str):
    """ determine json file name
    """
    if target_type == SILK:
        return "-silk.json"
    if target_type == EXTERNAL_PROVIDER:
        return "-external_provider_url.json"
    return "-rpcdaemon.json"


def get_jwt_secret(name):
    """ parse secret file
    """
    try:
        with open(name, encoding='utf8') as file:
            contents = file.readline()
            return contents[2:]
    except FileNotFoundError:
        return ""


def to_lower_case(file, dest_file):
    """ converts input string into lower case
    """
    cmd = "tr '[:upper:]' '[:lower:]' < " + file + " > " + dest_file
    os.system(cmd)


def replace_str_from_file(filer, filew, matched_string):
    """ parse file and replace string
    """
    with open(filer, "r", encoding='utf8') as input_file:
        with open(filew, "w", encoding='utf8') as output_file:
            # iterate all lines from file
            for line in input_file:
                # if text matches then don't write it
                if (matched_string in line) == 0:
                    output_file.write(line)


def replace_message(filer, filew, matched_string):
    """ parse file and replace string
    """
    with open(filer, "r", encoding='utf8') as input_file:
        with open(filew, "w", encoding='utf8') as output_file:
            # iterate all lines from file
            for line in input_file:
                # if text matches then don't write it
                if (matched_string in line) == 0:
                    output_file.write(line)
                else:
                    output_file.write("     \"message\": \"\"\n")


def modified_str_from_file(filer, filew, matched_string):
    """ parse file and convert string
    """
    with open(filer, "r", encoding='utf8') as input_file:
        with open(filew, "w", encoding='utf8') as output_file:
            # iterate all lines from file
            for line in input_file:
                # if text matches then don't write it
                if (matched_string in line) == 1:
                    output_file.write(line.lower())
                else:
                    output_file.write(line)


def is_skipped(api_name, net, exclude_api_list, exclude_test_list,
               test_name: str, req_test_number, verify_with_daemon,
               global_test_number):
    """ determine if test must be skipped
    """
    api_full_name = net + "/" + api_name
    api_full_test_name = net + "/" + test_name
    if req_test_number == -1 and verify_with_daemon == 1:
        for curr_test_name in api_not_compared:
            if curr_test_name == api_full_name:
                return 1
    if req_test_number == -1 and verify_with_daemon == 1:
        for curr_test in tests_not_compared:
            if curr_test == api_full_test_name:
                return 1
    if exclude_api_list != "":  # scans exclude api list (-x)
        tokenize_exclude_api_list = exclude_api_list.split(",")
        for exclude_api in tokenize_exclude_api_list:
            if exclude_api in api_full_name or exclude_api in api_full_test_name:
                return 1
    if exclude_test_list != "":  # scans exclude test list (-X)
        tokenize_exclude_test_list = exclude_test_list.split(",")
        for exclude_test in tokenize_exclude_test_list:
            if exclude_test == str(global_test_number):
                return 1
    return 0


def is_testing_apis(api_name, testing_apis: str):
    """ determine if api_name is in testing_apis
    """
    if testing_apis == "":
        return 1
    tokenize_list = testing_apis.split(",")
    for test in tokenize_list:
        if test in api_name:
            return 1
    return 0


def is_big_json(test_name, net: str, ):
    """ determine if json is in the big list
    """
    test_full_name = net + "/" + test_name
    for curr_test_name in tests_with_big_json:
        if curr_test_name == test_full_name:
            return 1
    return 0


def is_not_compared_result(test_name, net: str):
    """ determine if test not compared result
    """
    test_full_name = net + "/" + test_name
    for curr_test_name in tests_not_compared_result:
        if curr_test_name == test_full_name:
            return 1
    return 0


def is_not_compared_message(test_name, net: str):
    """ determine if test not compared message field
    """
    test_full_name = net + "/" + test_name
    for curr_test_name in tests_not_compared_message:
        if curr_test_name == test_full_name:
            return 1
    return 0

def is_not_compared_error(test_name, net: str):
    """ determine if test not compared error field
    """
    test_full_name = net + "/" + test_name
    for curr_test_name in tests_not_compared_error:
        if curr_test_name == test_full_name:
            return 1
    return 0

def is_message_to_be_converted(test_name, net: str):
    """ determine if test not compared result
    """
    test_full_name = net + "/" + test_name
    for curr_test_name in tests_message_lower_case:
        if curr_test_name == test_full_name:
            return 1
    return 0


class Config:
    # pylint: disable=too-many-instance-attributes
    """ This class manage User options params """

    def __init__(self):
        """ init the configuration params """
        self.exit_on_fail = True
        self.daemon_under_test = SILK
        self.daemon_as_reference = RPCDAEMON
        self.loop_number = 1
        self.verbose_level = 0
        self.req_test_number = -1
        self.force_dump_jsons = False
        self.external_provider_url = ""
        self.daemon_on_host = "localhost"
        self.daemon_on_port = 0
        self.testing_apis = ""
        self.verify_with_daemon = False
        self.net = "goerli"
        self.json_dir = "./" + self.net + "/"
        self.results_dir = "results"
        self.output_dir = self.json_dir + self.results_dir + "/"
        self.exclude_api_list = ""
        self.exclude_test_list = ""
        self.start_test = ""
        self.jwt_secret = ""
        self.display_only_fail = 0
        self.transport_type = "http"
        self.without_compare_results = False
        self.compression = False

    def select_user_options(self, argv):
        """ process user command """
        try:
            opts, _ = getopt.getopt(argv[1:], "iwhfrcv:t:l:a:de:b:ox:X:H:k:s:p:CT:",
                   ['help', 'continue', 'erigon-rpcdaemon', 'verify-external-provider', 'host=',
                   'port=', 'display-only-fail', 'verbose=', 'run-single-test=', 'start-from-test=',
                   'api-list=', 'loops=', 'compare-erigon-rpcdaemon', 'jwt=', 'blockchain=', 'compression',
                   'transport_type=', 'exclude-api-list=', 'exclude-test-list=', 'dump-response',
                   'without-compare-results'])
            for option, optarg in opts:
                if option in ("-h", "--help"):
                    usage(argv)
                    sys.exit(1)
                elif option in ("-c", "--continue"):
                    self.exit_on_fail = 0
                elif option in ("-r", "--erigon-rpcdaemon"):
                    if self.verify_with_daemon == 1:
                        print("Error on options: -r/--erigon-rpcdaemon is not compatible with -d/--compare-erigon-rpcdaemon")
                        usage(argv)
                        sys.exit(1)
                    self.daemon_under_test = RPCDAEMON
                elif option in ("-e", "--verify-external-provider"):
                    self.daemon_as_reference = EXTERNAL_PROVIDER
                    self.external_provider_url = optarg
                elif option in ("-H", "--host"):
                    self.daemon_on_host = optarg
                elif option in ("-p", "--port"):
                    self.daemon_on_port = int(optarg)
                elif option in ("-f", "--display-only-fail"):
                    self.display_only_fail = 1
                elif option in ("-v", "--verbose"):
                    self.verbose_level = int(optarg)
                elif option in ("-t", "--run-single-test"):
                    if self.exclude_test_list != "" or self.exclude_api_list != "":
                        print("Error on options: -t/--run-single-test is not compatible with -x/--exclude-api-list or -X/--exclude-test-list")
                        usage(argv)
                        sys.exit(1)
                    self.req_test_number = int(optarg)
                elif option in ("-s", "--start-from-test"):
                    self.start_test = int(optarg)
                elif option in ("-a", "--api-list"):
                    if self.exclude_api_list != "":
                        print("Error on options: -a/--api-list is not compatible with -X/--exclude-test-list")
                        usage(argv)
                        sys.exit(1)
                    self.testing_apis = optarg
                elif option in ("-l", "--loops"):
                    self.loop_number = int(optarg)
                elif option in ("-d", "--compare-erigon-rpcdaemon"):
                    if self.daemon_under_test != SILK:
                        print("Error in options: -d/--compare-erigon-rpcdaemon is not compatible with -r/--erigon-rpcdaemon")
                        usage(argv)
                        sys.exit(1)
                    if self.without_compare_results is True:
                        print("Error in options: -d/--compare-erigon-rpcdaemon is not compatible with -i/--without_compare_results")
                        usage(argv)
                        sys.exit(1)
                    self.verify_with_daemon = 1
                elif option in ("-o", "--dump-response"):
                    self.force_dump_jsons = 1
                elif option in ("-T", "--transport_type"):
                    if optarg == "":
                        print("Error in options: -T/--transport_type http,websocket")
                        usage(argv)
                        sys.exit(1)
                    tokenize_list = optarg.split(",")
                    for test in tokenize_list:
                        if test not in ['websocket','http']:
                            print("Error invalid connection type: ",test)
                            print("Error in options: -T/--transport_type http,websocket")
                            usage(argv)
                            sys.exit(1)
                    self.transport_type = optarg
                elif option in ("-b", "--blockchain"):
                    self.net = optarg
                    self.json_dir = "./" + self.net + "/"
                    self.output_dir = self.json_dir + self.results_dir + "/"
                elif option in ("-x", "--exclude-api-list"):
                    if self.req_test_number != -1 or self.testing_apis != "":
                        print("Error in options: -x/--exclude-api-list is not compatible with -a/--api-list or -t/--run-single-test")
                        usage(argv)
                        sys.exit(1)
                    self.exclude_api_list = optarg
                elif option in ("-X", "--exclude-test-list"):
                    if self.req_test_number != -1:
                        print("Error in options: -X/--exclude-test-list is not compatible with -t/--run-single-test")
                        usage(argv)
                        sys.exit(1)
                    self.exclude_test_list = optarg
                elif option in ("-k", "--jwt"):
                    self.jwt_secret = get_jwt_secret(optarg)
                    if self.jwt_secret == "":
                        print("secret file not found")
                        usage(argv)
                        sys.exit(1)
                elif option in ("-i", "--without-compare-results"):
                    if self.verify_with_daemon == 1:
                        print("Error on options: -i/--without-compare-results is not compatible with -d/--compare-erigon-rpcdaemon")
                        usage(argv)
                        sys.exit(1)
                    self.without_compare_results = True
                elif option in ("-C", "--compression"):
                    self.compression = True
                else:
                    print("Error option not managed:", option)
                    usage(argv)
                    sys.exit(1)

        except getopt.GetoptError as err:
            # print help information and exit:
            print(err)
            usage(argv)
            sys.exit(1)

        if os.path.exists(self.output_dir):
            shutil.rmtree(self.output_dir)


def get_json_from_response(msg, verbose_level: int, json_file, result: str, test_number, exit_on_fail: int):
    """ retrieve json from response """
    if verbose_level > 2:
        print(msg + " :[" + result + "]")

    if len(result) == 0:
        file = json_file.ljust(60)
        print(f"{test_number:03d}. {file} Failed [" + msg + "]  (json response is zero length)")
        if verbose_level:
            print(msg)
            print("Failed (json response zero length)")
        if exit_on_fail:
            print("TEST ABORTED!")
            sys.exit(1)
        return None
    try:
        response = json.loads(result)
        return response
    except json.decoder.JSONDecodeError:
        file = json_file.ljust(60)
        print(f"{test_number:03d}. {file} Failed [" + msg + "]  (bad json format)")
        if verbose_level:
            print(msg)
            print("Failed (bad json format)")
            print(result)
        if exit_on_fail:
            print("TEST ABORTED!")
            sys.exit(1)
        return None


def dump_jsons(dump_json, silk_file, exp_rsp_file, output_dir, response, expected_response: str):
    """ dump jsons on result dir """
    if dump_json:
        if silk_file != "" and os.path.exists(output_dir) == 0:
            os.mkdir(output_dir)
        if silk_file != "":
            with open(silk_file, 'w', encoding='utf8') as json_file_ptr:
                json_file_ptr.write(json.dumps(response, indent=6, sort_keys=True))
        if exp_rsp_file != "":
            with open(exp_rsp_file, 'w', encoding='utf8') as json_file_ptr:
                json_file_ptr.write(json.dumps(expected_response, indent=5, sort_keys=True))


def execute_request(transport_type: str, jwt_auth, encoded, request_dumps, target: str, verbose_level: int, compression: bool):
    """ execute request on server identified by target """
    if transport_type == "http":
        options = jwt_auth
        if compression:
            options = options + " --compressed  "
        cmd = '''curl --silent -X POST -H "Content-Type: application/json" ''' + options + ''' --data \'''' + request_dumps + '''\' ''' + target
        result = os.popen(cmd).read()
    else:
        ws_target = "ws://" + target  # use websocket
        if compression:
            selected_compression='deflate'
            curr_extensions=[
                 permessage_deflate.ClientPerMessageDeflateFactory(
                     client_max_window_bits=15,
                     compress_settings={"memLevel": 4}
                 ),
            ]
        else:
            selected_compression=None
            curr_extensions=None
        try:
            with connect(ws_target, max_size=1000048576, compression=selected_compression, extensions=curr_extensions) as websocket:
                websocket.send(request_dumps)
                result = websocket.recv(None)
        except:
            print("\nwebsocket connection fail")
            print("TEST ABORTED!")
            sys.exit(1)

    if verbose_level > 1:
        print ("\n",target)
        print (request_dumps)
        print ("Response.len:",len(result))
    return result


def compare_json(net, response, json_file, silk_file, exp_rsp_file, diff_file: str, verbose_level, test_number,
                 exit_on_fail: int):
    """ Compare jsos response. """
    temp_file1 = "/tmp/silk_lower_case"
    temp_file2 = "/tmp/rpc_lower_case"

    if "error" in response:
        to_lower_case(silk_file, temp_file1)
        to_lower_case(exp_rsp_file, temp_file2)
    else:
        cmd = "cp " + silk_file + " " + temp_file1
        os.system(cmd)
        cmd = "cp " + exp_rsp_file + " " + temp_file2
        os.system(cmd)

    if is_not_compared_result(json_file, net):
        removed_line_string = "error"
        replace_str_from_file(exp_rsp_file, temp_file1, removed_line_string)
        replace_str_from_file(silk_file, temp_file2, removed_line_string)
        cmd = "json-diff -s " + temp_file2 + " " + temp_file1 + " > " + diff_file
    elif is_not_compared_message(json_file, net):
        removed_line_string = "message"
        replace_message(exp_rsp_file, temp_file1, removed_line_string)
        replace_message(silk_file, temp_file2, removed_line_string)
        cmd = "json-diff -s " + temp_file2 + " " + temp_file1 + " > " + diff_file
    elif is_not_compared_error(json_file, net):
        removed_line_string = "error"
        replace_message(exp_rsp_file, temp_file1, removed_line_string)
        replace_message(silk_file, temp_file2, removed_line_string)
        cmd = "json-diff -s " + temp_file2 + " " + temp_file1 + " > " + diff_file
    elif is_message_to_be_converted(json_file, net):
        modified_string = "message"
        modified_str_from_file(exp_rsp_file, temp_file1, modified_string)
        modified_str_from_file(silk_file, temp_file2, modified_string)
        cmd = "json-diff -s " + temp_file2 + " " + temp_file1 + " > " + diff_file
    #        elif is_big_json(json_file, net):
    #            cmd = "json-patch-jsondiff --indent 4 " + temp_file2 + " " + temp_file1 + " > " + diff_file
    else:
        cmd = "json-diff -s " + temp_file2 + " " + temp_file1 + " > " + diff_file
    os.system(cmd)
    diff_file_size = os.stat(diff_file).st_size
    if diff_file_size != 0:
        file = json_file.ljust(60)
        print(f"{test_number:03d}. {file} Failed")
        if verbose_level:
            print("Failed")
        if exit_on_fail:
            print("TEST ABORTED!")
            sys.exit(1)
        return 0
    if verbose_level:
        print("OK")

    # cleanup
    if os.path.exists(temp_file1):
        os.remove(temp_file1)
    if os.path.exists(temp_file2):
        os.remove(temp_file2)
    return 1


def process_response(net, result, result1, response_in_file: str, verbose_level: int, exit_on_fail: bool,
                     output_dir: str, silk_file: str,
                     exp_rsp_file: str, diff_file: str, force_dump_jsons, json_file: str, test_number: int,
                     daemon_under_test, daemon_as_reference: str, without_compare_results):
    """ Process the response If exact result or error don't care, they are null but present in expected_response. """

    response = get_json_from_response(daemon_under_test, verbose_level, json_file, result, test_number, exit_on_fail)
    if response is None:
        return 0

    if result1 != "":
        expected_response = get_json_from_response(daemon_as_reference, verbose_level, json_file, result1, test_number,
                                                   exit_on_fail)
        if expected_response is None:
            return 0
    else:
        expected_response = response_in_file

    if without_compare_results is True:
        if verbose_level:
            print("OK")
        dump_jsons(force_dump_jsons, silk_file, exp_rsp_file, output_dir, response, expected_response)
        return 1

    if response != expected_response:
        if "result" in response and "result" in expected_response and expected_response["result"] is None:
            # response and expected_response are different but don't care
            if verbose_level:
                print("OK")
            dump_jsons(force_dump_jsons, silk_file, exp_rsp_file, output_dir, response, expected_response)
            return 1
        if "error" in response and "error" in expected_response and expected_response["error"] is None:
            # response and expected_response are different but don't care
            if verbose_level:
                print("OK")
            dump_jsons(force_dump_jsons, silk_file, exp_rsp_file, output_dir, response, expected_response)
            return 1
        if "error" not in expected_response and "result" not in expected_response:
            # response and expected_response are different but don't care
            if verbose_level:
                print("OK")
            dump_jsons(force_dump_jsons, silk_file, exp_rsp_file, output_dir, response, expected_response)
            return 1
        dump_jsons(True, silk_file, exp_rsp_file, output_dir, response, expected_response)

        same = compare_json(net, response, json_file, silk_file, exp_rsp_file, diff_file,
                            verbose_level, test_number, exit_on_fail)
        # cleanup
        if same:
            os.remove(silk_file)
            os.remove(exp_rsp_file)
            os.remove(diff_file)
        if not os.listdir(output_dir):
            os.rmdir(output_dir)

        return same

    if verbose_level:
        print("OK")

    dump_jsons(force_dump_jsons, silk_file, exp_rsp_file, output_dir, response, expected_response)
    return 1


def run_test(net: str, test_dir: str, output_dir: str, json_file: str, verbose_level: int,
             daemon_under_test: str, exit_on_fail: bool, verify_with_daemon: bool,
             daemon_as_reference: str, force_dump_jsons: bool, test_number, external_provider_url: str,
             daemon_on_host: str, daemon_on_port: int,
             jwt_secret: str, transport_type, without_compare_results: bool, compression: bool):
    """ Run integration tests. """
    json_filename = test_dir + json_file
    ext = os.path.splitext(json_file)[1]

    if ext in (".zip", ".tar"):
        with tarfile.open(json_filename, encoding='utf-8') as tar:
            files = tar.getmembers()
            if len(files) != 1:
                print("bad archive file " + json_filename)
                sys.exit(1)
            file = tar.extractfile(files[0])
            buff = file.read()
            tar.close()
            jsonrpc_commands = json.loads(buff)
    elif ext in ".gzip":
        with gzip.open(json_filename, 'rb') as zipped_file:
            buff = zipped_file.read()
            jsonrpc_commands = json.loads(buff)
    else:
        with open(json_filename, encoding='utf8') as json_file_ptr:
            jsonrpc_commands = json.load(json_file_ptr)
    for json_rpc in jsonrpc_commands:
        request = json_rpc["request"]
        try:
            if isinstance(request, dict) == 1:
                method = request["method"]
            else:
                method = request[0]["method"]
        except KeyError:
            method = ""
        request_dumps = json.dumps(request)
        target = get_target(daemon_under_test, method, external_provider_url, daemon_on_host, daemon_on_port)
        if jwt_secret == "":
            jwt_auth = ""
            encoded = ""
        else:
            byte_array_secret = bytes.fromhex(jwt_secret)
            encoded = jwt.encode({"iat": datetime.now(pytz.utc)}, byte_array_secret, algorithm="HS256")
            jwt_auth = "-H \"Authorization: Bearer " + str(encoded) + "\" "
        if verify_with_daemon == 0:  # compare daemon result with file
            result = execute_request(transport_type, jwt_auth, encoded, request_dumps, target, verbose_level, compression)
            result1 = ""
            response_in_file = json_rpc["response"]

            output_api_filename = output_dir + json_file[:-4]
            output_dir_name = output_api_filename[:output_api_filename.rfind("/")]
            diff_file = output_api_filename + "diff.json"

            silk_file = output_api_filename + "response.json"
            exp_rsp_file = output_api_filename + "expResponse.json"
        else:  # run tests with both servers
            target = get_target(SILK, method, external_provider_url, daemon_on_host, daemon_on_port)
            result = execute_request(transport_type, jwt_auth, encoded, request_dumps, target, verbose_level, compression)
            target1 = get_target(daemon_as_reference, method, external_provider_url, daemon_on_host, daemon_on_port)
            result1 = execute_request(transport_type, jwt_auth, encoded, request_dumps, target1, verbose_level, compression)
            response_in_file = None

            output_api_filename = output_dir + json_file[:-4]
            output_dir_name = output_api_filename[:output_api_filename.rfind("/")]
            diff_file = output_api_filename + "diff.json"

            silk_file = output_api_filename + get_json_filename_ext(SILK)
            exp_rsp_file = output_api_filename + get_json_filename_ext(daemon_as_reference)

        return process_response(
            net,
            result,
            result1,
            response_in_file,
            verbose_level,
            exit_on_fail,
            output_dir_name,
            silk_file,
            exp_rsp_file,
            diff_file,
            force_dump_jsons,
            json_file,
            test_number,
            daemon_under_test,
            daemon_as_reference,
            without_compare_results)


#
# main
#
def main(argv) -> int:
    """ parse command line and execute tests
    """
    config = Config()
    config.select_user_options(argv)

    tstart = datetime.now()
    os.mkdir(config.output_dir)
    match = 0
    executed_tests = 0
    failed_tests = 0
    success_tests = 0
    tests_not_executed = 0
    global_test_number = 1
    for test_rep in range(0, config.loop_number):  # makes tests more times
        test_number_in_any_loop = 1
        if config.verbose_level:
            print("Test iteration: ", test_rep + 1)
        tokenize_transport_type = config.transport_type.split(",")
        for transport_type in tokenize_transport_type:
            dirs = sorted(os.listdir(config.json_dir))
            for api_name in dirs:  # scans all api present in dir
                # jump results folder or any hidden OS-specific folder
                if api_name == config.results_dir or api_name.startswith("."):
                    continue
                test_dir = config.json_dir + api_name
                if not os.path.isdir(test_dir):  # jump if not dir
                    continue
                test_lists = sorted(os.listdir(test_dir))
                test_number = 1
                for test_name in test_lists:  # scan all json test present in the dir
                    if (test_name in ["json", "zip", "gzip"] == 0):  # if file doesn't terminate with .json, .gzip, .tar jump it
                        continue
                    if is_testing_apis(api_name, config.testing_apis):  # -a or all
                        test_file = api_name + "/" + test_name
                        if is_skipped(api_name, config.net, config.exclude_api_list, config.exclude_test_list, test_file,
                                      config.req_test_number,
                                      config.verify_with_daemon, test_number_in_any_loop) == 1:
                            if config.start_test == "" or test_number_in_any_loop >= int(config.start_test):
                                if config.display_only_fail == 0 and config.req_test_number != "":
                                    file = test_file.ljust(60)
                                    print(f"{test_number_in_any_loop:03d}. {file} Skipped")
                                    tests_not_executed = tests_not_executed + 1
                        else:
                            # runs all tests or
                            # runs single global test
                            # runs only tests a specific test_number in the testing_apis list
                            if ((config.testing_apis == "" and config.req_test_number in (-1, test_number_in_any_loop)) or
                                (config.testing_apis != "" and config.req_test_number in (-1, test_number))):
                                if (config.start_test == "" or # start from specific test
                                    (config.start_test != "" and test_number_in_any_loop >= int(config.start_test))):
                                    file = test_file.ljust(60)
                                    curr_tt = transport_type.ljust(8)
                                    if config.verbose_level:
                                        print(f"{test_number_in_any_loop:03d}. {curr_tt}::{file} ", end='', flush=True)
                                    else:
                                        print(f"{test_number_in_any_loop:03d}. {curr_tt}::{file}\r", end='', flush=True)
                                    ret = run_test(config.net, config.json_dir, config.output_dir,
                                               test_file,
                                               config.verbose_level, config.daemon_under_test,
                                               config.exit_on_fail, config.verify_with_daemon,
                                               config.daemon_as_reference,
                                               config.force_dump_jsons, test_number_in_any_loop,
                                               config.external_provider_url,
                                               config.daemon_on_host, config.daemon_on_port,
                                               config.jwt_secret,
                                               transport_type,
                                               config.without_compare_results,
                                               config.compression)
                                    if ret == 1:
                                        success_tests = success_tests + 1
                                    else:
                                        failed_tests = failed_tests + 1
                                    executed_tests = executed_tests + 1
                                    if config.req_test_number != -1 or config.testing_apis != "":
                                        match = 1

                    global_test_number = global_test_number + 1
                    test_number_in_any_loop = test_number_in_any_loop + 1
                    test_number = test_number + 1

    if (config.req_test_number != -1 or config.testing_apis != "") and match == 0:
        print("ERROR: api or testNumber not found")
        return 1

    tend = datetime.now()
    elapsed = tend - tstart
    print("                                                                                    \r")
    print(f"Test time-elapsed:            {str(elapsed)}")
    print(f"Number of executed tests:     {executed_tests}/{global_test_number - 1}")
    print(f"Number of NOT executed tests: {tests_not_executed}")
    print(f"Number of success tests:      {success_tests}")
    print(f"Number of failed tests:       {failed_tests}")

    return failed_tests


#
# module as main
#
if __name__ == "__main__":
    sys.exit(main(sys.argv))
