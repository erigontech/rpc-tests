# Integration Test Suite

These integration tests currently available for Goerli testnet must run as non-regression suite

# Requirements

```
% pip3 install -r requirements.txt
```

Currently, `json-diff` and `json-patch-jsondiff` are also required:

## Linux
```
% sudo apt update
% sudo apt install npm
% npm install -g json-diff

% sudo apt install python3-jsonpatch
```

## macOS
```
% brew update
% brew install node
% npm install -g json-diff
```

# Run tests

```
% python3 ./run_tests.py -c -k jwt.hex -b <chain>
```

# Synopsis

```
% python3 ./run_tests.py -h

Usage: ./run_tests.py:

Launch an automated test sequence on Silkworm RpcDaemon (aka Silkrpc) or Erigon RpcDaemon

-h,--help: print this help
-j,--json-diff: use json-diff to make compare (default use diff)
-f,--display-only-fail: shows only failed tests (not Skipped)
-v,--verbose: <verbose_level>
-c,--continue: runs all tests even if one test fails [default: exit at first test fail]
-l,--loops: <number of loops>
-b,--blockchain: [default: mainnet]
-s,--start-from-test: <test_number>: run tests starting from input
-t,--run-single-test: <test_number>: run single test
-d,--compare-erigon-rpcdaemon: send requests also to the reference daemon e.g.: Erigon RpcDaemon
-T,--transport_type: <http,http_comp,https,websocket,websocket_comp>
-k,--jwt: authentication token file
-a,--api-list-with: <apis>: run all tests of the specified API that contains string (e.g.: eth_,debug_)
-A,--api-list: <apis>: run all tests of the specified API that match full name (e.g.: eth_call,eth_getLogs)
-x,--exclude-api-list: exclude API list (e.g.: txpool_content,txpool_status,engine_)
-X,--exclude-test-list: exclude test list (e.g.: 18,22)
-o,--dump-response: dump JSON RPC response
-H,--host: host where the RpcDaemon is located (e.g.: 10.10.2.3)
-p,--port: port where the RpcDaemon is located (e.g.: 8545)
-r,--erigon-rpcdaemon: connect to Erigon RpcDaemon [default: connect to Silkrpc] 
-e,--verify-external-provider: <provider_url> send any request also to external API endpoint as reference
-i,--without-compare-results: send request without compare results

```

# Invoke examples

```
% ./run_tests.py -b mainnet -d -c -v 1
```

Runs all tests on main net chain comparing Silkrpc response with rpcdaemon response, printing each test result

```
% ./run_tests.py -b mainnet -c -v 1
```

Runs all tests on main net chain comparing silkrpc response to response saved on json file, printing each test result

```
% ./run_tests.py -b mainnet -c -a eth_call
```

Runs all tests of eth_call on main net chain comparing silkrpc response with saved json file, printing only failed tests

```
% ./run_tests.py -b mainnet -r -c -a eth_call -t 1
```

Runs test 1 of eth_call on main net chain comparing rpcdaemon response to saved json file, printing only failed tests

% ./run_tests.py -b mainnet -r -c -t 20 -v 1

Runs test number 20 in main net chain using rpcdaemon and compare result with json file, printing each test result

% ./run_tests.py -b mainnet -d -c -X 20 -v 1

Runs all tests (excluding test number 20) on main net chain comparing silkrpc response with rpcdaemon response, printing each test result

% ./run_tests.py -b mainnet -d -c -x eth_call -v 1

Runs all tests (excluding eth_call tests) on main net chain comparing silkrpc response with rpcdaemon response, printing each test result

