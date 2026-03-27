# Performance Tests

These are the instructions to execute the RPC performance tests using [Vegeta](https://github.com/tsenart/vegeta/) HTTP load testing.

## Requirements

Build the performance test runner:
```bash
go build -o ./build/bin/rpc_perf ./cmd/perf/main.go
```

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

## Setup

Currently, our setup for performance tests is "all-in-one", executing Erigon Core, Erigon RPCDaemon and/or Silkworm RPCDaemon all on the same host.

### Activation

#### _Erigon Core_
From Erigon project directory:
```
build/bin/erigon --goerli --private.api.addr=localhost:9090
```
#### _Erigon RPCDaemon_
```
./build/bin/rpcdaemon --private.api.addr=localhost:9090 --http.api=eth,debug,net,web3,txpool,trace,erigon,parity,ots --datadir <erigon_data_dir>
```

#### _Silkrpc_
```
./build/cmd/silkrpcdaemon --private.addr 127.0.0.1:9090 --eth.addr 127.0.0.1:51515 --engine.addr 127.0.0.1:51516 --workers 64 --contexts 8 --datadir <erigon_data_dir> --api admin,debug,eth,parity,erigon,trace,web3,txpool,ots,net --log.verbosity 2
```
You *must* specify different ports for Silkrpc (i.e. `--eth.addr 127.0.0.1:51515 --engine.addr 127.0.0.1:51516`) if you want to run Silkrpc simultaneously with Erigon RPCDaemon.

## Test Workload

Currently the performance workload targets the [eth_getLogs, eth_call, eth_getBalance] Ethereum APIs. The test workloads are executed using request files of [Vegeta](https://github.com/tsenart/vegeta/), a HTTP load testing tool.

### Workload Generation

Execute the relevant Erigon bench tool e.g. [bench_ethcall](https://github.com/ledgerwatch/erigon/blob/devel/cmd/rpctest/rpctest/bench_ethcall.go) against both Erigon RPCDaemon and Silkrpc using the following command line:

```
build/bin/rpctest bench_ethcall --erigonUrl http://localhost:8545 --gethUrl http://localhost:51515 --blockFrom 200000 --blockTo 300000
```

Vegeta request files are written to `/tmp/erigon_stress_test`:
* `results_geth_debug_getModifiedAccountsByNumber.csv`, `results_geth_eth_<api>.csv`
* `results_turbo_geth_debug_getModifiedAccountsByNumber.csv`, `results_turbo_geth_<api>.csv`
* `vegeta_geth_debug_getModifiedAccountsByNumber.txt`, `vegeta_geth_eth_<api>.txt`
* `vegeta_turbo_geth_debug_getModifiedAccountsByNumber.txt`, `vegeta_turbo_geth_eth_<api>.txt`

### Workload Activation

```
./build/bin/rpc_perf --help

NAME:
   rpc_perf - Launch an automated sequence of RPC performance tests on target blockchain node(s)

OPTIONS:
   -O, --disable-http-compression          Disable Http compression
   -Z, --not-verify-server-alive           Don't verify server is still active
   -R, --tmp-test-report                   Generate report in tmp directory
   -u, --test-report                       Generate report in reports area ready for Git repo
   -v, --verbose                           Enable verbose output
   -x, --tracing                           Enable verbose and tracing output
   -e, --empty-cache                       Empty OS cache before each test
   -C, --max-connections <value>           Maximum number of connections [default: 9000]
   -D, --testing-client <value>            Name of testing client [default: rpcdaemon]
   -b, --blockchain <value>               Blockchain network name [default: mainnet]
   -y, --test-type <value>                Test type (e.g., eth_call, eth_getLogs) [default: eth_getLogs]
   -p, --pattern-file <value>             Path to the Vegeta attack pattern file
   -r, --repetitions <value>              Number of repetitions for each test in sequence [default: 10]
   -t, --test-sequence <value>            Test sequence as qps:duration,... [default: 50:30,1000:30,2500:20,10000:20]
   -w, --wait-after-test-sequence <value>  Wait time between test iterations in seconds [default: 5]
   -d, --rpc-client-address <value>        Client address [default: localhost]
   -g, --client-build-dir <value>          Path to Client build folder
   -c, --run-vegeta-on-core <value>        Taskset format for Vegeta [default: -:-]
   -T, --response-timeout <value>          Vegeta response timeout [default: 300s]
   -M, --max-body-rsp <value>              Max bytes to read from response bodies [default: 1500]
   -j, --json-report <value>              Generate JSON report at specified path
   -P, --more-percentiles                  Print more percentiles in console report
   -H, --halt-on-vegeta-error              Consider test failed if Vegeta reports any error
   -I, --instant-report                    Print instant Vegeta report for each test
```

Results are written to output and, when `-R` is specified, also to a CSV file at `/tmp/<network>/<machine>/<test_type><date_time>_<additional_test>_perf.csv`.
When `-u` is specified, the CSV file is written to `./reports/<network>/<machine>/` ready to be committed into the Git repository.

### Invocation examples

Run perf eth_call test with default test sequence (50:30,1000:30,2500:20,10000:20), each repeated 10 times:
```bash
./build/bin/rpc_perf -y eth_call -p pattern/mainnet/stress_test_eth_call_001_14M.tar
```

Run with custom test sequence and 5 repetitions:
```bash
./build/bin/rpc_perf -y eth_call -p pattern/mainnet/stress_test_eth_call_001_14M.tar -r 5 -t 50:30,1000:30,2500:20,5000:20
```

Run with report generation in tmp area:
```bash
./build/bin/rpc_perf -y eth_call -p pattern/mainnet/stress_test_eth_call_001_14M.tar -g /project/erigon -r 3 -R -b mainnet -t 50:30,1000:30
```

Run with report generation in reports area (for Git):
```bash
./build/bin/rpc_perf -y eth_call -p pattern/mainnet/stress_test_eth_call_001_14M.tar -g /project/erigon -r 3 -u -b mainnet -t 50:30,1000:30,2500:20,5000:20
```

## Legacy Python Runner

<details>
<summary>Python performance test runner (run_perf_tests.py)</summary>

The previous Python-based performance test runner is still available. It requires Python >= 3.10 and the same Vegeta setup.

```bash
python3 ./run_perf_tests.py --help
```

</details>
