# Integration Test Suite

These integration tests run as a non-regression suite against a running Ethereum RPC daemon.

## Build

Build the integration test runner:
```bash
go build -o ./build/bin/rpc_int ./cmd/integration/main.go
```

## Run tests

```bash
./build/bin/rpc_int -c -k jwt.hex -b <chain>
```

## Synopsis

```
./build/bin/rpc_int -h

Usage: rpc_int [options]

Launch an automated sequence of RPC integration tests on target blockchain node(s)

Options:
  -h, --help                           print this help
  -j, --json-diff                      use json-diff to make compare [default: use json-diff-go]
  -f, --display-only-fail              shows only failed tests (not Skipped) [default: print all]
  -E, --do-not-compare-error           compare error code only, ignore error message
  -v, --verbose <level>                0: no message; 1: print result; 2: print request/response [default: 0]
  -c, --continue                       runs all tests even if one test fails [default: exit at first failed test]
  -l, --loops <number>                 the number of integration tests loops [default: 1]
  -b, --blockchain <name>              the network to test [default: mainnet]
  -s, --start-from-test <number>       run tests starting from specified test number [default: 1]
  -t, --run-test <number>              run single test using global test number
  -d, --compare-erigon-rpcdaemon       send requests also to the reference daemon e.g.: Erigon RpcDaemon
  -T, --transport-type <type>          http,http_comp,https,websocket,websocket_comp [default: http]
  -k, --jwt <file>                     authentication token file
  -K, --create-jwt <file>              generate authentication token file and use it
  -a, --api-list-with <apis>           run all tests of the specified API that contains string
  -A, --api-list <apis>                run all tests of the specified API that match full name
  -x, --exclude-api-list <list>        exclude API list
  -X, --exclude-test-list <list>       exclude test list
  -o, --dump-response                  dump JSON RPC response even if responses are the same
  -H, --host <host>                    host where the RpcDaemon is located [default: localhost]
  -p, --port <port>                    port where the RpcDaemon is located [default: 8545]
  -P, --engine-port <port>             engine port
  -I, --daemon-port                    use 51515/51516 ports to server
  -e, --verify-external-provider <url> send any request also to external API endpoint as reference
  -i, --without-compare-results        send request and waits response without compare results
  -w, --waiting-time <ms>              wait time after test execution in milliseconds
  -S, --serial                         all tests run in serial way [default: parallel]
  -L, --tests-on-latest-block          runs only test on latest block
  -C, --committed-history              include tests requiring committed history [default: skip]
  -M, --max-failures <n>               stop after n failures, 0 = unlimited [default: 100]
  -R, --report-file <file>             write summary report to file (.csv or .txt)
      --cpuprofile <file>              write cpu profile to file
      --memprofile <file>              write memory profile to file
      --trace <file>                   write execution trace to file

Note:
* In case of authentication, use option -k (--jwt) to read an existing authentication token
  or -K (--create-jwt) to generate a new one.
```

## Invoke examples

### Run all tests in parallel, show only failures

```bash
./build/bin/rpc_int -c -f

Run tests in parallel on localhost:8545/localhost:8551

Test time-elapsed:            0:01:15.715804
Available tests:              1203
Available tested api:         108
Number of loop:               1
Number of executed tests:     1132
Number of NOT executed tests: 72
Number of success tests:      1132
Number of failed tests:       0
```

### Run all tests in serial, show only failures

```bash
./build/bin/rpc_int -c -f -S

Run tests in serial on localhost:8545/localhost:8551

Test time-elapsed:            0:06:14.423804
Available tests:              1203
Available tested api:         108
Number of loop:               1
Number of executed tests:     1132
Number of NOT executed tests: 72
Number of success tests:      1132
Number of failed tests:       0
```

### Run all tests of eth_getLogs in parallel

```bash
./build/bin/rpc_int -c -A eth_getLogs

Run tests in parallel on localhost:8545/localhost:8551
0672. http           ::eth_getLogs/test_16.tar                                      Skipped
0673. http           ::eth_getLogs/test_17.json                                     Skipped
0674. http           ::eth_getLogs/test_18.json                                     Skipped
0675. http           ::eth_getLogs/test_19.tar                                      Skipped
0676. http           ::eth_getLogs/test_20.json                                     Skipped

Test time-elapsed:            0:00:00.125199
Available tests:              1203
Available tested api:         108
Number of loop:               1
Number of executed tests:     15
Number of NOT executed tests: 5
Number of success tests:      15
Number of failed tests:       0
```

