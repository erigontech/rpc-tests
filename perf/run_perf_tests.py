#!/usr/bin/env python3
""" This script uses Vegeta to execute a list of performance tests (configured via command line) and saves its result in CSV file
"""

# pylint: disable=consider-using-with

import json
import os
import csv
import pathlib
import sys
import time
import getopt
import getpass
from datetime import datetime
from random import randint

import psutil

DEFAULT_TEST_SEQUENCE = "50:30,1000:30,2500:20,10000:20"
DEFAULT_REPETITIONS = 10
DEFAULT_VEGETA_PATTERN_TAR_FILE = ""
DEFAULT_DAEMON_VEGETA_ON_CORE = "-:-"
DEFAULT_ERIGON_BUILD_DIR = ""
DEFAULT_SILKWORM_BUILD_DIR = ""
DEFAULT_ERIGON_ADDRESS = "localhost"
DEFAULT_TEST_MODE = "3"
DEFAULT_WAITING_TIME = 5
DEFAULT_MAX_CONN = "9000"
DEFAULT_TEST_TYPE = "eth_getLogs"
DEFAULT_VEGETA_RESPONSE_TIMEOUT = "300"
DEFAULT_MAX_BODY_RSP = "1500"

SILKWORM = "silkworm"
ERIGON = "rpcdaemon"
BINARY = "bin"
ERIGON_RPC_SERVER_NAME = "rpcdaemon"
RAND_NUM = randint(0, 100000)
RUN_TEST_DIRNAME = "/tmp/run_tests_" + str(RAND_NUM)
VEGETA_PATTERN_DIRNAME = RUN_TEST_DIRNAME + "/erigon_stress_test"
VEGETA_REPORT = RUN_TEST_DIRNAME + "/vegeta_report.hrd"
VEGETA_TAR_FILE_NAME = RUN_TEST_DIRNAME + "/vegeta_TAR_File"
VEGETA_PATTERN_SILKWORM_BASE = VEGETA_PATTERN_DIRNAME + "/vegeta_geth_"
VEGETA_PATTERN_ERIGON_BASE = VEGETA_PATTERN_DIRNAME + "/vegeta_erigon_"


def usage(argv):
    """ Print script usage """
    print("Usage: " + argv[0] + " [options]")
    print("")
    print("Launch an automated performance test sequence on Silkrpc and RPCDaemon using Vegeta")
    print("")
    print("-h,--help:                            print this help")
    print("-Z,--not-verify-server-alive:         doesn't verify server is still active")
    print("-R,--tmp-test-report:                 generate Report on tmp")
    print("-u,--test-report:                     generate Report in reports area ready to be inserted into Git repo")
    print("-v,--verbose:                         verbose")
    print("-x,--tracing:                         verbose and tracing")
    print("-e,--empty-cache:                     empty cache")
    print("-C,--max-connections <conn>:                                                                             [default: " + DEFAULT_MAX_CONN + "]")
    print("-D,--testing-daemon <string>:         name of testing daemon")
    print("-b,--blockchain <chain name>:         mandatory in case of -R or -u")
    print("-y,--test-type <test-type>:           eth_call, eth_getLogs, ...                                         [default: " + DEFAULT_TEST_TYPE + "]")
    print("-m,--test-mode <0,1,2>:               silkworm(1), erigon(2), both(3)                                    [default: " + str(DEFAULT_TEST_MODE) + "]")
    print("-p,--pattern-file <file-name>:        path to the request file for Vegeta attack                         [default: " + DEFAULT_VEGETA_PATTERN_TAR_FILE + "]")
    print("-r,--repetitions <number>:            number of repetitions for each element in test sequence (e.g. 10)  [default: " + str(DEFAULT_REPETITIONS) + "]")
    print("-t,--test-sequence <seq>:             list of qps/time as <qps1>:<t1>,... (e.g. 200:30,400:10)           [default: " + DEFAULT_TEST_SEQUENCE + "]")
    print("-w,--wait-after-test-sequence <secs>: time interval between successive test iterations in sec            [default: " + str(DEFAULT_WAITING_TIME) + "]")
    print("-d,--rpc-daemon-address <addr>:       address of RPCDaemon (e.g. 192.2.3.1)                              [default: " + DEFAULT_ERIGON_ADDRESS + "]")
    print("-g,--erigon-dir <path>:               path to erigon folder (e.g. /home/erigon)                          [default: " + DEFAULT_ERIGON_BUILD_DIR + "]")
    print("-s,--silk-dir <path>:                 path to silk folder (e.g. /home/silkworm)                          [default: " + DEFAULT_SILKWORM_BUILD_DIR + "]")
    print("-c,--run-vegeta-on-core <...>         taskset format for Vegeta (e.g. 0-1:2-3 or 0-2:3-4)                [default: " + DEFAULT_DAEMON_VEGETA_ON_CORE + "]")
    print("-T,--response-timeout <timeout>:      Vegeta response timeout                                            [default: " + DEFAULT_VEGETA_RESPONSE_TIMEOUT + "]")
    print("-M,--max-body-rsp <size>:             max number of bytes to read from response bodies                   [default: " + DEFAULT_MAX_BODY_RSP + "]")
    print("-j,--json-report <file-name>:         generate json report")
    print("-P,--more-percentiles:                print more percentiles in console report")
    print("-H,--halt-on-vegeta-error:            consider test failed if Vegeta reports any error")
    print("-I,--instant-report:                  print instant Vegeta report in console for each executed test case")
    sys.exit(1)


