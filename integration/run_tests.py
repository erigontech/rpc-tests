#!/usr/bin/python3
""" Run the JSON RPC API curl commands as integration tests """

from datetime import datetime
import getopt
import gzip
import json
import os
import shlex
import shutil
import subprocess
import sys
import tarfile
import time
import pytz
import jwt

SILK = "silk"
RPCDAEMON = "rpcdaemon"
INFURA = "infura"

tests_with_big_json = [
]

api_not_compared = [
    "goerly/trace_rawTransaction", # erigon does not support raw tx but hash of tx
    "goerly/parity_getBlockReceipts", # not supported by rpcdaemon
    "goerly/erigon_watchTheBurn", # not supported by rpcdaemon
    "goerly/erigon_cumulativeChainTraffic", # not supported by rpcdaemon
    "goerly/engine_exchangeCapabilities", # not supported by silkrpc removed from ethbackend i/f
    "goerly/engine_forkchoiceUpdatedV1", # not supported by silkrpc removed from ethbackend i/f
    "goerly/engine_forkchoiceUpdatedV2", # not supported by silkrpc removed from ethbackend i/f
    "goerly/engine_getPayloadBodiesByHashV1", # not supported by silkrpc removed from ethbackend i/f
    "goerly/engine_getPayloadBodiesByRangeV1", # not supported by silkrpc removed from ethbackend i/f
    "goerly/engine_getPayloadV1", # not supported by silkrpc removed from ethbackend i/f
    "goerly/engine_getPayloadV2", # not supported by silkrpc removed from ethbackend i/f
    "goerly/engine_newPayloadV1", # not supported by silkrpc removed from ethbackend i/f
    "goerly/engine_newPayloadV2" # not supported by silkrpc removed from ethbackend i/f
]

tests_not_compared = [
    "goerly/debug_accountAt/test_06.json", # debug
    "goerly/debug_accountAt/test_07.json", # debug
    "goerly/debug_accountAt/test_11.json", # debug
    "goerly/debug_traceBlockByHash/test_02.tar", # diff on gasCost
    "goerly/debug_traceBlockByHash/test_03.tar", # diff on gasCost
    "goerly/debug_traceBlockByHash/test_04.tar", # diff on gasCost

    "goerly/debug_traceBlockByNumber/test_02.tar", # diff on gasCost
    "goerly/debug_traceBlockByNumber/test_09.tar", # diff on gasCost
    "goerly/debug_traceBlockByNumber/test_10.tar", # diff on gasCost
    "goerly/debug_traceBlockByNumber/test_11.tar", # diff on gasCost
    "goerly/debug_traceBlockByNumber/test_12.tar", # diff on gasCost
    "goerly/debug_traceBlockByNumber/test_14.tar", # diff on gasCost

    "goerly/trace_replayBlockTransactions/test_01.tar",  # diff on gasCost
    "goerly/trace_replayBlockTransactions/test_02.tar",  # diff on gasCost

    "goerly/trace_replayTransaction/test_16.tar",  # diff on gasCost
    "goerly/trace_replayTransaction/test_23.tar",  # diff on gasCost

    "goerly/debug_traceCall/test_10.json", # diff on gasCost
    "goerly/debug_traceCall/test_14.json", # diff on gasCost
    "goerly/debug_traceCall/test_17.json", # diff on gasCost
    "goerly/eth_callMany/test_01.json", # debug bad value format
    "goerly/eth_callMany/test_02.json", # debug bad value format
    "goerly/eth_callMany/test_05.json", # debug bad error format
    "goerly/eth_callMany/test_06.json", # debug bad error format
    "goerly/eth_callMany/test_09.json", # debug silkrpc return ok rpcdaemon error
    "goerly/eth_callMany/test_10.json", # debug silkrpc return ok rpcdaemon error
    "goerly/eth_feeHistory/test_1.json", # debug values are different

    "mainnet/debug_accountAt/test_02.json", # to be debugged
    "mainnet/debug_accountAt/test_05.json", # to be debugged
    "mainnet/debug_accountAt/test_07.json", # to be debugged
    "mainnet/debug_accountAt/test_08.json", # to be debugged

    "mainnet/debug_traceBlockByNumber/test_05.tar", # json too big
    "mainnet/debug_traceBlockByNumber/test_06.tar", # json too big
    "mainnet/debug_traceBlockByNumber/test_08.tar", # json too big
    "mainnet/debug_traceBlockByNumber/test_09.tar", # json too big
    "mainnet/debug_traceBlockByNumber/test_10.tar", # json too big
    "mainnet/debug_traceBlockByNumber/test_11.tar", # json too big
    "mainnet/debug_traceBlockByNumber/test_12.tar", # json too big

    "mainnet/debug_traceBlockByHash/test_03.tar", # diff on gasCost
]

