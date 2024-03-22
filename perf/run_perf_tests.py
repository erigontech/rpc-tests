#!/usr/bin/env python3
""" This script uses Vegeta to execute a list of performance tests (configured via command line) and saves its result in CSV file
"""

# pylint: disable=consider-using-with

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
DEFAULT_ERIGON_ADDRESS = "localhost:9090"
DEFAULT_ERIGON_BUILD_DIR = ""
DEFAULT_SILKRPC_BUILD_DIR = ""
DEFAULT_RPCDAEMON_ADDRESS = "localhost"
DEFAULT_TEST_MODE = "3"
DEFAULT_WAITING_TIME = 5
DEFAULT_MAX_CONN = "9000"
DEFAULT_TEST_TYPE = "eth_getLogs"
DEFAULT_VEGETA_RESPONSE_TIMEOUT = "300"
DEFAULT_MAX_BODY_RSP = "1500"

SILKRPC="silk"
RPCDAEMON="rpcdaemon"
SILKRPC_SERVER_NAME="rpcdaemon"
RPCDAEMON_SERVER_NAME="rpcdaemon"
RAND_NUM = randint(0, 100000)
RUN_TEST_DIRNAME = "/tmp/run_tests_" + str(RAND_NUM)
VEGETA_PATTERN_DIRNAME = RUN_TEST_DIRNAME + "/erigon_stress_test"
VEGETA_REPORT = RUN_TEST_DIRNAME + "/vegeta_report.hrd"
VEGETA_TAR_FILE_NAME = RUN_TEST_DIRNAME + "/vegeta_TAR_File"
VEGETA_PATTERN_SILKRPC_BASE = VEGETA_PATTERN_DIRNAME + "/vegeta_geth_"
VEGETA_PATTERN_RPCDAEMON_BASE = VEGETA_PATTERN_DIRNAME + "/vegeta_erigon_"

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
    print("-A,--additional-string-name <string>: string to be add in the file name")
    print("-b,--blockchain <chain name>:         mandatory in case of -R or -u")
    print("-y,--test-type <test-type>:           eth_call, eth_getLogs, ...                                         [default: " + DEFAULT_TEST_TYPE + "]")
    print("-m,--test-mode <0,1,2>:               silkrpc(1), rpcdaemon(2), both(3)                                  [default: " + str(DEFAULT_TEST_MODE) + "]")
    print("-p,--pattern-file <file-name>:        path to the request file for Vegeta attack                         [default: " + DEFAULT_VEGETA_PATTERN_TAR_FILE +"]")
    print("-r,--repetitions <number>:            number of repetitions for each element in test sequence (e.g. 10)  [default: " + str(DEFAULT_REPETITIONS) + "]")
    print("-t,--test-sequence <seq>:             list of qps/timeas <qps1>:<t1>,... (e.g. 200:30,400:10)            [default: " + DEFAULT_TEST_SEQUENCE + "]")
    print("-w,--wait-after-test-sequence <secs>: time interval between successive test iterations in sec            [default: " + str(DEFAULT_WAITING_TIME) + "]")
    print("-d,--rpc-daemon-address <addr>:       address of RPCDaemonc (e.g. 192.2.3.1)                             [default: " + DEFAULT_RPCDAEMON_ADDRESS +"]")
    print("-g,--erigon-dir <path>:               path to erigon folder (e.g. /home/erigon)                          [default: " + DEFAULT_ERIGON_BUILD_DIR + "]")
    print("-s,--silk-dir <path>:                 path to silk folder (e.g. /home/silkworm)                          [default: " + DEFAULT_SILKRPC_BUILD_DIR + "]")
    print("-c,--run-vegeta-on-core <...>         taskset format for vegeta (e.g. 0-1:2-3 or 0-2:3-4)                [default: " + DEFAULT_DAEMON_VEGETA_ON_CORE +"]")
    print("-T,--response-timeout <timeout>:      vegeta response timeout                                            [default: " + DEFAULT_VEGETA_RESPONSE_TIMEOUT + "]")
    print("-M,--max-body-rsp <size>:             max number of bytes to read from response bodies                   [default: " + DEFAULT_MAX_BODY_RSP + "]")
    sys.exit(-1)

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
        self.silkrpc_dir = DEFAULT_SILKRPC_BUILD_DIR
        self.repetitions = DEFAULT_REPETITIONS
        self.test_sequence = DEFAULT_TEST_SEQUENCE
        self.rpc_daemon_address = DEFAULT_RPCDAEMON_ADDRESS
        self.test_mode = DEFAULT_TEST_MODE
        self.test_type = DEFAULT_TEST_TYPE
        self.additional_string = ""
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

        self.__parse_args(argv)

    def __parse_args(self, argv):
        try:
            local_config = 0
            specified_chain = 0
            opts, _ = getopt.getopt(argv[1:], "hm:d:p:c:a:g:s:r:t:y:zw:uvxZRb:A:C:eT:M:",
                   ['help', 'test-mode=', 'rpc-daemon-address=', 'pattern-file=', 'additional-string-name=', 'max-connections=',
                    'run-vegeta-on-core=', 'empty-cache', 'erigon-dir=', 'silk-dir=', 'repetitions=', 'test-sequence=',
                    'tmp-test-report', 'test-report', 'blockchain=', 'verbose', 'tracing', 'wait-after-test-sequence=', 'test-type=',
                    'not-verify-server-alive', 'response-timeout=', 'max-body-rsp='])

            for option, optarg in opts:
                if option in ("-h", "--help"):
                    usage(argv)
                elif option in ("-m", "--test-mode"):
                    self.test_mode = optarg
                elif option in ("-d", "--rpc-daemon-address"):
                    if local_config == 1:
                        print("ERROR: incompatible option -d/rpc-daemon-address with -g/erigon-dir -s/silk-dir")
                        usage(argv)
                    local_config = 2
                    self.rpc_daemon_address = optarg
                elif option in ("-p", "--pattern-file"):
                    self.vegeta_pattern_tar_file = optarg
                elif option in ("-A", "--additional-string-name"):
                    self.additional_string = optarg
                elif option in ("-C", "--max-connections"):
                    self.max_connection = optarg
                elif option in ("-c", "--run-vegeta-on-core"):
                    self.daemon_vegeta_on_core = optarg
                elif option in ("-e", "--empty-cache"):
                    if getpass.getuser() != "root":
                        print("ERROR: this option can be used only by root")
                        usage(argv)
                    self.empty_cache = True
                elif option in ("-g", "--erigon-dir"):
                    if local_config == 2:
                        print("ERROR: incompatible option -d/rpc-daemon-address with -g/erigon-dir -s/silk-dir")
                        usage(argv)
                    local_config = 1
                    self.erigon_dir = optarg
                elif option in ("-s", "--silk-dir"):
                    if local_config == 2:
                        print("ERROR: incompatible option -d/rpc-daemon-address with -g/erigon-dir -s/silk-dir")
                        usage(argv)
                    local_config = 1
                    self.silkrpc_dir = optarg
                elif option in ("-r", "--repetitions"):
                    self.repetitions = int(optarg)
                elif option in ("-t", "--test-sequence"):
                    self.test_sequence = optarg
                elif option in ("-R", "--tmp-test-report"):
                    self.create_test_report = True
                    if os.path.exists(self.erigon_dir) == 0:
                        print ("ERROR: erigon buildir not specified correctly: ", self.erigon_dir)
                        usage(argv)
                    if os.path.exists(self.silkrpc_dir) == 0:
                        print ("ERROR: silkrpc buildir not specified correctly: ", self.silkrpc_dir)
                        usage(argv)
                    if specified_chain == 0:
                        print ("ERROR: chain not specified ")
                        usage(argv)
                elif option in ("-b", "--blockchain"):
                    self.chain_name = optarg
                    specified_chain = 1
                elif option in ("-u", "--test-report"):
                    self.create_test_report = True
                    self.versioned_test_report = True
                    if os.path.exists(self.erigon_dir) == 0:
                        print ("ERROR: erigon buildir not specified correctly: ", self.erigon_dir)
                        usage(argv)
                    if os.path.exists(self.silkrpc_dir) == 0:
                        print ("ERROR: silkrpc buildir not specified correctly: ", self.silkrpc_dir)
                        usage(argv)
                    if specified_chain == 0:
                        print ("ERROR: chain not specified ")
                        usage(argv)
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
                else:
                    usage(argv)
        except getopt.GetoptError as err:
            # print help information and exit:
            print(err)
            usage(argv)
            sys.exit(-1)


