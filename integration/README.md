# Integration Test Suite

These integration tests currently available for Goerli testnet must run as non-regression suite

# Requirements

```
% pip3 install -r requirements.txt
```

Currently, `json-diff` is also required:

```
% sudo apt update
% sudo apt install npm
% npm install -g json-diff
```

# Run tests

```
% python3 ./run_tests.py -c -k jwt.hex
```

# Synopsis

```
% python3 ./run_tests.py -h

Usage: ./run_tests.py:

Launch an automated test sequence on Silkworm RpcDaemon (aka Silkrpc) or Erigon RpcDaemon

-h print this help
-f shows only failed tests (not Skipped)
-c runs all tests even if one test fails [default: exit at first test fail]
-r connect to Erigon RpcDaemon [default: connect to Silkrpc] 
-l <number of loops>
-a <test_api>: run all tests of the specified API
-s <start_test_number>: run tests starting from input
-t <test_number>: run single test
-d send requests also to the reference daemon i.e. Erigon RpcDaemon
-i <infura_url> send any request also to the Infura API endpoint as reference
-b blockchain [default: goerly]
-v <verbose_level>
-o dump response
-k authentication token file
-x exclude API list (i.e. txpool_content,txpool_status,engine_
-X exclude test list (i.e. 18,22
-H host where the RpcDaemon is located (e.g. 10.10.2.3)
-p port where the RpcDaemon is located (e.g. 8545)

```

# Invoke examples

% ./run_tests.py -b mainnet -d -c -v 1

Runs all tests on main net chain comparing silkrpc response with rpcdaemon response, printing each test result

% ./run_tests.py -b mainnet -c -v 1

Runs all tests on main net chain comparing silkrpc response to response saved on json file, printing each test result

% ./run_tests.py -b mainnet -c -a eth_call

Runs all tests of eth_call on main net chain comparing silkrpc response with saved json file, printing only failed tests

% ./run_tests.py -b mainnet -r -c -a eth_call -t 1

Runs test 1 of eth_call on main net chain comparing rpcdaemon response to saved json file, printing only failed tests

% ./run_tests.py -b mainnet -r -c -t 20 -v 1

Runs test number 20 in main net chain using rpcdaemon and compare result with json file, printing each test result

% ./run_tests.py -b mainnet -d -c -X 20 -v 1

Runs all tests (excluding test number 20) on main net chain comparing silkrpc response with rpcdaemon response, printing each test result

% ./run_tests.py -b mainnet -d -c -x eth_call -v 1

Runs all tests (excluding eth_call tests) on main net chain comparing silkrpc response with rpcdaemon response, printing each test result