tests_not_compared_result = [
    "goerly/trace_call/test_04.json", # error message different invalidOpcode vs badInstructions
    "goerly/trace_call/test_11.json", # error message different invalidOpcode vs badInstructions
    "goerly/trace_call/test_15.json", # error message different invalidOpcode vs badInstructions
    "goerly/trace_call/test_17.json", # error message different invalidOpcode vs badInstructions
    "goerly/trace_callMany/test_04.json", # error message different invalidOpcode vs badInstructions
    "goerly/eth_callMany/test_04.json", # error message different invalidOpcode vs badInstructions
    "goerly/trace_callMany/test_05.json", # error message different invalidOpcode vs badInstructions
    "goerly/trace_callMany/test_13.json", # error message different invalidOpcode vs badInstructions
    "goerly/trace_callMany/test_14.tar", # error message different invalidOpcode vs badInstructions
    "goerly/trace_callMany/test_15.json" # error message different invalidOpcode vs badInstructions
]

tests_not_compared_message = [
    "goerly/trace_callMany/test_10.json", # silkrpc message contains also address
    "goerly/trace_callMany/test_11.json", # silkrpc message contains also address
    "goerly/eth_callMany/test_08.json",  # silkrpc message contains few chars
    "goerly/trace_call/test_12.json", # silkrpc message contains also address
    "goerly/trace_call/test_16.json" # silkrpc message contains also address
]

tests_message_lower_case = [
]



def get_target(target_type: str, method: str, infura_url: str, host: str, port: int = 0):
    """ determine target
    """
    if "engine_" in method and target_type == SILK:
        return host + ":" + str(port if port > 0 else 51516)

    if "engine_" in method and target_type == RPCDAEMON:
        return host + ":" + str(port if port > 0 else 8551)

    if target_type == SILK:
        return host + ":" + str(port if port > 0 else 51515)

    if target_type == INFURA:
        return infura_url

    return host + ":" + str(port if port > 0 else 8545)


def get_json_filename_ext(target_type: str):
    """ determine json file name
    """
    if target_type == SILK:
        return "-silk.json"
    if target_type == INFURA:
        return "-infura.json"
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


def is_skipped(api_name, net, exclude_api_list, exclude_test_list, api_file: str, req_test, verify_with_daemon,
               global_test_number):
    """ determine if test must be skipped
    """
    api_full_name = net + "/" + api_name
    api_full_test_name = net + "/" + api_file
    if req_test == -1 and verify_with_daemon == 1:
        for curr_test_name in api_not_compared:
            if curr_test_name == api_full_name:
                return 1
    if req_test == -1 and verify_with_daemon == 1:
        for curr_test in tests_not_compared:
            if curr_test == api_full_test_name:
                return 1
    if exclude_api_list != "":  # scans exclude api list (-x)
        tokenize_exclude_api_list = exclude_api_list.split(",")
        for exclude_api in tokenize_exclude_api_list:
            if exclude_api in api_full_name:
                return 1
    if exclude_test_list != "":  # scans exclude test list (-X)
        tokenize_exclude_test_list = exclude_test_list.split(",")
        for exclude_test in tokenize_exclude_test_list:
            if exclude_test == str(global_test_number):
                return 1
    return 0