class PerfTest:
    """ This class manage performance test """

    def __init__(self, test_report, config):
        """ The initialization routine stop any previos server """
        self.test_report = test_report
        self.config = config
        self.cleanup(1)
        self.copy_and_extract_pattern_file()

    def cleanup(self, initial):
        """ Cleanup temporary files """
        self.silk_daemon = 0
        self.rpc_daemon = 0
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
        """ Copy the vegeta pattern file into /tmp/run_tests_xyz/ and untar the file """
        if os.path.exists(self.config.vegeta_pattern_tar_file) == 0:
            print ("ERROR: invalid pattern file: ", self.config.vegeta_pattern_tar_file)
            sys.exit(-1)
        cmd = "mkdir " +  RUN_TEST_DIRNAME
        status = os.system(cmd)
        cmd = "/bin/cp -f " + self.config.vegeta_pattern_tar_file + " " + VEGETA_TAR_FILE_NAME
        if self.config.tracing:
            print(f"Copy Vegeta pattern: {cmd}")
        status = os.system(cmd)
        if int(status) != 0:
            print("Vegeta pattern copy failed. Test Aborted!")
            sys.exit(-1)

        cmd = "cd " + RUN_TEST_DIRNAME + "; tar xvf " + VEGETA_TAR_FILE_NAME + " > /dev/null"
        if self.config.tracing:
            print(f"Extracting Vegeta pattern: {cmd}")
        status = os.system(cmd)
        if int(status) != 0:
            print("Vegeta pattern untar failed. Test Aborted!")
            sys.exit(-1)

        # If address is provided substitute the address and port of daemon in the vegeta file
        if self.config.rpc_daemon_address != "localhost":
            cmd = "sed -i 's/localhost/" + self.config.rpc_daemon_address + "/g' " + VEGETA_PATTERN_SILKRPC_BASE + self.config.test_type + ".txt"
            os.system(cmd)
            cmd = "sed -i 's/localhost/" + self.config.rpc_daemon_address + "/g' " + VEGETA_PATTERN_RPCDAEMON_BASE + self.config.test_type + ".txt"
            os.system(cmd)

    def execute(self, test_number, name, qps_value, duration):
        """ Execute the tests using specified queries-per-second (QPS) and duration """
        if self.config.empty_cache:
            if "linux" in sys.platform or "linux2" in sys.platform: #linux
                status = os.system("sync && sudo sysctl vm.drop_caches=3 > /dev/null")
            elif sys.platform == "darwin": # OS X
                status = os.system("sync && sudo purge > /dev/null")
        if name == SILKRPC:
            pattern = VEGETA_PATTERN_SILKRPC_BASE + self.config.test_type + ".txt"
        else:
            pattern = VEGETA_PATTERN_RPCDAEMON_BASE + self.config.test_type + ".txt"
        on_core = self.config.daemon_vegeta_on_core.split(':')
        if self.config.max_connection == "0":
            vegeta_cmd = " vegeta attack -keepalive -rate=" + qps_value + " -format=json -duration=" + duration + "s -timeout=" + \
                           self.config.vegeta_response_timeout + "s -max-body=" + self.config.max_body_rsp
        else:
            vegeta_cmd = " vegeta attack -keepalive -rate=" + qps_value + " -format=json -duration=" + duration + "s -timeout=" + \
                          self.config.vegeta_response_timeout + "s -max-connections=" + self.config.max_connection + " -max-body=" + \
                          self.config.max_body_rsp
        if on_core[1] == "-":
            cmd = "cat " + pattern + " | " + vegeta_cmd + " | vegeta report -type=text > " + VEGETA_REPORT + " &"
        else:
            cmd = "taskset -c " + on_core[1] + " cat " + pattern + " | " \
                  "taskset -c " + on_core[1] + vegeta_cmd + " | " \
                  "taskset -c " + on_core[1] + " vegeta report -type=text > " + VEGETA_REPORT + " &"
        print(f"{test_number} daemon: executes test qps: {qps_value} time: {duration} -> ", end="")
        sys.stdout.flush()
        status = os.system(cmd)
        if int(status) != 0:
            print("vegeta test fails: Test Aborted!")
            return 0

        while 1:
            time.sleep(3)
            if self.config.check_server_alive:
                if name == SILKRPC:
                    cmd = "ps aux | grep '" + SILKRPC_SERVER_NAME + "' | grep -v 'grep' | awk '{print $2}'"
                else:
                    cmd = "ps aux | grep '" + RPCDAEMON_SERVER_NAME + "' | grep -v 'grep' | awk '{print $2}'"
                pid = os.popen(cmd).read()
                if pid == "" :
                    # the server is dead; kill vegeta and returns fails
                    os.system("kill -2 $(ps aux | grep 'vegeta' | grep -v 'grep' | grep -v 'python' | awk '{print $2}') 2> /dev/null")
                    return 0

            pid = os.popen("ps aux | grep 'vegeta report' | grep -v 'grep' | awk '{print $2}'").read()
            if pid == "":
                # Vegeta has completed its works, generate report and return OK
                self.get_result(test_number, name, qps_value, duration)
                return 1

    def execute_sequence(self, sequence, tag):
        """ Execute the sequence of tests """
        test_number = 1
        for test in sequence:
            for test_rep in range(0, self.config.repetitions):
                qps = test.split(':')[0]
                duration = test.split(':')[1]
                test_name = "[{:d}.{:2d}] "
                test_name_formatted = test_name.format(test_number, test_rep+1)
                result = self.execute(test_name_formatted, tag, qps, duration)
                if result == 0:
                    print("Server dead test Aborted!")
                    return 0
                time.sleep(self.config.waiting_time)
            test_number = test_number + 1
            print("")
        return 1

    def get_result(self, test_number, daemon_name, qps_value, duration):
        """ Processes the report file generated by vegeta and reads latency data """
        test_report_filename = VEGETA_REPORT
        file = open(test_report_filename, encoding='utf8')
        try:
            file_raws = file.readlines()
            newline = file_raws[2].replace('\n', ' ')
            latency_values = newline.split(',')
            min_latency = latency_values[6].split(']')[1]
            max_latency = latency_values[12]
            newline = file_raws[5].replace('\n', ' ')
            ratio = newline.split(' ')[34]
            if len(file_raws) > 8:
                error = file_raws[8]
                print(" [ Ratio="+ratio+", MaxLatency="+max_latency+ " Error: " + error +"]")
            else:
                error = ""
                print(" [ Ratio="+ratio+", MaxLatency="+max_latency+"]")
            threads = os.popen("ps -efL | grep erigon | grep bin | wc -l").read().replace('\n', ' ')
        finally:
            file.close()

        if self.config.create_test_report:
            self.test_report.write_test_report(daemon_name, test_number, threads, qps_value, duration, min_latency, latency_values[7], latency_values[8], \
                                               latency_values[9], latency_values[10], latency_values[11], max_latency, ratio, error)
        os.system("/bin/rm " + test_report_filename)


