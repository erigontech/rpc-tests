#!/usr/bin/python3
""" Run the JSON RPC API curl commands as integration tests """

from datetime import datetime
import getopt
import gzip
import json
import os
import random
import re
import shutil
import sys
import tarfile
import time
from concurrent.futures import ProcessPoolExecutor
import pytz
import jwt
import requests
from websockets.sync.client import connect
from websockets.extensions import permessage_deflate

DAEMON_ON_OTHER_PORT = "other-daemon"
DAEMON_ON_DEFAULT_PORT = "rpcdaemon"
NONE = "none"
EXTERNAL_PROVIDER = "external-provider"
TIME = 0.1
MAX_TIME = 200  # times of TIME secs

api_not_compared = [
    "mainnet/engine_getClientVersionV1",  # not supported by erigon
    "mainnet/trace_rawTransaction",       # not supported by erigon
    "mainnet/engine_"                     # not supported on external EP
]

tests_not_compared = [
]

tests_not_compared_message = [
]

tests_not_compared_error = [
]


tests_on_latest = [
   "mainnet/eth_blockNumber",
    "mainnet/debug_traceBlockByNumber/test_24.json",
    "mainnet/debug_traceBlockByNumber/test_30.json",
    "mainnet/debug_traceCall/test_22.json",
    "mainnet/debug_traceCall/test_33.json",
    "mainnet/debug_traceCall/test_34.json",
    "mainnet/debug_traceCall/test_35.json",
    "mainnet/debug_traceCall/test_36.json",
    "mainnet/debug_traceCall/test_37.json",
    "mainnet/debug_traceCall/test_38.json",
    "mainnet/debug_traceCall/test_39.json",
    "mainnet/debug_traceCall/test_40.json",
    "mainnet/debug_traceCallMany/test_11.json",
    "mainnet/debug_traceCallMany/test_12.json",
    "mainnet/eth_block_number",                                                  # works always on latest block
    "mainnet/eth_call/test_20.json",
    "mainnet/eth_call/test_28.json",
    "mainnet/eth_call/test_29.json",
    "mainnet/eth_callBundle/test_09.json",
    "mainnet/eth_createAccessList/test_18.json",
    "mainnet/eth_createAccessList/test_19.json",
    "mainnet/eth_createAccessList/test_20.json",
    "mainnet/eth_estimateGas/test_01",
    "mainnet/eth_estimateGas/test_02",
    "mainnet/eth_estimateGas/test_03",
    "mainnet/eth_estimateGas/test_04",
    "mainnet/eth_estimateGas/test_05",
    "mainnet/eth_estimateGas/test_06",
    "mainnet/eth_estimateGas/test_07",
    "mainnet/eth_estimateGas/test_08",
    "mainnet/eth_estimateGas/test_09",
    "mainnet/eth_estimateGas/test_10",
    "mainnet/eth_estimateGas/test_11",
    "mainnet/eth_estimateGas/test_12",
    "mainnet/eth_estimateGas/test_21",
    "mainnet/eth_estimateGas/test_22",
    "mainnet/eth_estimateGas/test_23",
    "mainnet/eth_feeHistory/test_07.json",
    "mainnet/eth_feeHistory/test_22.json",
    "mainnet/eth_getBalance/test_03.json",
    "mainnet/eth_getBalance/test_26.json",
    "mainnet/eth_getBalance/test_27.json",
    "mainnet/eth_getBlockTransactionCountByNumber/test_03.json",
    "mainnet/eth_getBlockByNumber/test_10.json",
    "mainnet/eth_getBlockByNumber/test_27.json",
    "mainnet/eth_getBlockReceipts/test_07.json",
    "mainnet/eth_getCode/test_05.json",
    "mainnet/eth_getCode/test_06.json",
    "mainnet/eth_getCode/test_07.json",
    "mainnet/eth_getLogs/test_21.json",
    "mainnet/eth_getProof/test_01.json",
    "mainnet/eth_getProof/test_02.json",
    "mainnet/eth_getProof/test_03.json",
    "mainnet/eth_getProof/test_04.json",
    "mainnet/eth_getProof/test_05.json",
    "mainnet/eth_getProof/test_06.json",
    "mainnet/eth_getProof/test_07.json",
    "mainnet/eth_getProof/test_08.json",
    "mainnet/eth_getProof/test_09.json",
    "mainnet/eth_getProof/test_10.json",
    "mainnet/eth_getRawTransactionByBlockNumberAndIndex/test_11.json",
    "mainnet/eth_getRawTransactionByBlockNumberAndIndex/test_12.json",
    "mainnet/eth_getRawTransactionByBlockNumberAndIndex/test_13.json",
    "mainnet/eth_getStorageAt/test_04.json",
    "mainnet/eth_getStorageAt/test_07.json",
    "mainnet/eth_getStorageAt/test_08.json",
    "mainnet/eth_getTransactionByBlockNumberAndIndex/test_02.json",
    "mainnet/eth_getTransactionByBlockNumberAndIndex/test_08.json",
    "mainnet/eth_getTransactionByBlockNumberAndIndex/test_09.json",
    "mainnet/eth_getTransactionCount/test_02.json",
    "mainnet/eth_getTransactionCount/test_07.json",
    "mainnet/eth_getTransactionCount/test_08.json",
    "mainnet/eth_getUncleCountByBlockNumber/test_03.json",
    "mainnet/eth_getUncleByBlockNumberAndIndex/test_02.json",
    "mainnet/erigon_blockNumber/test_4.json",
    "mainnet/erigon_blockNumber/test_6.json",
    "mainnet/ots_hasCode/test_10.json",
    "mainnet/ots_searchTransactionsBefore/test_02.json",
    "mainnet/parity_listStorageKeys",
    "mainnet/trace_call/test_26.json",
    "mainnet/trace_callMany/test_15.json",
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
    print("-j,--json-diff: use json-diff to make compare [default use json-diff]")
    print("-f,--display-only-fail: shows only failed tests (not Skipped) [default: print all] ")
    print("-E,--do-not-compare-error: do not compare error")
    print("-v,--verbose: <verbose_level> 0: no message for each test; 1: print operation result; 2: print request and response message) [default verbose_level 0]")
    print("-c,--continue: runs all tests even if one test fails [default: exit at first failed test]")
    print("-l,--loops: <number of loops> [default loop 1]")
    print("-b,--blockchain: [default: mainnet]")
    print("-s,--start-from-test: <test_number>: run tests starting from specified test number [default starts from 1]")
    print("-t,--run-test: <test_number>: run single test using global test number (i.e: -t 256 runs 256 test) or test number of one specified APi used in combination with -a or -A (i.e -a eth_getLogs() -t 3: run test 3 of eth_getLogs())")
    print("-d,--compare-erigon-rpcdaemon: send requests also to the reference daemon e.g.: Erigon RpcDaemon")
    print("-T,--transport_type: <http,http_comp,https,websocket,websocket_comp> [default http]")
    print("-k,--jwt: authentication token file (i.e -k /tmp/jwt_file.hex) ")
    print("-K,--create-jwt: generate authentication token file and use it (-K /tmp/jwt_file.hex) ")
    print("-a,--api-list-with: <apis>: run all tests of the specified API that contains string (e.g.: eth_,debug_)")
    print("-A,--api-list: <apis>: run all tests of the specified API that match full name (e.g.: eth_call,eth_getLogs)")
    print("-x,--exclude-api-list < list of tested api>: exclude API list (e.g.: txpool_content,txpool_status,engine_)")
    print("-X,--exclude-test-list <test-list>: exclude test list (e.g.: 18,22, 18,22 are global test number)")
    print("-o,--dump-response: dump JSON RPC response even if the response are the same")
    print("-H,--host: host where the RpcDaemon is located (e.g.: 10.10.2.3)")
    print("-p,--port: port where the RpcDaemon is located (e.g.: 8545)")
    print("-I,--daemon-port: Use 51515/51516 ports to server")
    print("-e,--verify-external-provider: <provider_url> send any request also to external API endpoint as reference")
    print("-i,--without-compare-results: send request and waits response without compare results (used only to see the response time to execute one api or more apis)")
    print("-w,--waiting_time: wait time after test execution in milliseconds (can be used only for serial test see -S)")
    print("-S,--serial: all tests run in serial way [default: the selected files run in parallel]")
    print("-L,--tests-on-latest-block: runs only test on latest block]")


def get_target(target_type: str, method: str, config):
    """ determine target
    """

    if target_type == EXTERNAL_PROVIDER:
        return config.external_provider_url

    if config.verify_with_daemon and target_type == DAEMON_ON_OTHER_PORT and "engine_" in method:
        return config.daemon_on_host + ":" + str(51516)

    if config.verify_with_daemon and target_type == DAEMON_ON_OTHER_PORT:
        return config.daemon_on_host + ":" + str(51515)

    if target_type == DAEMON_ON_OTHER_PORT and "engine_" in method:
        return config.daemon_on_host + ":" + str(51516)

    if target_type == DAEMON_ON_OTHER_PORT:
        return config.daemon_on_host + ":" + str(51515)

    if "engine_" in method:
        return config.daemon_on_host + ":" + str(config.engine_port if config.engine_port > 0 else 8551)

    return config.daemon_on_host + ":" + str(config.server_port if config.server_port > 0 else 8545)


def get_json_filename_ext(target_type: str, target):
    """ determine json file name
    """
    port = target.split(":")
    if target_type == DAEMON_ON_OTHER_PORT:
        return "_" + port[1] + "-daemon.json"
    if target_type == EXTERNAL_PROVIDER:
        return "-external_provider_url.json"
    return "_" + port[1] + "-rpcdaemon.json"


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


def is_skipped(curr_api, test_name: str, global_test_number, config):
    """ determine if test must be skipped
    """
    api_full_name = config.net + "/" + curr_api
    api_full_test_name = config.net + "/" + test_name
    if ((config.req_test_number == -1 or config.testing_apis != "" or config.testing_apis_with != "") and  # -t or -a, or -A
        not (config.req_test_number != -1 and (config.testing_apis != "" or config.testing_apis_with != "")) and  # NOT (-t and (-A or -a))
            config.exclude_api_list == "" and config.exclude_test_list == ""):  # if not -t and -x and -X are null -x or -X
        for curr_test_name in api_not_compared:
            if curr_test_name in api_full_name:
                return 1
        for curr_test in tests_not_compared:
            if curr_test in api_full_test_name:
                return 1
    if config.exclude_api_list != "":  # scans exclude api list (-x)
        tokenize_exclude_api_list = config.exclude_api_list.split(",")
        for exclude_api in tokenize_exclude_api_list:
            if exclude_api in api_full_name or exclude_api in api_full_test_name:
                return 1
    if config.exclude_test_list != "":  # scans exclude test list (-X)
        tokenize_exclude_test_list = config.exclude_test_list.split(",")
        for exclude_test in tokenize_exclude_test_list:
            if exclude_test == str(global_test_number):
                return 1
    return 0


def verify_in_latest_list(curr_api, test_name, config):
    """ verify if the test in test from latest block
    """
    api_full_test_name = config.net + "/" + test_name
    if config.tests_on_latest_block is True:
        for curr_test in tests_on_latest:
            if curr_test in api_full_test_name:
                return 1

    return 0


def api_under_test(curr_api, test_name, config):
    """ determine if curr_api is in testing_apis_with or == testing_apis
    """
    if config.testing_apis_with == "" and config.testing_apis == "" and config.tests_on_latest_block is False:
        return 1

    if config.testing_apis_with != "":
        tokenize_list = config.testing_apis_with.split(",")
        for test in tokenize_list:
            if test in curr_api:
                if config.tests_on_latest_block and verify_in_latest_list(curr_api, test_name, config):
                    return 1
                if config.tests_on_latest_block:
                    return 0
                return 1
        return 0

    if config.testing_apis != "":
        tokenize_list = config.testing_apis.split(",")
        for test in tokenize_list:
            if test == curr_api:
                if config.tests_on_latest_block and verify_in_latest_list(curr_api, test_name, config):
                    return 1
                if config.tests_on_latest_block:
                    return 0
                return 1

        return 0

    in_latest_list = 0
    if config.tests_on_latest_block is True:
        in_latest_list = verify_in_latest_list(curr_api, test_name, config)

    return in_latest_list


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


class Config:
    # pylint: disable=too-many-instance-attributes
    """ This class manage User options params """

    def __init__(self):
        """ init the configuration params """
        self.exit_on_fail = True
        self.daemon_under_test = DAEMON_ON_DEFAULT_PORT
        self.daemon_as_reference = NONE
        self.loop_number = 1
        self.verbose_level = 0
        self.req_test_number = -1
        self.force_dump_jsons = False
        self.external_provider_url = ""
        self.daemon_on_host = "localhost"
        self.server_port = 0
        self.engine_port = 0
        self.testing_apis_with = ""
        self.testing_apis = ""
        self.verify_with_daemon = False
        self.net = "mainnet"
        self.json_dir = "./" + self.net + "/"
        self.results_dir = "results"
        self.output_dir = self.json_dir + self.results_dir + "/"
        self.exclude_api_list = ""
        self.exclude_test_list = ""
        self.start_test = ""
        self.jwt_secret = ""
        self.display_only_fail = False
        self.transport_type = "http"
        self.parallel = True
        self.use_jsondiff = True
        self.without_compare_results = False
        self.waiting_time = 0
        self.do_not_compare_error = False
        self.tests_on_latest_block = False

    def select_user_options(self, argv):
        """ process user command """
        try:
            opts, _ = getopt.getopt(argv[1:], "iw:hfIcv:t:l:a:de:b:ox:X:H:k:s:p:P:T:A:jSK:EL",
                                    ['help', 'continue', 'daemon-port', 'verify-external-provider', 'host=', 'engine-port=',
                                     'port=', 'display-only-fail', 'verbose=', 'run-single-test=', 'start-from-test=',
                                     'api-list-with=', 'api-list=', 'loops=', 'compare-erigon-rpcdaemon', 'jwt=', 'create-jwt=', 'blockchain=',
                                     'transport_type=', 'exclude-api-list=', 'exclude-test-list=', 'json-diff', 'waiting_time=',
                                     'dump-response', 'without-compare-results', 'serial', 'do-not-compare-error', 'tests-on-latest-block'])
            for option, optarg in opts:
                if option in ("-h", "--help"):
                    usage(argv)
                    sys.exit(1)
                elif option in ("-w", "--waiting_time"):
                    if self.parallel:
                        print("Error on options: "
                              "-w/--waiting_time is not compatible with parallel tests configuration (default config)")
                        usage(argv)
                        sys.exit(1)
                    self.waiting_time = int(optarg)
                elif option in ("-c", "--continue"):
                    self.exit_on_fail = False
                elif option in ("-I", "--daemon-port"):
                    if self.verify_with_daemon is True:
                        print("Error on options: "
                              "-I/--daemon-port is not compatible with -d/--compare-erigon-rpcdaemon")
                        usage(argv)
                        sys.exit(1)
                    self.daemon_under_test = DAEMON_ON_OTHER_PORT
                elif option in ("-e", "--verify-external-provider"):
                    self.daemon_as_reference = EXTERNAL_PROVIDER
                    self.external_provider_url = optarg
                    self.verify_with_daemon = True
                elif option in ("-S", "--serial"):
                    self.parallel = False
                elif option in ("-H", "--host"):
                    self.daemon_on_host = optarg
                elif option in ("-L", "--tests-on-latest-block"):
                    self.tests_on_latest_block = True
                elif option in ("-p", "--port"):
                    self.server_port = int(optarg)
                elif option in ("-P", "--engine-port"):
                    self.engine_port = int(optarg)
                elif option in ("-f", "--display-only-fail"):
                    self.display_only_fail = True
                elif option in ("-v", "--verbose"):
                    if int(optarg) > 2:
                        print("Error on verbose level: permitted values: 0,1,2")
                        usage(argv)
                        sys.exit(1)
                    self.verbose_level = int(optarg)
                elif option in ("-t", "--run-single-test"):
                    if self.exclude_test_list != "" or self.exclude_api_list != "":
                        print("Error on options: "
                              "-t/--run-single-test is not compatible with -x/--exclude-api-list or"
                              " -X/--exclude-test-list")
                        usage(argv)
                        sys.exit(1)
                    self.req_test_number = int(optarg)
                elif option in ("-s", "--start-from-test"):
                    self.start_test = int(optarg)
                elif option in ("-a", "--api-list-with"):
                    self.testing_apis_with = optarg
                elif option in ("-A", "--api-list"):
                    if self.exclude_api_list != "":
                        print("Error on options: "
                              "-A/--api-list is not compatible with -X/--exclude-test-list")
                        usage(argv)
                        sys.exit(1)
                    self.testing_apis = optarg
                elif option in ("-l", "--loops"):
                    self.loop_number = int(optarg)
                elif option in ("-d", "--compare-erigon-rpcdaemon"):
                    if self.daemon_under_test != DAEMON_ON_DEFAULT_PORT:
                        print("Error in options: "
                              "-d/--compare-erigon-rpcdaemon is not compatible with -I/--daemon-port")
                        usage(argv)
                        sys.exit(1)
                    if self.without_compare_results is True:
                        print("Error in options: "
                              "-d/--compare-erigon-rpcdaemon is not compatible with -i/--without_compare_results")
                        usage(argv)
                        sys.exit(1)
                    self.verify_with_daemon = True
                    self.daemon_as_reference = DAEMON_ON_DEFAULT_PORT
                    self.use_jsondiff = True
                elif option in ("-o", "--dump-response"):
                    self.force_dump_jsons = 1
                elif option in ("-T", "--transport_type"):
                    if optarg == "":
                        print("Error in options: -T/--transport_type http,http_comp,https,websocket,websocket_comp")
                        usage(argv)
                        sys.exit(1)
                    tokenize_list = optarg.split(",")
                    for test in tokenize_list:
                        if test not in ['websocket', 'http', 'http_comp', 'https', 'websocket_comp']:
                            print("Error invalid connection type: ", test)
                            print("Error in options: -T/--transport_type http,http_comp,https,websocket,websocket_comp")
                            usage(argv)
                            sys.exit(1)
                    self.transport_type = optarg
                elif option in ("-b", "--blockchain"):
                    self.net = optarg
                    self.json_dir = "./" + self.net + "/"
                    self.output_dir = self.json_dir + self.results_dir + "/"
                elif option in ("-x", "--exclude-api-list"):
                    self.exclude_api_list = optarg
                elif option in ("-X", "--exclude-test-list"):
                    if self.req_test_number != -1:
                        print("Error in options: "
                              "-X/--exclude-test-list is not compatible with -t/--run-single-test")
                        usage(argv)
                        sys.exit(1)
                    self.exclude_test_list = optarg
                elif option in ("-K", "--create-jwt"):
                    generate_jwt_secret(optarg)
                    self.jwt_secret = get_jwt_secret(optarg)
                    if self.jwt_secret == "":
                        print("secret file not found:", optarg)
                        usage(argv)
                        sys.exit(1)
                elif option in ("-k", "--jwt"):
                    self.jwt_secret = get_jwt_secret(optarg)
                    if self.jwt_secret == "":
                        print("secret file not found:", optarg)
                        usage(argv)
                        sys.exit(1)
                elif option in ("-E", "--do-not-compare-error"):
                    self.do_not_compare_error = True
                elif option in ("-j", "--json-diff"):
                    self.use_jsondiff = True
                elif option in ("-i", "--without-compare-results"):
                    if self.verify_with_daemon is True:
                        print("Error on options: "
                              "-i/--without-compare-results is not compatible with -d/--compare-erigon-rpcdaemon")
                        usage(argv)
                        sys.exit(1)
                    self.without_compare_results = True
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


def generate_jwt_secret(filename, length=64):
    """generates a  file contains 64 hex digit"""
    random_hex = "0x" + ''.join(random.choices('0123456789abcdef', k=length))
    with open(filename, 'w', encoding='utf8') as file:
        file.write(random_hex)

    print(f"Secret File '{filename}' created with success!")


def get_json_from_response(target, msg, verbose_level: int, result):
    """ Retrieve JSON from response """
    if verbose_level > 2:
        print(msg + " :[" + str(result) + "]")

    if len(result) == 0:
        error_msg = "Failed (json response is zero length, maybe server is down) on " + target
        return None, error_msg
    try:
        return result, ""
    except json.decoder.JSONDecodeError:
        error_msg = "Failed (bad json format) + target"
        if verbose_level:
            print(msg)
            print("Failed (bad json format)")
            print(result)
        return None, error_msg


def dump_jsons(dump_json, daemon_file, exp_rsp_file, output_dir, response, expected_response: str):
    """ dump jsons on result dir """
    if not dump_json:
        return

    for attempt in range(10):
        try:
            os.makedirs(output_dir, exist_ok=True)
        except OSError as e:
            print("Exception on makedirs: ", output_dir, {e})

        try:
            if daemon_file != "":
                if os.path.exists(daemon_file):
                    os.remove(daemon_file)
                with open(daemon_file, 'w', encoding='utf8') as json_file_ptr:
                    json_file_ptr.write(json.dumps(response, indent=2, sort_keys=True))
            if exp_rsp_file != "":
                if os.path.exists(exp_rsp_file):
                    os.remove(exp_rsp_file)
                with open(exp_rsp_file, 'w', encoding='utf8') as json_file_ptr:
                    json_file_ptr.write(json.dumps(expected_response, indent=2, sort_keys=True))
            break

        except OSError as e:
            print("Exception on file write: ..  ", {e}, attempt)


def execute_request(transport_type: str, jwt_auth, request_dumps, target: str, verbose_level: int):
    """ execute request on server identified by target """
    if transport_type in ("http", 'http_comp', 'https'):
        http_headers = {'content-type': 'application/json'}
        if transport_type != 'http_comp':
            http_headers['Accept-Encoding'] = 'Identity'

        if jwt_auth:
            http_headers['Authorization'] = jwt_auth

        target_url = ("https://" if transport_type == "https" else "http://") + target
        try:
            rsp = requests.post(target_url, data=request_dumps, headers=http_headers, timeout=300)
            if rsp.status_code != 200:
                if verbose_level > 1:
                    print("\npost result status_code: ", rsp.status_code)
                return ""
            if verbose_level > 1:
                print("\npost result content: ", rsp.content)
            result = rsp.json()
        except OSError as e:
            if verbose_level:
                print("\nhttp connection fail: ", target_url, e)
            return ""
    else:
        ws_target = "ws://" + target  # use websocket
        if transport_type == 'websocket_comp':
            selected_compression = 'deflate'
            curr_extensions = [
                permessage_deflate.ClientPerMessageDeflateFactory(
                    client_max_window_bits=15,
                    compress_settings={"memLevel": 4}
                ),
            ]
        else:
            selected_compression = None
            curr_extensions = None
        try:
            http_headers = {}
            if jwt_auth:
                http_headers['Authorization'] = jwt_auth
            with connect(ws_target, max_size=1000048576, compression=selected_compression,
                         extensions=curr_extensions, open_timeout=None) as websocket:
                websocket.send(request_dumps)
                rsp = websocket.recv(None)
                result = json.loads(rsp)

        except OSError as e:
            if verbose_level:
                print("\nwebsocket connection fail:", e)
            return ""

    if verbose_level > 1:
        print("\n target:", target)
        print(request_dumps)
        print("Response.len:", len(result))
        print("Response:", result)
    return result


def run_compare(use_jsondiff, temp_file1, temp_file2, diff_file, test_number):
    """ run Compare command and verify if command complete. """

    if use_jsondiff:
        cmd_result = os.system("json-diff --help > /dev/null")
        if cmd_result:
            return 0
        cmd = "json-diff -s " + temp_file2 + " " + temp_file1 + " > " + diff_file + " 2> /dev/null &"
        already_failed = False
    else:
        cmd = "diff " + temp_file2 + " " + temp_file1 + " > " + diff_file + " 2> /dev/null &"
        already_failed = True
    os.system(cmd)
    idx = 0
    while 1:
        idx += 1
        time.sleep(TIME)
        # verify if json-diff or diff in progress
        cmd = "ps aux | grep -v run_tests | grep 'diff' | grep -v 'grep' | grep test_" + str(test_number) + " | awk '{print $2}'"
        pid = os.popen(cmd).read()
        if pid == "":
            # json-diff or diff terminated
            return 1
        if idx >= MAX_TIME:
            killing_pid = pid.strip()
            # reach timeout. kill it
            cmd = "kill " + killing_pid
            # print ("kill: test_number: ", str(test_number), " cmd: " , cmd)
            os.system(cmd)
            if already_failed:
                # timeout with json-diff and diff so return timeout->0
                return 0
            already_failed = True
            # try json diffs with diff
            cmd = "diff " + temp_file2 + " " + temp_file1 + " > " + diff_file + " &"
            os.system(cmd)
            idx = 0
            continue


def compare_json(config, response, json_file, daemon_file, exp_rsp_file, diff_file: str, test_number):
    """ Compare JSON response. """
    base_name = "/tmp/test_" + str(test_number) + "/"
    if os.path.exists(base_name) == 0:
        os.makedirs(base_name, exist_ok=True)
    temp_file1 = base_name + "daemon_lower_case.txt"
    temp_file2 = base_name + "rpc_lower_case.txt"

    if "error" in response:
        to_lower_case(daemon_file, temp_file1)
        to_lower_case(exp_rsp_file, temp_file2)
    else:
        cmd = "cp " + daemon_file + " " + temp_file1
        os.system(cmd)
        cmd = "cp " + exp_rsp_file + " " + temp_file2
        os.system(cmd)

    if is_not_compared_message(json_file, config.net):
        removed_line_string = "message"
        replace_message(exp_rsp_file, temp_file1, removed_line_string)
        replace_message(daemon_file, temp_file2, removed_line_string)
    elif is_not_compared_error(json_file, config.net):
        removed_line_string = "error"
        replace_message(exp_rsp_file, temp_file1, removed_line_string)
        replace_message(daemon_file, temp_file2, removed_line_string)

    diff_result = run_compare(config.use_jsondiff, temp_file1, temp_file2, diff_file, test_number)
    diff_file_size = 0
    return_code = 1  # ok
    error_msg = ""
    if diff_result == 1:
        diff_file_size = os.stat(diff_file).st_size
    if diff_file_size != 0 or diff_result == 0:
        if diff_result == 0:
            error_msg = "Failed Timeout"
        else:
            error_msg = "Failed"
        return_code = 0  # failed

    if os.path.exists(temp_file1):
        os.remove(temp_file1)
    if os.path.exists(temp_file2):
        os.remove(temp_file2)
    return return_code, error_msg


def process_response(target, target1, result, result1: str, response_in_file, config,
                     output_dir: str, daemon_file: str, exp_rsp_file: str, diff_file: str, json_file: str, test_number: int):
    """ Process the response If exact result or error don't care, they are null but present in expected_response. """

    response, error_msg = get_json_from_response(target, config.daemon_under_test, config.verbose_level, result)
    if response is None:
        return 0, error_msg

    if result1 != "":
        expected_response, error_msg = get_json_from_response(target1, config.daemon_as_reference, config.verbose_level, result1)
        if expected_response is None:
            return 0, error_msg
    else:
        expected_response = response_in_file

    if config.without_compare_results is True:
        dump_jsons(config.force_dump_jsons, daemon_file, exp_rsp_file, output_dir, response, expected_response)
        return 1, ""

    if response is None:
        return 0, "Failed [" + config.daemon_under_test + "] (server doesn't response)"

    if expected_response is None:
        return 0, "Failed [" + config.daemon_as_reference + "] (server doesn't response)"

    if response != expected_response:
        if "result" in response and "result" in expected_response and expected_response["result"] is None and result1 == "":
            # response and expected_response are different but don't care
            dump_jsons(config.force_dump_jsons, daemon_file, exp_rsp_file, output_dir, response, expected_response)
            return 1, ""
        if "error" in response and "error" in expected_response and expected_response["error"] is None:
            # response and expected_response are different but don't care
            dump_jsons(config.force_dump_jsons, daemon_file, exp_rsp_file, output_dir, response, expected_response)
            return 1, ""
        if "error" not in expected_response and "result" not in expected_response:
            # response and expected_response are different but don't care
            dump_jsons(config.force_dump_jsons, daemon_file, exp_rsp_file, output_dir, response, expected_response)
            return 1, ""
        if "error" in response and "error" in expected_response and config.do_not_compare_error:
            dump_jsons(config.force_dump_jsons, daemon_file, exp_rsp_file, output_dir, response, expected_response)
            return 1, ""
        dump_jsons(True, daemon_file, exp_rsp_file, output_dir, response, expected_response)

        same, error_msg = compare_json(config, response, json_file, daemon_file, exp_rsp_file, diff_file, test_number)
        # cleanup
        if same:
            os.remove(daemon_file)
            os.remove(exp_rsp_file)
            os.remove(diff_file)
        if not os.listdir(output_dir):
            try:
                os.rmdir(output_dir)
            except OSError:
                pass

        return same, error_msg

    dump_jsons(config.force_dump_jsons, daemon_file, exp_rsp_file, output_dir, response, expected_response)
    return 1, ""


def run_test(json_file: str, test_number, transport_type, config):
    """ Run integration tests. """
    json_filename = config.json_dir + json_file
    ext = os.path.splitext(json_file)[1]

    if ext in (".zip", ".tar"):
        with tarfile.open(json_filename, encoding='utf-8') as tar:
            files = tar.getmembers()
            if len(files) != 1:
                return 0, "bad archive file " + json_filename
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
        target = get_target(config.daemon_under_test, method, config)
        target1 = ""
        if config.jwt_secret == "":
            jwt_auth = ""
        else:
            byte_array_secret = bytes.fromhex(config.jwt_secret)
            encoded = jwt.encode({"iat": datetime.now(pytz.utc)}, byte_array_secret, algorithm="HS256")
            jwt_auth = "Bearer " + str(encoded)
        if config.verify_with_daemon is False:  # compare daemon result with file
            result = execute_request(transport_type, jwt_auth, request_dumps, target, config.verbose_level)
            result1 = ""
            response_in_file = json_rpc["response"]

            output_api_filename = config.output_dir + os.path.splitext(json_file)[0]
            output_dir_name = output_api_filename[:output_api_filename.rfind("/")]
            diff_file = output_api_filename + "-diff.json"

            daemon_file = output_api_filename + "response.json"
            exp_rsp_file = output_api_filename + "expResponse.json"

        else:  # run tests with two servers
            target = get_target(DAEMON_ON_DEFAULT_PORT, method, config)
            result = execute_request(transport_type, jwt_auth, request_dumps, target, config.verbose_level)
            target1 = get_target(config.daemon_as_reference, method, config)
            result1 = execute_request(transport_type, jwt_auth, request_dumps, target1, config.verbose_level)
            response_in_file = None

            output_api_filename = config.output_dir + os.path.splitext(json_file)[0]
            output_dir_name = output_api_filename[:output_api_filename.rfind("/")]
            diff_file = output_api_filename + "-diff.json"

            daemon_file = output_api_filename + get_json_filename_ext(DAEMON_ON_DEFAULT_PORT, target)
            exp_rsp_file = output_api_filename + get_json_filename_ext(config.daemon_as_reference, target1)

        return process_response(
            target,
            target1,
            result,
            result1,
            response_in_file,
            config,
            output_dir_name,
            daemon_file,
            exp_rsp_file,
            diff_file,
            json_file,
            test_number)


def extract_number(filename):
    """
    Extract test number from filename
    """
    match = re.search(r'\d+', filename) # Look for one or more digits
    if match:
        return int(match.group())
    return 0 # Or some other default value, like float('inf') if you want them at the end
             # Or even just the filename itself if you want alphabetical sort for non-numeric names


def check_test_name_for_number(test_name, req_test_number):
    """
    Verify that string test_name contains the number req_test_number,
    do not consider the initial zero 
    """
    if req_test_number == -1:
        return True
    pattern = r"_" + r"0*" + str(req_test_number) + r"($|[^0-9])"

    if re.search(pattern, test_name):
        return True
    return False

#
# main
#
def main(argv) -> int:
    """ Run integration tests. """
    config = Config()
    config.select_user_options(argv)

    start_time = datetime.now()
    os.makedirs(config.output_dir, exist_ok=True)
    executed_tests = 0
    failed_tests = 0
    success_tests = 0
    tests_not_executed = 0

    if config.verify_with_daemon is True:
        if config.daemon_as_reference == EXTERNAL_PROVIDER:
            server_endpoints = "both servers (rpcdaemon with " + config.external_provider_url + ")"
        else:
            server_endpoints = "both servers (rpcdaemon with " + config.daemon_under_test + ")"
    else:
        target = get_target(config.daemon_under_test, "eth_call", config)
        target1 = get_target(config.daemon_under_test, "engine_", config)
        server_endpoints = target + "/" + target1
    if config.parallel is True:
        print("Run tests in parallel on", server_endpoints)
        exe = ProcessPoolExecutor()
    else:
        print("Run tests in serial on", server_endpoints)
        exe = ProcessPoolExecutor(max_workers=1)

    if config.transport_type in ('http_comp', 'websocket_comp'):
        print("Run tests using compression")

    global_test_number = 0
    available_tested_apis = 0
    test_rep = 0
    try:
        for test_rep in range(0, config.loop_number):  # makes tests more times
            if config.loop_number != 1:
                print(
                    "\r                                                                                                             ",
                    end='', flush=True)
                print("\nTest iteration: ", test_rep + 1,
                      "                                                                      ")
            tokenize_transport_type = config.transport_type.split(",")
            for transport_type in tokenize_transport_type:
                test_number_in_any_loop = 1
                tests_descr_list = []
                dirs = sorted(os.listdir(config.json_dir))
                global_test_number = 0
                available_tested_apis = 0
                for curr_api in dirs:  # scans all api present in dir
                    # jump results folder or any hidden OS-specific folder
                    if curr_api == config.results_dir or curr_api.startswith("."):
                        continue
                    test_dir = config.json_dir + curr_api
                    if not os.path.isdir(test_dir):  # jump if not dir
                        continue
                    available_tested_apis = available_tested_apis + 1
                    test_lists = sorted(os.listdir(test_dir), key=extract_number)
                    test_number = 1
                    for test_name in test_lists:  # scan all json test present in the dir
                        if not test_name.startswith("test_"):
                            continue
                        if not test_name.endswith((".zip", ".gzip", ".json", ".tar")):
                            continue
                        json_test_full_name = curr_api + "/" + test_name
                        if api_under_test(curr_api, json_test_full_name, config):  # -a/-A or any api
                            if is_skipped(curr_api, json_test_full_name, test_number_in_any_loop, config) == 1:
                                if config.start_test == "" or test_number_in_any_loop >= int(config.start_test):
                                    if config.display_only_fail is False and config.req_test_number != "":
                                        file = json_test_full_name.ljust(60)
                                        curr_tt = transport_type.ljust(15)
                                        print(f"{test_number_in_any_loop:04d}. {curr_tt}::{file} Skipped")
                                    tests_not_executed = tests_not_executed + 1
                            else:
                                # runs all tests or
                                # runs single global test
                                # runs only tests a specific test_number in the testing_apis list
                                if ((config.testing_apis_with == "" and config.testing_apis == "" and config.req_test_number in (-1, test_number_in_any_loop)) or
                                    (config.testing_apis_with != "" and check_test_name_for_number(test_name, config.req_test_number)) or
                                    (config.testing_apis != "" and check_test_name_for_number(test_name, config.req_test_number))):
                                    if (config.start_test == "" or  # start from specific test
                                            (config.start_test != "" and test_number_in_any_loop >= int(config.start_test))):
                                        # create process pool
                                        try:
                                            future = exe.submit(run_test, json_test_full_name, test_number_in_any_loop,
                                                                transport_type, config)
                                            tests_descr_list.append(
                                                {'name': json_test_full_name, 'number': test_number_in_any_loop,
                                                 'transport-type': transport_type, 'future': future})
                                            if config.waiting_time:
                                                time.sleep(config.waiting_time / 1000)
                                            executed_tests = executed_tests + 1
                                        except Exception as e:
                                            print(f"An error occurred: {e}")
                                            return 100

                        global_test_number = global_test_number + 1
                        test_number_in_any_loop = test_number_in_any_loop + 1
                        test_number = test_number + 1

                # when all tests on specific transport type are spawned
                if executed_tests == 0:
                    print("ERROR: api-name or testNumber not found")
                    return 1

                # waits the future to check tests results
                cancel = False
                for test in tests_descr_list:
                    curr_json_test_full_name = test['name']
                    curr_test_number_in_any_loop = test['number']
                    curr_transport_type = test['transport-type']
                    curr_future = test['future']
                    file = curr_json_test_full_name.ljust(60)
                    curr_tt = curr_transport_type.ljust(15)
                    if cancel:
                        curr_future.cancel()
                        continue
                    print(f"{curr_test_number_in_any_loop:04d}. {curr_tt}::{file}   ", end='', flush=True)
                    result, error_msg = curr_future.result()
                    if result == 1:
                        success_tests = success_tests + 1
                        if config.verbose_level:
                            print("OK                   ", flush=True)
                        else:
                            print("OK                   \r", end='', flush=True)
                    else:
                        failed_tests = failed_tests + 1
                        print(error_msg, "\r")
                        if config.exit_on_fail:
                            cancel = True
            if config.exit_on_fail and failed_tests:
                print("TEST ABORTED!")
                break
    except KeyboardInterrupt:
        print("TEST INTERRUPTED!")

    # print results at the end of all the tests
    elapsed = datetime.now() - start_time
    print("                                                                                                                  \r")
    print(f"Test time-elapsed:            {str(elapsed)}")
    print(f"Available tests:              {global_test_number - 1}")
    print(f"Available tested api:         {available_tested_apis}")
    print(f"Number of loop:               {test_rep + 1}")
    print(f"Number of executed tests:     {executed_tests}")
    print(f"Number of NOT executed tests: {tests_not_executed}")
    print(f"Number of success tests:      {success_tests}")
    print(f"Number of failed tests:       {failed_tests}")

    return failed_tests


#
# module as main
#
if __name__ == "__main__":
    sys.exit(main(sys.argv))