def is_testing_apis(api_name, requested_apis: str):
    """ determine if api_name is in requested_apis
    """
    if requested_apis == "":
        return 1
    tokenize_list = requested_apis.split(",")
    for test in tokenize_list:
        if test in api_name:
            return 1
    return 0

def is_big_json(test_name, net: str,):
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
    """ determine if test not compared result
    """
    test_full_name = net + "/" + test_name
    for curr_test_name in tests_not_compared_message:
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


def run_shell_command(net: str, command: str, command1: str, expected_response: str, verbose_level: int, exit_on_fail: bool,
                      output_dir: str, silk_file: str,
                      exp_rsp_file: str, diff_file: str, dump_output, json_file: str, test_number):
    """ Run the specified command as shell. If exact result or error don't care, they are null but present in expected_response. """

    command_and_args = shlex.split(command)
    process = subprocess.run(command_and_args, stdout=subprocess.PIPE, universal_newlines=True, check=True)
    if process.returncode != 0:
        sys.exit(process.returncode)
    process.stdout = process.stdout.strip('\n')
    if verbose_level > 1:
        print(process.stdout)
    response = json.loads(process.stdout)
    if command1 != "":
        command_and_args = shlex.split(command1)
        process = subprocess.run(command_and_args, stdout=subprocess.PIPE, universal_newlines=True, check=True)
        if process.returncode != 0:
            sys.exit(process.returncode)
        process.stdout = process.stdout.strip('\n')
        try:
            expected_response = json.loads(process.stdout)
        except json.decoder.JSONDecodeError:
            if verbose_level:
                print("Failed (bad json format on expected rsp)")
                print(process.stdout)
                return 1
            file = json_file.ljust(60)
            print(f"{test_number:03d}. {file} Failed (bad json format on expected rsp)")
            if exit_on_fail:
                print("TEST ABORTED!")
                sys.exit(1)
            return 1

    if response != expected_response:
        if "result" in response and "result" in expected_response and expected_response["result"] is None:
            # response and expected_response are different but don't care
            if verbose_level:
                print("OK")
            if dump_output:
                if silk_file != "" and os.path.exists(output_dir) == 0:
                    os.mkdir(output_dir)
                if silk_file != "":
                    with open(silk_file, 'w', encoding='utf8') as json_file_ptr:
                        json_file_ptr.write(json.dumps(response, indent=6, sort_keys=True))
                if exp_rsp_file != "":
                    with open(exp_rsp_file, 'w', encoding='utf8') as json_file_ptr:
                        json_file_ptr.write(json.dumps(expected_response, indent=5, sort_keys=True))
            return 0
        if "error" in response and "error" in expected_response and expected_response["error"] is None:
            # response and expected_response are different but don't care
            if verbose_level:
                print("OK")
            if dump_output:
                if silk_file != "" and os.path.exists(output_dir) == 0:
                    os.mkdir(output_dir)
                if silk_file != "":
                    with open(silk_file, 'w', encoding='utf8') as json_file_ptr:
                        json_file_ptr.write(json.dumps(response, indent=6, sort_keys=True))
                if exp_rsp_file != "":
                    with open(exp_rsp_file, 'w', encoding='utf8') as json_file_ptr:
                        json_file_ptr.write(json.dumps(expected_response, indent=5, sort_keys=True))
            return 0
        if "error" not in expected_response and "result" not in expected_response:
            # response and expected_response are different but don't care
            if verbose_level:
                print("OK")
            if dump_output:
                if silk_file != "" and os.path.exists(output_dir) == 0:
                    os.mkdir(output_dir)
                if silk_file != "":
                    with open(silk_file, 'w', encoding='utf8') as json_file_ptr:
                        json_file_ptr.write(json.dumps(response, indent=6, sort_keys=True))
                if exp_rsp_file != "":
                    with open(exp_rsp_file, 'w', encoding='utf8') as json_file_ptr:
                        json_file_ptr.write(json.dumps(expected_response, indent=5, sort_keys=True))
            return 0
        if silk_file != "" and os.path.exists(output_dir) == 0:
            os.mkdir(output_dir)
        if silk_file != "":
            with open(silk_file, 'w', encoding='utf8') as json_file_ptr:
                json_file_ptr.write(json.dumps(response, indent=5, sort_keys=True))
        if exp_rsp_file != "":
            with open(exp_rsp_file, 'w', encoding='utf8') as json_file_ptr:
                json_file_ptr.write(json.dumps(expected_response, indent=5, sort_keys=True))

        temp_file1 = "/tmp/silk_lower_case"
        temp_file2 = "/tmp/rpc_lower_case"

        if "error" in response:
            to_lower_case(exp_rsp_file, temp_file2)
            to_lower_case(silk_file, temp_file1)
        else:
            cmd = "cp " +  silk_file  + " " + temp_file1
            os.system(cmd)
            cmd = "cp " +  exp_rsp_file  + " " + temp_file2
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
        elif is_message_to_be_converted(json_file, net):
            modified_string = "message"
            modified_str_from_file(exp_rsp_file, temp_file1, modified_string)
            modified_str_from_file(silk_file, temp_file2, modified_string)
            cmd = "json-diff -s " + temp_file2 + " " + temp_file1 + " > " + diff_file
        elif is_big_json(json_file, net):
            cmd = "json-patch-jsondiff --indent 4 " + temp_file2 + " " + temp_file1 + " > " + diff_file
        else:
            cmd = "json-diff -s " + temp_file2 + " " + temp_file1 + " > " + diff_file
        os.system(cmd)
        diff_file_size = os.stat(diff_file).st_size
        if diff_file_size != 0:
            if verbose_level:
                print("Failed")
            else:
                file = json_file.ljust(60)
                print(f"{test_number:03d}. {file} Failed")
            if exit_on_fail:
                print("TEST ABORTED!")
                sys.exit(1)
            return 1
        if verbose_level:
            print("OK")
        if os.path.exists(temp_file1):
            os.remove(temp_file1)
        if os.path.exists(temp_file2):
            os.remove(temp_file2)
        os.remove(silk_file)
        os.remove(exp_rsp_file)
        os.remove(diff_file)
        if not os.listdir(output_dir):
            os.rmdir(output_dir)
    else:
        if verbose_level:
            print("OK")

    if dump_output:
        if silk_file != "" and os.path.exists(output_dir) == 0:
            os.mkdir(output_dir)
        if silk_file != "":
            with open(silk_file, 'w', encoding='utf8') as json_file_ptr:
                json_file_ptr.write(json.dumps(response, indent=6, sort_keys=True))
        if exp_rsp_file != "":
            with open(exp_rsp_file, 'w', encoding='utf8') as json_file_ptr:
                json_file_ptr.write(json.dumps(expected_response, indent=5, sort_keys=True))
    return 0