class Hardware:
    """ Extract hardware information from the underlying platform. """

    @classmethod
    def vendor(cls):
        """ Return the system vendor """
        command = "cat /sys/devices/virtual/dmi/id/sys_vendor"
        return os.popen(command).readline().replace('\n', '')

    @classmethod
    def normalized_vendor(cls):
        """ Return the system vendor as lowercase first-token splitted by whitespace """
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
        self.config = config

    def open(self):
        """ Writes on CSV file the header """
        estension = Hardware.normalized_product()
        if estension == "systemproductname":
            estension = Hardware.normalized_board()
        csv_folder = Hardware.normalized_vendor() + '_' + estension
        if self.config.versioned_test_report:
            csv_folder_path = './reports/' + self.config.chain_name + '/' + csv_folder
        else:
            csv_folder_path = RUN_TEST_DIRNAME + "/" + self.config.chain_name + '/' + csv_folder
        pathlib.Path(csv_folder_path).mkdir(parents=True, exist_ok=True)

        # Generate unique CSV file name w/ date-time and open it
        if self.config.additional_string != "":
            csv_filename = self.config.test_type + "_" + datetime.today().strftime('%Y%m%d%H%M%S') + "_" + self.config.additional_string + "_perf.csv"
        else:
            csv_filename = self.config.test_type + "_" + datetime.today().strftime('%Y%m%d%H%M%S') + "_perf.csv"
        csv_filepath = csv_folder_path + '/' + csv_filename
        self.csv_file = open(csv_filepath, 'w', newline='', encoding='utf8')
        self.writer = csv.writer(self.csv_file)

        print("Perf report file: " + csv_filepath + "\n")

        command = "sum "+ self.config.vegeta_pattern_tar_file
        checksum = os.popen(command).read().split('\n')

        command = "gcc --version"
        gcc_vers = os.popen(command).read().split(',')

        command = "go version"
        go_vers = os.popen(command).read().replace('\n', '')

        command = "uname -r"
        kern_vers = os.popen(command).read().replace('\n', "").replace('\'', '')

        command = "cat /proc/cpuinfo | grep 'model name' | uniq"
        model = os.popen(command).readline().replace('\n', ' ').split(':')
        command = "cat /proc/cpuinfo | grep 'bogomips' | uniq"
        tmp = os.popen(command).readline().replace('\n', '').split(':')[1]
        bogomips = tmp.replace(' ', '')

        erigon_branch = ""
        erigon_commit = ""
        silkrpc_branch = ""
        silkrpc_commit = ""
        if self.config.test_mode in ("1", "3"):
            command = "cd " + self.config.silkrpc_dir + " && git branch --show-current"
            silkrpc_branch = os.popen(command).read().replace('\n', '')

            command = "cd " + self.config.silkrpc_dir + " && git rev-parse HEAD"
            silkrpc_commit = os.popen(command).read().replace('\n', '')

        if self.config.test_mode in ("2", "3"):
            command = "cd " + self.config.erigon_dir + " && git branch --show-current"
            erigon_branch = os.popen(command).read().replace('\n', '')

            command = "cd " + self.config.erigon_dir + " && git rev-parse HEAD"
            erigon_commit = os.popen(command).read().replace('\n', '')

        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "Vendor", Hardware.vendor()])
        product = Hardware.product()
        if product != "System Product Name":
            self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "Product", product])
        else:
            self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "Board", Hardware.board()])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "CPU", model[1]])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "Bogomips", bogomips])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "Kernel", kern_vers])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "DaemonVegetaRunOnCore", self.config.daemon_vegeta_on_core])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "VegetaFile", self.config.vegeta_pattern_tar_file])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "VegetaChecksum", checksum[0]])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "GCC version", gcc_vers[0]])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "Go version", go_vers])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "Silkrpc version", silkrpc_branch + " " + silkrpc_commit])
        self.writer.writerow(["", "", "", "", "", "", "", "", "", "", "", "", "Erigon version", erigon_branch + " " + erigon_commit])
        self.writer.writerow([])
        self.writer.writerow([])
        self.writer.writerow(["Daemon", "TestNo", "TG-Threads", "Qps", "Time", "Min", "Mean", "50", "90", "95", "99", "Max", "Ratio", "Error"])
        self.csv_file.flush()

    def write_test_report(self, daemon, test_number, threads, qps_value, duration, min_latency, mean, fifty, ninty, nintyfive, nintynine, max_latency, ratio, error):
        """ Writes on CSV the latency data for one completed test """
        self.writer.writerow([daemon, str(test_number), threads, qps_value, duration, min_latency, mean, fifty, ninty, nintyfive, nintynine, max_latency, ratio, error])
        self.csv_file.flush()

    def close(self):
        """ Close the report """
        self.csv_file.flush()
        self.csv_file.close()


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
        result = perf_test.execute_sequence(current_sequence, SILKRPC)
        if result == 0:
            print("Server dead test Aborted!")
            if config.create_test_report:
                test_report.close()
            sys.exit(-1)
        if config.test_mode == "3":
            print("--------------------------------------------------------------------------------------------\n")

    if config.test_mode in ("2", "3"):
        result = perf_test.execute_sequence(current_sequence, RPCDAEMON)
        if result == 0:
            print("Server dead test Aborted!")
            if config.create_test_report:
                test_report.close()
            sys.exit(-1)

    if config.create_test_report:
        test_report.close()
    perf_test.cleanup(0)
    print("Performance Test completed successfully.")


#
# module as main
#
if __name__ == "__main__":
    main(sys.argv)
    sys.exit(0)
