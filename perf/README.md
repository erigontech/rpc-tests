# Performance Tests
These are the instructions to execute the performance comparison tests between Silkrpc and Erigon RPCDaemon.

## System Configuration
The following system configuration steps shall be performed:

* increase core dump size 
```
ulimit -c unlimited
```
* increase max file descriptors
```
ulimit -n 999999
```
* check current IP local port range and increase it (writing permanently in /etc/sysctl.conf)

### Linux
```
cat /proc/sys/net/ipv4/ip_local_port_range
sudo sysctl -w "net.ipv4.ip_local_port_range=5000 65000"
```
### macOS
```
sudo sysctl net.inet.ip.portrange
sudo sysctl net.inet.ip.portrange.first=5000 net.inet.ip.portrange.last=65535
```

## Software Versions
In order to reproduce the environment used in last performance testing session, pick the following source code versions:

* Erigon RPCDaemon:  https://github.com/ledgerwatch/erigon/releases/tag/v2.48.1
* Silkworm RPCDaemon (a.k.a. Silkrpc) LATEST: https://github.com/erigontech/silkworm

## Build
Follow the instructions for building:

* Erigon RPCDaemon [build](https://github.com/)
* Silkworm RPCDaemon (a.k.a. Silkrpc) [build](https://github.com/torquem-ch/silkworm)

## Setup
Currently, our setup for performance tests is "all-in-one", executing Erigon Core, Erigon RPCDaemon and/or Silkworm RPCDaemon all on the same host.
These are the instructions to execute the performance comparison tests.

### Activation
The shell commands to activate such Erigon Core for performance testing are

#### _Erigon Core_
From Erigon project directory:
```
build/bin/erigon --goerli --private.api.addr=localhost:9090
```
#### _Erigon RPCDaemon_
```
./build/bin/rpcdaemon --private.api.addr=localhost:9090 --http.api=eth,debug,net,web3,txpool,trace,erigon,parity,ots  --datadir <erigon_data_dir>
```

#### _Silkrpc_
```
./build/cmd/silkrpcdaemon --private.addr 127.0.0.1:9090 --eth.addr 127.0.0.1:51515 --engine.addr 127.0.0.1:51516 --workers 64 --contexts 8 --datadir <erigon_data_dir>  --api admin,debug,eth,parity,erigon,trace,web3,txpool,ots,net --log.verbosity 2 
```
You *must* specify different ports for Silkrpc (i.e. `--eth.addr 127.0.0.1:51515 --engine.addr 127.0.0.1:51516`) if you want to run Silkrpc simultaneously with Erigon RPCDaemon.

### Test Workload

Currently the performance workload targets just the [eth_getLogs, eth_call, eth_getBalance] Ethereum API. The test workloads are executed using requests files of [Vegeta](https://github.com/tsenart/vegeta/), a HTTP load testing tool.

#### _Workload Generation_

Execute the relevant Erigon bench tool e.g. [bench_ethcall](https://github.com/ledgerwatch/erigon/blob/devel/cmd/rpctest/rpctest/bench_ethcall.go) against both Erigon RPCDaemon and Silkrpc using the following command line:

```
build/bin/rpctest bench_ethcall --erigonUrl http://localhost:8545 --gethUrl http://localhost:51515 --blockFrom 200000 --blockTo 300000
```

Vegeta request files are written to `/tmp/erigon_stress_test`:
* results_geth_debug_getModifiedAccountsByNumber.csv, results_geth_eth_<api>.csv
* results_turbo_geth_debug_getModifiedAccountsByNumber.csv, results_turbo_geth_<api>.csv
* vegeta_geth_debug_getModifiedAccountsByNumber.txt, vegeta_geth_eth_<api>.txt
* vegeta_turbo_geth_debug_getModifiedAccountsByNumber.txt, vegeta_turbo_geth_eth_<api>.txt

#### _Workload Activation_

From Silkrpc project directory check the performance test runner usage:
```
$ tests/perf/run_perf_tests.py --help
Usage: ./run_perf_tests.py -p vegetaPatternTarFile -y <api_name>  

Launch an automated performance test sequence on Silkrpc and RPCDaemon using Vegeta


h,--help:                            print this help
-Z,--not-verify-server-alive:         doesn't verify server is still active
-R,--tmp-test-report:                 generate Report on tmp
-u,--test-report:                     generate Report in reports area ready to be inserted into Git repo
-v,--verbose:                         verbose
-x,--tracing:                         verbose and tracing
-e,--empty-cache:                     empty cache
-C,--max-connections <conn>:                                                                             [default: 9000]
-D,--testing-daemon <string>:         name of testing daemon
-b,--blockchain <chain name>:         mandatory in case of -R or -u
-y,--test-type <test-type>:           eth_call, eth_getLogs, ...                                         [default: eth_getLogs]
-m,--test-mode <0,1,2>:               silkworm(1), erigon(2), both(3)                                    [default: 3]
-p,--pattern-file <file-name>:        path to the request file for Vegeta attack                         [default: ]
-r,--repetitions <number>:            number of repetitions for each element in test sequence (e.g. 10)  [default: 10]
-t,--test-sequence <seq>:             list of qps/time as <qps1>:<t1>,... (e.g. 200:30,400:10)           [default: 50:30,1000:30,2500:20,10000:20]
-w,--wait-after-test-sequence <secs>: time interval between successive test iterations in sec            [default: 5]
-d,--rpc-daemon-address <addr>:       address of RPCDaemon (e.g. 192.2.3.1)                              [default: localhost]
-g,--erigon-dir <path>:               path to erigon folder (e.g. /home/erigon)                          [default: ]
-s,--silk-dir <path>:                 path to silk folder (e.g. /home/silkworm)                          [default: ]
-c,--run-vegeta-on-core <...>         taskset format for vegeta (e.g. 0-1:2-3 or 0-2:3-4)                [default: -:-]
-T,--response-timeout <timeout>:      vegeta response timeout                                            [default: 300]
-M,--max-body-rsp <size>:             max number of bytes to read from response bodies                   [default: 1500]
-j,--json-report <file-name>:         generate json report
-P,--more-percentiles:                print more percentiles in console report
```

Results are written on output and in case -R option is specified also in a CSV file `/tmp/<network>/<machine>/<test_type><date_time>_<additional test>_perf.csv`
Results are written on output and in case -u option is specified also in a CSV file in ./reports area  `./reports/<network>/<machine>/<test_type><date_time>_<additional test>_perf.csv`

Invocation examples
./run_perf_tests.py -y eth_call -p pattern/mainnet/stress_test_eth_call_001_14M.tar  
Runs perf eth_call test according input pattern making a default tests sequence (50:30,1000:30,2500:20,10000:30) each sequence is repeated default times (10)

./run_perf_tests.py -y eth_call -p pattern/mainnet/stress_test_eth_call_001_14M.tar  -r 5 -t 50:30,1000:30,2500:20,5000:20
Runs perf eth_call test according input pattern making a tests sequence according the input (50 qps: 30 seconds, ...) each sequence is repeated 5 times

./run_perf_tests.py -y eth_call -p pattern/mainnet/stress_test_eth_call_001_14M.tar  -s /project/silkworm -g /project/erigon -r 3 -R -b mainnet -t 50:30,1000:30
Runs perf eth_call test according input pattern making a tests sequence according the input (50 qps: 30 seconds, ...) each sequence is repeated 3 times 
the report is generated in tmp area(according: chain_name, machine).

./run_perf_tests.py -y eth_call -p pattern/mainnet/stress_test_eth_call_001_14M.tar  -s /project/silkworm -g /project/erigon -r 3 -u -b mainnet -t 50:30,1000:30,2500:20,5000:20
Runs perf eth_call test according input pattern making a tests sequence according the input (50 qps: 30 seconds, ...) each sequence is repeated 3 times 
the report is generated in reports area(according: chain_name, machine) ready to be saved on git repository 