def run_tests(net: str, test_dir: str, output_dir: str, json_file: str, verbose_level: int, daemon_under_test: str, exit_on_fail: bool,
              verify_with_daemon: bool, daemon_as_reference: str,
              dump_output: bool, test_number, infura_url: str, daemon_on_host: str, daemon_on_port: int,
              jwt_secret: str):
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
    elif ext in (".gzip"):
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
        target = get_target(daemon_under_test, method, infura_url, daemon_on_host, daemon_on_port)
        if jwt_secret == "":
            jwt_auth = ""
        else:
            byte_array_secret = bytes.fromhex(jwt_secret)
            encoded = jwt.encode({"iat": datetime.now(pytz.utc)}, byte_array_secret, algorithm="HS256")
            jwt_auth = "-H \"Authorization: Bearer " + str(encoded) + "\" "
        if verify_with_daemon == 0:
            cmd = '''curl --silent -X POST -H "Content-Type: application/json" ''' + jwt_auth + ''' --data \'''' + request_dumps + '''\' ''' + target
            cmd1 = ""
            output_api_filename = output_dir + json_file[:-4]
            output_dir_name = output_api_filename[:output_api_filename.rfind("/")]
            response = json_rpc["response"]
            silk_file = output_api_filename + "-response.json"
            exp_rsp_file = output_api_filename + "-expResponse.json"
            diff_file = output_api_filename + "-diff.json"
        else:
            target = get_target(SILK, method, infura_url, daemon_on_host, daemon_on_port)
            target1 = get_target(daemon_as_reference, method, infura_url, daemon_on_host, daemon_on_port)
            cmd = '''curl --silent -X POST -H "Content-Type: application/json" ''' + jwt_auth + ''' --data \'''' + request_dumps + '''\' ''' + target
            cmd1 = '''curl --silent -X POST -H "Content-Type: application/json" ''' + jwt_auth + ''' --data \'''' + request_dumps + '''\' ''' + target1
            output_api_filename = output_dir + json_file[:-4]
            output_dir_name = output_api_filename[:output_api_filename.rfind("/")]
            response = ""
            silk_file = output_api_filename + get_json_filename_ext(SILK)
            exp_rsp_file = output_api_filename + get_json_filename_ext(daemon_as_reference)
            diff_file = output_api_filename + "-diff.json"

        return run_shell_command(
            net,
            cmd,
            cmd1,
            response,
            verbose_level,
            exit_on_fail,
            output_dir_name,
            silk_file,
            exp_rsp_file,
            diff_file,
            dump_output,
            json_file,
            test_number)


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
    print("-h print this help")
    print("-f shows only failed tests (not Skipped)")
    print("-c runs all tests even if one test fails [default: exit at first test fail]")
    print("-r connect to Erigon RpcDaemon [default: connect to Silkrpc] ")
    print("-l <number of loops>")
    print("-a <test_apis>: run all tests of the specified API (e.g.: eth_call,eth_getLogs,debug_)")
    print("-s <start_test_number>: run tests starting from input")
    print("-t <test_number>: run single test")
    print("-d send requests also to the reference daemon e.g.: Erigon RpcDaemon")
    print("-i <infura_url> send any request also to the Infura API endpoint as reference")
    print("-b blockchain [default: goerly]")
    print("-v <verbose_level>")
    print("-o dump response")
    print("-k authentication token file")
    print("-x exclude API list (e.g.: txpool_content,txpool_status,engine_)")
    print("-X exclude test list (e.g.: 18,22)")
    print("-H host where the RpcDaemon is located (e.g.: 10.10.2.3)")
    print("-p port where the RpcDaemon is located (e.g.: 8545)")