### Run test 1 of eth_getLogs with verbose output

```bash
./build/bin/rpc_int -c -A eth_getLogs -t 1 -v 1

Run tests in parallel on localhost:8545/localhost:8551
0657. http           ::eth_getLogs/test_01.json                                       OK

Test time-elapsed:            0:00:00.024762
Available tests:              1203
Available tested api:         108
Number of loop:               1
Number of executed tests:     1
Number of NOT executed tests: 0
Number of success tests:      1
Number of failed tests:       0
```

### Run test 5 of eth_call 3 times with verbose output

```bash
./build/bin/rpc_int -c -A eth_call -t 5 -l 3 -v 1

Run tests in parallel on localhost:8545/localhost:8551

Test iteration:  1
0448. http           ::eth_call/test_05.json                                          OK

Test iteration:  2
0448. http           ::eth_call/test_05.json                                          OK

Test iteration:  3
0448. http           ::eth_call/test_05.json                                          OK

Test time-elapsed:            0:00:00.035965
Available tests:              1203
Available tested api:         108
Number of loop:               3
Number of executed tests:     3
Number of NOT executed tests: 0
Number of success tests:      3
Number of failed tests:       0
```

### Run a single test by global number (e.g. test 246)

```bash
./build/bin/rpc_int -c -t 246 -v 1

Run tests in parallel on localhost:8545/localhost:8551
0246. http           ::debug_traceTransaction/test_46.json                            OK

Test time-elapsed:            0:00:00.023964
Available tests:              1203
Available tested api:         108
Number of loop:               1
Number of executed tests:     1
Number of NOT executed tests: 0
Number of success tests:      1
Number of failed tests:       0
```

### Exclude specific test numbers

```bash
./build/bin/rpc_int -c -X 335,336,337

Run tests in parallel on localhost:8545/localhost:8551
0335. http           ::engine_exchangeCapabilities/test_1.json                      Skipped
0336. http           ::engine_forkchoiceUpdatedV1/test_01.json                      Skipped
0337. http           ::engine_forkchoiceUpdatedV2/test_01.json                      Skipped

Test time-elapsed:            0:00:24.617112
Available tests:              1203
Available tested api:         108
Number of loop:               1
Number of executed tests:     1201
Number of NOT executed tests: 3
Number of success tests:      1198
Number of failed tests:       0
```

### Exclude APIs by pattern

```bash
./build/bin/rpc_int -c -x engine_,admin_,eth_getLogs/test_05

Run tests in parallel on localhost:8545/localhost:8551
0001. http           ::admin_nodeInfo/test_01.json                                  Skipped
0002. http           ::admin_peers/test_01.json                                     Skipped
0335. http           ::engine_exchangeCapabilities/test_1.json                      Skipped
...
0661. http           ::eth_getLogs/test_05.json                                     Skipped

Test time-elapsed:            0:00:28.677763
Available tests:              1203
Available tested api:         108
Number of loop:               1
Number of executed tests:     1188
Number of NOT executed tests: 16
Number of success tests:      1188
Number of failed tests:       0
```

### Stop after too many failures

By default the runner stops after 100 failures to keep result artifacts small. Use `-M` to
override the limit or set it to `0` for unlimited:

```bash
# Stop after 50 failures (stricter than default)
./build/bin/rpc_int -c -f -M 50

# Run all tests regardless of failure count
./build/bin/rpc_int -c -f -M 0
```

When the limit is reached the runner prints:
```
ABORTED: too many failures (100), test sequence stopped early
```

### Run CI tests with Erigon

Assuming you have `erigon` installed beside `rpc-tests`:

```bash
./../../erigon/.github/workflows/scripts/run_rpc_tests_ethereum.sh # for Ethereum mainnet
./../../erigon/.github/workflows/scripts/run_rpc_tests_gnosis.sh   # for Gnosis mainnet
./../../erigon/.github/workflows/scripts/run_rpc_tests_polygon.sh  # for Polygon Bor mainnet
```

## Legacy Python Runner

<details>
<summary>Python integration test runner (run_tests.py)</summary>

The previous Python-based integration test runner is still available. It requires:
* Python >= 3.10
* `pip3 install -r requirements.txt`
* `json-diff` (via npm: `npm install -g json-diff`)
* `python3-jsonpatch` >= 1.32

```bash
python3 ./run_tests.py -c -k jwt.hex -b <chain>
```

Run `python3 ./run_tests.py -h` for full help.

</details>