def get_process(process_name: str):
    """ Return the running process having specified name or None if not exists """
    for proc in psutil.process_iter():
        if proc.name() == process_name:
            return proc
    return None


class Config:
    # pylint: disable=too-many-instance-attributes
    """ This class manage configuration params """

    def __init__(self, argv):
        """ Processes the command line contained in argv """
        self.vegeta_pattern_tar_file = DEFAULT_VEGETA_PATTERN_TAR_FILE
        self.daemon_vegeta_on_core = DEFAULT_DAEMON_VEGETA_ON_CORE
        self.erigon_dir = DEFAULT_ERIGON_BUILD_DIR
        self.silkworm_dir = DEFAULT_SILKWORM_BUILD_DIR
        self.repetitions = DEFAULT_REPETITIONS
        self.test_sequence = DEFAULT_TEST_SEQUENCE
        self.rpc_daemon_address = DEFAULT_ERIGON_ADDRESS
        self.test_mode = DEFAULT_TEST_MODE
        self.test_type = DEFAULT_TEST_TYPE
        self.testing_daemon = ""
        self.waiting_time = DEFAULT_WAITING_TIME
        self.versioned_test_report = False
        self.verbose = False
        self.mac_connection = False
        self.check_server_alive = True
        self.tracing = False
        self.empty_cache = False
        self.create_test_report = False
        self.max_connection = DEFAULT_MAX_CONN
        self.vegeta_response_timeout = DEFAULT_VEGETA_RESPONSE_TIMEOUT
        self.max_body_rsp = DEFAULT_MAX_BODY_RSP
        self.json_report_file = ""
        self.binary_file_full_pathname = ""
        self.binary_file = ""
        self.chain_name = "mainnet"
        self.more_percentiles = False
        self.instant_report = False
        self.halt_on_vegeta_error = False

        self.__parse_args(argv)

    def validate_input(self):
        """ This method validate user input """
        if self.json_report_file != "" and self.test_mode == "3":
            print("ERROR: incompatible option -j/--json-report with -m/--test-mode")
            return 0

        if self.test_mode == "3" and self.testing_daemon != "":
            print("ERROR: incompatible option -m/--test-mode and -D/--testing-daemon")
            return 0

        if self.json_report_file != "" and self.testing_daemon == "":
            print("ERROR: with option -j/--json-report must be set also -D/--testing_daemon")
            return 0

        if (self.erigon_dir != DEFAULT_ERIGON_BUILD_DIR or self.silkworm_dir != DEFAULT_SILKWORM_BUILD_DIR) and self.rpc_daemon_address != DEFAULT_ERIGON_ADDRESS:
            print("ERROR: incompatible option -d/rpc-daemon-address with -g/erigon-dir -s/silk-dir")
            return 0

        if self.empty_cache and getpass.getuser() != "root":
            print("ERROR: this option can be used only by root")
            return 0

        if self.create_test_report:
            if os.path.exists(self.erigon_dir) == 0:
                print("ERROR: erigon build dir not specified correctly: ", self.erigon_dir)
                return 0

            if os.path.exists(self.silkworm_dir) == 0:
                print("ERROR: silkworm build dir not specified correctly: ", self.silkworm_dir)
                return 0

        return 1

    def __parse_args(self, argv):
        """ This methods parse input args """
        try:
            opts, _ = getopt.getopt(argv[1:], "hm:d:p:c:D:g:s:r:t:y:zw:uvxZRb:A:C:eT:M:j:PHI",
                   ['help', 'test-mode=', 'rpc-daemon-address=', 'pattern-file=', 'testing-daemon=', 'max-connections=',
                    'run-vegeta-on-core=', 'empty-cache', 'erigon-dir=', 'silk-dir=', 'repetitions=', 'test-sequence=',
                    'tmp-test-report', 'test-report', 'blockchain=', 'verbose', 'tracing', 'wait-after-test-sequence=',
                    'test-type=', 'not-verify-server-alive', 'response-timeout=', 'max-body-rsp=', 'json-report=',
                    'more-percentiles', 'halt-on-vegeta-error', 'instant-report'])

            for option, optarg in opts:
                if option in ("-h", "--help"):
                    usage(argv)
                elif option in ("-m", "--test-mode"):
                    self.test_mode = optarg
                elif option in ("-j", "--json-report"):
                    self.json_report_file = optarg
                elif option in ("-d", "--rpc-daemon-address"):
                    self.rpc_daemon_address = optarg
                elif option in ("-p", "--pattern-file"):
                    self.vegeta_pattern_tar_file = optarg
                elif option in ("-D", "--testing-daemon"):
                    self.testing_daemon = optarg
                elif option in ("-C", "--max-connections"):
                    self.max_connection = optarg
                elif option in ("-c", "--run-vegeta-on-core"):
                    self.daemon_vegeta_on_core = optarg
                elif option in ("-e", "--empty-cache"):
                    self.empty_cache = True
                elif option in ("-g", "--erigon-dir"):
                    self.erigon_dir = optarg
                elif option in ("-s", "--silk-dir"):
                    self.silkworm_dir = optarg
                elif option in ("-r", "--repetitions"):
                    self.repetitions = int(optarg)
                elif option in ("-t", "--test-sequence"):
                    self.test_sequence = optarg
                elif option in ("-R", "--tmp-test-report"):
                    self.create_test_report = True
                elif option in ("-b", "--blockchain"):
                    self.chain_name = optarg
                elif option in ("-u", "--test-report"):
                    self.create_test_report = True
                    self.versioned_test_report = True
                elif option in ("-v", "--verbose"):
                    self.verbose = True
                elif option in ("-x", "--tracing"):
                    self.verbose = True
                    self.tracing = True
                elif option in ("-w", "--wait-after-test-sequence"):
                    self.waiting_time = int(optarg)
                elif option in ("-y", "--test-type"):
                    self.test_type = optarg
                elif option in ("-Z", "--not-verify-server-alive"):
                    self.check_server_alive = False
                elif option in ("-T", "--response-timeout"):
                    self.vegeta_response_timeout = optarg
                elif option in ("-M", "--max-body-rsp"):
                    self.max_body_rsp = optarg
                elif option in ("-P", "--more-percentiles"):
                    self.more_percentiles = True
                elif option in ("-I", "--instant-report"):
                    self.instant_report = True
                elif option in ("-H", "--halt-on-vegeta-error"):
                    self.halt_on_vegeta_error = True
                else:
                    usage(argv)
        except getopt.GetoptError as err:
            # print help information and exit:
            print(err)
            usage(argv)
            sys.exit(1)

        if self.validate_input() == 0:
            usage(argv)
            sys.exit(1)