#
# main
#
def main(argv):
    """ parse command line and execute tests
    """
    exit_on_fail = True
    daemon_under_test = SILK
    daemon_as_reference = RPCDAEMON
    loop_number = 1
    verbose_level = 0
    req_test = -1
    dump_output = False
    infura_url = ""
    daemon_on_host = "localhost"
    daemon_on_port = 0
    requested_apis = ""
    verify_with_daemon = False
    net = "goerly"
    json_dir = "./" + net + "/"
    results_dir = "results"
    output_dir = json_dir + results_dir + "/"
    exclude_api_list = ""
    exclude_test_list = ""
    start_test = ""
    jwt_secret = ""
    display_only_fail = 0

    try:
        opts, _ = getopt.getopt(argv[1:], "hfrcv:t:l:a:di:b:ox:X:H:k:s:p:")
        for option, optarg in opts:
            if option in ("-h", "--help"):
                usage(argv)
                sys.exit(-1)
            elif option == "-c":
                exit_on_fail = 0
            elif option == "-r":
                daemon_under_test = RPCDAEMON
            elif option == "-i":
                daemon_as_reference = INFURA
                infura_url = optarg
            elif option == "-H":
                daemon_on_host = optarg
            elif option == "-p":
                daemon_on_port = int(optarg)
            elif option == "-f":
                display_only_fail = 1
            elif option == "-v":
                verbose_level = int(optarg)
            elif option == "-t":
                req_test = int(optarg)
            elif option == "-s":
                start_test = int(optarg)
            elif option == "-a":
                requested_apis = optarg
            elif option == "-l":
                loop_number = int(optarg)
            elif option == "-d":
                verify_with_daemon = 1
            elif option == "-o":
                dump_output = 1
            elif option == "-b":
                net = optarg
                json_dir = "./" + net + "/"
                output_dir = json_dir + results_dir + "/"
            elif option == "-x":
                exclude_api_list = optarg
            elif option == "-X":
                exclude_test_list = optarg
            elif option == "-k":
                jwt_secret = get_jwt_secret(optarg)
                if jwt_secret == "":
                    print("secret file not found")
                    sys.exit(-1)
            else:
                usage(argv)
                sys.exit(-1)

    except getopt.GetoptError as err:
        # print help information and exit:
        print(err)
        usage(argv)
        sys.exit(-1)

    if os.path.exists(output_dir):
        shutil.rmtree(output_dir)

    start_time = time.time()
    os.mkdir(output_dir)
    match = 0
    executed_tests = 0
    failed_tests = 0
    success_tests = 0
    tests_not_executed = 0
    global_test_number = 1
    for test_rep in range(0, loop_number):
        if verbose_level:
            print("Test iteration: ", test_rep + 1)
        dirs = sorted(os.listdir(json_dir))
        for api_file in dirs:
            # jump result_dir
            if api_file == results_dir:
                continue
            test_dir = json_dir + api_file
            test_lists = sorted(os.listdir(test_dir))
            test_number = 1
            for test_name in test_lists:
                if is_testing_apis(api_file, requested_apis):  # -a
                    test_file = api_file + "/" + test_name
                    if is_skipped(api_file, net, exclude_api_list, exclude_test_list, test_file, req_test,
                                  verify_with_daemon, global_test_number) == 1:
                        if start_test == "" or global_test_number >= int(start_test):
                            if display_only_fail == 0:
                                file = test_file.ljust(60)
                                print(f"{global_test_number:03d}. {file} Skipped")
                                tests_not_executed = tests_not_executed + 1
                    else:
                        # runs all tests req_test refers global test number or
                        # runs only tests on specific api req_test refers all test on specific api
                        if ((requested_apis == "" and req_test in (-1, global_test_number)) or
                                (requested_apis != "" and req_test in (-1, test_number))):
                            if (start_test == "") or (start_test != "" and global_test_number >= int(start_test)):
                                file = test_file.ljust(60)
                                if verbose_level:
                                    print(f"{global_test_number:03d}. {file} ", end='', flush=True)
                                else:
                                    print(f"{global_test_number:03d}. {file}\r", end='', flush=True)
                                ret = run_tests(net, json_dir, output_dir, test_file, verbose_level, daemon_under_test,
                                                exit_on_fail, verify_with_daemon, daemon_as_reference,
                                                dump_output, global_test_number, infura_url, daemon_on_host,
                                                daemon_on_port, jwt_secret)
                                if ret == 0:
                                    success_tests = success_tests + 1
                                else:
                                    failed_tests = failed_tests + 1
                                executed_tests = executed_tests + 1
                                if req_test != -1 or requested_apis != "":
                                    match = 1

                global_test_number = global_test_number + 1
                test_number = test_number + 1

    if (req_test != -1 or requested_apis != "") and match == 0:
        print("ERROR: api or testNumber not found")
    else:
        end_time = time.time()
        elapsed = end_time - start_time
        print("                                                                                    \r")
        print(f"Test time-elapsed (secs):     {int(elapsed)}")
        print(f"Number of executed tests:     {executed_tests}/{global_test_number - 1}")
        print(f"Number of NOT executed tests: {tests_not_executed}")
        print(f"Number of success tests:      {success_tests}")
        print(f"Number of failed tests:       {failed_tests}")


#
# module as main
#
if __name__ == "__main__":
    main(sys.argv)
    sys.exit(0)