class PerfTest:
    """ This class manage performance test """

    def __init__(self, test_report, config):
        """ The initialization routine stop any previous server """
        self.test_report = test_report
        self.config = config
        self.cleanup(1)
        self.copy_and_extract_pattern_file()

    def cleanup(self, initial):
        """ Cleanup temporary files """
        cmd = "/bin/rm -f " + VEGETA_TAR_FILE_NAME
        os.system(cmd)
        cmd = "/bin/rm -rf " + VEGETA_PATTERN_DIRNAME
        os.system(cmd)
        cmd = "/bin/rm -f perf.data.old perf.data"
        os.system(cmd)
        if initial:
            cmd = "/bin/rm -rf " + RUN_TEST_DIRNAME
        else:
            cmd = "rmdir --ignore-fail-on-non-empty " + RUN_TEST_DIRNAME
        os.system(cmd)

    def copy_and_extract_pattern_file(self):
        """ Copy the vegeta pattern file into /tmp/run_tests_xyz/ and extract the file """
        if os.path.exists(self.config.vegeta_pattern_tar_file) == 0:
            print("ERROR: invalid pattern file: ", self.config.vegeta_pattern_tar_file)
            sys.exit(1)
        cmd = "mkdir " + RUN_TEST_DIRNAME
        status = os.system(cmd)
        if int(status) != 0:
            print("Vegeta temp pattern folder creation failed. Test Aborted!")
            sys.exit(1)
        cmd = "/bin/cp -f " + self.config.vegeta_pattern_tar_file + " " + VEGETA_TAR_FILE_NAME
        if self.config.tracing:
            print(f"Copy Vegeta pattern: {cmd}")
        status = os.system(cmd)
        if int(status) != 0:
            print("Vegeta pattern copy failed. Test Aborted!")
            sys.exit(1)

        cmd = "cd " + RUN_TEST_DIRNAME + "; tar xvf " + VEGETA_TAR_FILE_NAME + " > /dev/null 2>&1"
        if self.config.tracing:
            print(f"Extracting Vegeta pattern: {cmd}")
        status = os.system(cmd)
        if int(status) != 0:
            print("Vegeta pattern extraction failed. Test Aborted!")
            sys.exit(1)

        # If address is provided substitute the address and port of daemon in the vegeta file
        if self.config.rpc_daemon_address != "localhost":
            cmd = "sed -i 's/localhost/" + self.config.rpc_daemon_address + "/g' " + VEGETA_PATTERN_SILKWORM_BASE + self.config.test_type + ".txt"
            os.system(cmd)
            cmd = "sed -i 's/localhost/" + self.config.rpc_daemon_address + "/g' " + VEGETA_PATTERN_ERIGON_BASE + self.config.test_type + ".txt"
            os.system(cmd)

    def execute(self, test_number, repetition, name, qps_value, duration):
        """ Execute the tests using specified queries-per-second (QPS) and duration """
        if self.config.empty_cache:
            if "linux" in sys.platform or "linux2" in sys.platform:  # Linux
                status = os.system("sync && sudo sysctl vm.drop_caches=3 > /dev/null")
            elif sys.platform == "darwin":  # OS X
                status = os.system("sync && sudo purge > /dev/null")
        if name == SILKWORM:
            pattern = VEGETA_PATTERN_SILKWORM_BASE + self.config.test_type + ".txt"
        else:
            pattern = VEGETA_PATTERN_ERIGON_BASE + self.config.test_type + ".txt"

        on_core = self.config.daemon_vegeta_on_core.split(':')
        self.config.binary_file = datetime.today().strftime('%Y%m%d%H%M%S') + "_" + self.config.chain_name + "_" + self.config.testing_daemon + "_" +  self.config.test_type + \
                                                               "_" + qps_value + "_" + duration + "_" + str(repetition+1) + ".bin"
        if self.config.versioned_test_report:
            dirname = './reports/' + BINARY + '/'
        else:
            dirname = RUN_TEST_DIRNAME + "/" + BINARY + '/'
        pathlib.Path(dirname).mkdir(parents=True, exist_ok=True)
        self.config.binary_file_full_pathname = dirname + self.config.binary_file
        if self.config.max_connection == "0":
            vegeta_cmd = " vegeta attack -keepalive -rate=" + qps_value + " -format=json -duration=" + duration + "s -timeout=" + \
                           self.config.vegeta_response_timeout + "s -max-body=" + self.config.max_body_rsp
        else:
            vegeta_cmd = " vegeta attack -keepalive -rate=" + qps_value + " -format=json -duration=" + duration + "s -timeout=" + \
                          self.config.vegeta_response_timeout + "s -max-connections=" + self.config.max_connection + " -max-body=" + \
                          self.config.max_body_rsp
        if on_core[1] == "-":
            cmd = "cat " + pattern + " | " + vegeta_cmd + " | tee " + self.config.binary_file_full_pathname + " | vegeta report -type=text > " + VEGETA_REPORT + " &"
        else:
            cmd = "taskset -c " + on_core[1] + " cat " + pattern + " | " \
                  "taskset -c " + on_core[1] + vegeta_cmd + " tee " + self.config.binary_file_full_pathname + " | " \
                  "taskset -c " + on_core[1] + " vegeta report -type=text > " + VEGETA_REPORT + " &"

        #print ("Created binary file: ", self.config.binary_file_full_pathname)
        test_name = "[{:d}.{:2d}]"
        test_formatted = test_name.format(test_number, repetition+1)
        if self.config.testing_daemon != "":
            print(f"{test_formatted} " + self.config.testing_daemon + f": executes test qps: {qps_value} time: {duration} -> ", end="")
        else:
            print(f"{test_formatted} daemon: executes test qps: {qps_value} time: {duration} -> ", end="")
        sys.stdout.flush()
        status = os.system(cmd)
        if int(status) != 0:
            print("vegeta attach fails: Test Aborted!")
            return 1

        while 1:
            time.sleep(3)
            if self.config.check_server_alive:
                if self.config.testing_daemon != "":
                    cmd = "ps aux | grep '" + self.config.testing_daemon + "' | grep -v 'grep' | awk '{print $2}'"
                else:
                    cmd = "ps aux | grep '" + ERIGON_RPC_SERVER_NAME + "' | grep -v 'grep' | awk '{print $2}'"
                pid = os.popen(cmd).read()
                if pid == "":
                    # the server is dead; kill vegeta and returns fails
                    os.system("kill -2 $(ps aux | grep 'vegeta' | grep -v 'grep' | grep -v 'python' | awk '{print $2}') 2> /dev/null")
                    print("test failed: server is Dead")
                    return 1

            pid = os.popen("ps aux | grep 'vegeta report' | grep -v 'grep' | awk '{print $2}'").read()
            if pid == "":
                # Vegeta has completed its works, generate report
                return self.get_result(test_number, repetition, name, qps_value, duration)

    def execute_sequence(self, sequence, tag):
        """ Execute the sequence of tests """
        test_number = 1
        if tag == SILKWORM:
            pattern = VEGETA_PATTERN_SILKWORM_BASE + self.config.test_type + ".txt"
        else:
            pattern = VEGETA_PATTERN_ERIGON_BASE + self.config.test_type + ".txt"

        # retrieve port where load tests is provided
        with open(pattern, "r") as file:
            data = file.readline().strip()
            parsed_data = json.loads(data)
            url = parsed_data.get("url")
            print ("Test on port: ",url)

        for test in sequence:
            for test_rep in range(0, self.config.repetitions):
                qps = test.split(':')[0]
                duration = test.split(':')[1]
                result = self.execute(test_number, test_rep, tag, qps, duration)
                if result == 1:
                    print("Server dead test Aborted!")
                    return 1
                time.sleep(self.config.waiting_time)
            test_number = test_number + 1
            print("")
        return 0

    def get_result(self, test_number, repetition, daemon_name, qps_value, duration):
        """ Processes the report file generated by vegeta and reads latency data """
        test_report_filename = VEGETA_REPORT
        file = open(test_report_filename, encoding='utf8')
        try:
            file_rows = file.readlines()
            if len(file_rows) == 0:
                return 1
            newline = file_rows[2].replace('\n', ' ')
            latency_values = newline.split(',')
            min_latency = latency_values[6].split(']')[1]
            min_latency = min_latency.replace("\u00b5s", "us").strip()
            mean = latency_values[7]
            mean = mean.replace("\u00b5s", "us").strip()
            p50 = latency_values[8]
            p50 = p50.replace("\u00b5s", "us").strip()
            p90 = latency_values[9]
            p90 = p90.replace("\u00b5s", "us").strip()
            p95 = latency_values[10]
            p95 = p95.replace("\u00b5s", "us").strip()
            p99 = latency_values[11]
            p99 = p99.replace("\u00b5s", "us").strip()
            p100 = latency_values[12]
            p100 = p100.replace("\u00b5s", "us").strip()
            newline = file_rows[5].replace('\n', ' ')
            ratio = newline.split(' ')[34]
            if len(file_rows) > 8:
                error = file_rows[8].rstrip()
                print(f'[R={ratio} max={p100} error={error}]')
            else:
                error = ""
                if self.config.more_percentiles:
                    print(f'[R={ratio} p50={p50} p90={p90} p95={p95} p99={p99} max={p100}]')
                else:
                    print(f'[R={ratio} max={p100}]')
        finally:
            file.close()
            
        if error != "" and self.config.halt_on_vegeta_error:
            print("test failed: " + error)
            return 1

        if ratio != "100.00%":
            print("test failed: ratio is not 100.00%")
            return 1

        if self.config.create_test_report:
            self.test_report.write_test_report(daemon_name, test_number, repetition, qps_value, duration, min_latency, mean, p50,
                                               p90, p95, p99, p100, ratio, error)
        if self.config.instant_report:
            os.system("cat " + test_report_filename)  
        os.system("/bin/rm " + test_report_filename)
        return 0


class Hardware:
    """ Extract hardware information from the underlying platform. """

    @classmethod
    def vendor(cls):
        """ Return the system vendor """
        command = "cat /sys/devices/virtual/dmi/id/sys_vendor"
        return os.popen(command).readline().replace('\n', '')

    @classmethod
    def normalized_vendor(cls):
        """ Return the system vendor as lowercase first-token split by whitespace """
        return cls.vendor().split(' ')[0].lower()

    @classmethod
    def product(cls):
        """ Return the system product name """
        command = "cat /sys/devices/virtual/dmi/id/product_name"
        return os.popen(command).readline().replace('\n', '')

    @classmethod
    def board(cls):
        """ Return the system board name """
        command = "cat /sys/devices/virtual/dmi/id/board_name"
        return os.popen(command).readline().replace('\n', '')

    @classmethod
    def normalized_product(cls):
        """ Return the system product name as lowercase w/o whitespaces """
        return cls.product().replace(' ', '').lower()

    @classmethod
    def normalized_board(cls):
        """ Return the board name as lowercase w/o whitespaces """
        return cls.board().split('/')[0].replace(' ', '').lower()


class TestReport:
    """ The Comma-Separated Values (CSV) test report """

    def __init__(self, config):
        """ Create a new TestReport """
        self.csv_file = ''
        self.writer = ''
        self.json_test_report = ''
        self.config = config

    def create_csv_file(self):
        """ Creates CSV file """
        extension = Hardware.normalized_product()
        if extension == "systemproductname":
            extension = Hardware.normalized_board()
        csv_folder = Hardware.normalized_vendor() + '_' + extension
        if self.config.versioned_test_report:
            csv_folder_path = './reports/' + self.config.chain_name + '/' + csv_folder
        else:
            csv_folder_path = RUN_TEST_DIRNAME + "/" + self.config.chain_name + '/' + csv_folder
        pathlib.Path(csv_folder_path).mkdir(parents=True, exist_ok=True)

        # Generate unique CSV file name w/ date-time and open it
        if self.config.testing_daemon != "":
            csv_filename = self.config.test_type + "_" + datetime.today().strftime('%Y%m%d%H%M%S') + "_" + self.config.testing_daemon + "_perf.csv"
        else:
            csv_filename = self.config.test_type + "_" + datetime.today().strftime('%Y%m%d%H%M%S') + "_perf.csv"
        csv_filepath = csv_folder_path + '/' + csv_filename
        self.csv_file = open(csv_filepath, 'w', newline='', encoding='utf8')
        self.writer = csv.writer(self.csv_file)
        print("Perf report file: " + csv_filepath + "\n")

    def open(self):
        """ Writes on CSV file the header """
        self.create_csv_file()
        command = "sum " + self.config.vegeta_pattern_tar_file
        checksum = os.popen(command).read().split('\n')

        command = "gcc --version"
        gcc_vers = os.popen(command).read().split(',')

        command = "go version 2> /dev/null"
        go_vers = os.popen(command).read().replace('\n', '')

        command = "uname -r"
        kern_vers = os.popen(command).read().replace('\n', "").replace('\'', '')

        command = "cat /proc/cpuinfo | grep 'model name' | uniq"
        model = os.popen(command).readline().replace('\n', ' ').split(':')
        command = "cat /proc/cpuinfo | grep 'bogomips' | uniq"
        tmp = os.popen(command).readline().replace('\n', '').split(':')[1]
        bogomips = tmp.replace(' ', '')

        erigon_commit = ""
        silkrpc_commit = ""
        if self.config.test_mode in ("1", "3"):
            command = "cd " + self.config.silkworm_dir + " && git rev-parse HEAD 2> /dev/null"
            silkrpc_commit = os.popen(command).read().replace('\n', '')

        if self.config.test_mode in ("2", "3"):
            command = "cd " + self.config.erigon_dir + " && git rev-parse HEAD 2> /dev/null"
            erigon_commit = os.popen(command).read().replace('\n', '')

        self.write_test_header(model[1], bogomips, kern_vers, checksum[0], gcc_vers[0], go_vers, silkrpc_commit, erigon_commit)

    def write_test_header_on_json(self, model, bogomips, kern_vers, checksum, gcc_vers, go_vers, silkrpc_commit, erigon_commit):
        """ Writes test header on json """
        self.json_test_report = {
           'platform': {
               'vendor': Hardware.vendor().lstrip().rstrip(),
               'product': Hardware.product().lstrip().rstrip(),
               'board': Hardware.board().lstrip().rstrip(),
               'cpu': model.lstrip().rstrip(),
               'bogomips': bogomips.lstrip().rstrip(),
               'kernel': kern_vers.lstrip().rstrip(),
               'gccVersion': gcc_vers.lstrip().rstrip(),
               'goVersion': go_vers.lstrip().rstrip(),
               'silkrpcCommit': silkrpc_commit.lstrip().rstrip(),
               'erigonCommit': erigon_commit.lstrip().rstrip()
           },
           "configuration": {
               "testingDaemon": self.config.testing_daemon,
               "testingApi": self.config.test_type,
               "testSequence": self.config.test_sequence,
               "testRepetitions": self.config.repetitions,
               "vegetaFile": self.config.vegeta_pattern_tar_file,
               "vegetaChecksum": checksum,
               "taskset": self.config.daemon_vegeta_on_core,
           },
           "results": []
        }

    def write_test_header(self, model, bogomips, kern_vers, checksum, gcc_vers, go_vers, silkrpc_commit, erigon_commit):
        """ Writes test header """
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "vendor", Hardware.vendor()])
        product = Hardware.product()
        if product != "System Product Name":
            self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "product", product])
        else:
            self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "board", Hardware.board()])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "cpu", model])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "bogomips", bogomips])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "kernel", kern_vers])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "taskset", self.config.daemon_vegeta_on_core])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "vegetaFile", self.config.vegeta_pattern_tar_file])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "vegetaChecksum", checksum])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "gccVersion", gcc_vers])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "goVersion", go_vers])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "silkrpcVersion", silkrpc_commit])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "erigonVersion", erigon_commit])
        self.writer.writerow([])
        self.writer.writerow([])
        self.writer.writerow(["Daemon", "TestNo", "Repetition", "Qps", "Time(secs)", "Min", "Mean", "50", "90", "95", "99", "Max", "Ratio", "Error"])
        self.csv_file.flush()

        if self.config.json_report_file != "":
            self.write_test_header_on_json(model, bogomips, kern_vers, checksum, gcc_vers, go_vers, silkrpc_commit, erigon_commit)

    def write_test_report(self, daemon, test_number, repetition, qps_value, duration, min_latency, mean, fifty, ninty, nintyfive, nintynine,
                          max_latency, ratio, error):
        """ Writes on CSV the latency data for one completed test """
        self.writer.writerow([daemon, test_number, repetition, qps_value, duration, min_latency, mean, fifty, ninty, nintyfive, nintynine, max_latency, ratio, error])
        self.csv_file.flush()

        if self.config.json_report_file != "":
            self.write_test_report_on_json(test_number, repetition, qps_value, duration)

    def write_test_report_on_json(self, test_number, repetition, qps_value, duration):
        """ Writes on json the latency data for one completed test """
        if repetition == 0:
            self.json_test_report['results'].append({
                'qps': qps_value.lstrip().strip(),
                'duration': duration.lstrip().strip(),
                'testRepetitions': [],
            })
        cmd = "vegeta report --type=json " + self.config.binary_file_full_pathname
        json_string = os.popen(cmd).read()
        if json_string == "":
            print("error in vegeta report --type=json")
            sys.exit(1)
        json_object = json.loads(json_string)

        cmd = "vegeta report --type=hdrplot " + self.config.binary_file_full_pathname
        hdrplot_string = os.popen(cmd).read()
        if hdrplot_string == "":
            print("error in vegeta report --type=hdrplot")
            sys.exit(1)

        self.json_test_report['results'][test_number-1]['testRepetitions'].append({
               'vegetaBinary': self.config.binary_file,
               'vegetaReport': json_object,
               'vegetaReportHdrPlot': hdrplot_string
        })

    def close(self):
        """ Close the report """
        self.csv_file.flush()
        self.csv_file.close()

        if self.config.json_report_file != "":
            print("Create json file: ", self.config.json_report_file)
            with open(self.config.json_report_file, 'w', encoding='utf-8') as report_file:
                json.dump(self.json_test_report, report_file, indent=4)


#
# main
#
def main(argv):
    """ Execute performance tests on selected user configuration """
    print("Performance Test started")
    config = Config(argv)
    test_report = TestReport(config)
    perf_test = PerfTest(test_report, config)

    print(f"Test repetitions: {config.repetitions} on sequence: {config.test_sequence} for pattern: {config.vegeta_pattern_tar_file}")
    if config.create_test_report:
        test_report.open()

    current_sequence = str(config.test_sequence).split(',')

    if config.test_mode in ("1", "3"):
        result = perf_test.execute_sequence(current_sequence, SILKWORM)
        if result == 1:
            print("Server dead test Aborted!")
            if config.create_test_report:
                test_report.close()
            return 1
        if config.test_mode == "3":
            print("--------------------------------------------------------------------------------------------\n")

    if config.test_mode in ("2", "3"):
        result = perf_test.execute_sequence(current_sequence, ERIGON)
        if result == 1:
            print("Server dead test Aborted!")
            if config.create_test_report:
                test_report.close()
            return 1

    if config.create_test_report:
        test_report.close()
    perf_test.cleanup(0)
    print("Performance Test completed successfully.")
    return 0


#
# module as main
#
if __name__ == "__main__":
    sys.exit(main(sys.argv))
