# Integration Test Suite

These integration tests currently available for Goerli testnet must run as non-regression suite

# Requirements

```
pip3 install -r requirements.txt
```

Currently, `json-diff` and `json-patch-jsondiff` are also required:

## Linux
```
sudo apt update
sudo apt install npm
npm install -g json-diff

sudo apt install python3-jsonpatch
```

## macOS
```
brew update
brew install node
npm install -g json-diff
```

# Run tests

```
python3 ./run_tests.py -c -k jwt.hex -b <chain>
```

# Synopsis

```
python3 ./run_tests.py -h

Usage: ./run_tests.py:

Launch an automated test sequence on Silkworm RpcDaemon (aka Silkrpc) or Erigon RpcDaemon

Usage: ./run_tests.py:

Launch an automated test sequence on Silkworm RpcDaemon (aka Silkrpc) or Erigon RpcDaemon

-h,--help: print this help
-j,--json-diff: use json-diff to make compare [default use json-diff]
-f,--display-only-fail: shows only failed tests (not Skipped) [default: print all] 
-v,--verbose: <verbose_level> 0: no message for each test; 1: print operation result; 2: print request and response message) [default verbose_level 0]
-c,--continue: runs all tests even if one test fails [default: exit at first failed test]
-l,--loops: <number of loops> [default loop 1]
-b,--blockchain: [default: mainnet]
-s,--start-from-test: <test_number>: run tests starting from specified test number [default starts from 1]
-t,--run-test: <test_number>: run single test using global test number (i.e: -t 256 runs 256 test) or test number of one specified APi used in combination with -a or -A (i.e -a eth_getLogs() -t 3: run test 3 of eth_getLogs())
-d,--compare-erigon-rpcdaemon: send requests also to the reference daemon e.g.: Erigon RpcDaemon
-T,--transport_type: <http,http_comp,https,websocket,websocket_comp> [default http]
-k,--jwt: authentication token file (i.e -k /tmp/jwt_file.hex)
-K,--jwt: generate authentication token file and use it (-K /tmp/jwt_file.hex) 
-a,--api-list-with: <apis>: run all tests of the specified API that contains string (e.g.: eth_,debug_)
-A,--api-list: <apis>: run all tests of the specified API that match full name (e.g.: eth_call,eth_getLogs)
-x,--exclude-api-list < list of tested api>: exclude API list (e.g.: txpool_content,txpool_status,engine_)
-X,--exclude-test-list <test-list>: exclude test list (e.g.: 18,22, 18,22 are global test number)
-o,--dump-response: dump JSON RPC response even if the response are the same
-H,--host: host where the RpcDaemon is located (e.g.: 10.10.2.3)
-p,--port: port where the RpcDaemon is located (e.g.: 8545)
-I,--silk-port: Use 51515/51516 ports to server
-e,--verify-external-provider: <provider_url> send any request also to external API endpoint as reference
-i,--without-compare-results: send request and waits response without compare results (used only to see the response time to execuet one api or more apis)
-w,--waiting_time: waiting after test execution (millisec) (can be used only for serial test see -S)
-S,--serial: all tests are runned in serial way [default: the seleceted files are runned in parallel] 


```

# Invoke examples

```
./run_tests.py -c -f
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

Runs all tests on main net in parallel way; and compare the response with the saved json response; it hows only failed test (not success and skipped test)
---------------------------------------------------------------------------------------------------------------------------------------------------------------

```
./run_tests.py -c -f -S
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

Runs all tests on main net in serial way; and compare the response with the saved json response; shows only failed test (not success and skipped test)
---------------------------------------------------------------------------------------------------------------------------------------------------------------

```
./run_tests.py -c -A eth_getLogs
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

Runs all tests of eth_getLogs() on main net in parallel way; and compare the response with the saved json; printing only failed and skipped tests 
---------------------------------------------------------------------------------------------------------------------------------------------------------------

```
./run_tests.py -c -A eth_getLogs -t 1 -v 1
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

Runs test 1 of eth_getLogs() on main net; and compare the response with the saved json; printing test result 
---------------------------------------------------------------------------------------------------------------------------------------------------------------

```
./run_tests.py -c -A eth_call -t 5 -l 3 -v 1
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

Runs tests 5 of eth_call 3 times on main net in parallel way; and compare the response with the saved json; printing only failed and skipped tests 
---------------------------------------------------------------------------------------------------------------------------------------------------------------


```
./run_tests.py  -c -t 246 -v 1
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

Runs global test 246(debug_trace_transaction test 46) on main net; and compare the response with the saved json; printing test result 
---------------------------------------------------------------------------------------------------------------------------------------------------------------

```
./run_tests.py -c -X 335,336,337
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

Runs all tests (excluding global test number 181) on main net chain in parallel way comparing the response with saved response file
---------------------------------------------------------------------------------------------------------------------------------------------------------------

```
./run_tests.py -c -x engine_,admin_,eth_getLogs/test_05
Run tests in parallel on localhost:8545/localhost:8551
0001. http           ::admin_nodeInfo/test_01.json                                  Skipped
0002. http           ::admin_peers/test_01.json                                     Skipped
0335. http           ::engine_exchangeCapabilities/test_1.json                      Skipped
0336. http           ::engine_forkchoiceUpdatedV1/test_01.json                      Skipped
0337. http           ::engine_forkchoiceUpdatedV2/test_01.json                      Skipped
0338. http           ::engine_getClientVersionV1/test_1.json                        Skipped
0339. http           ::engine_getPayloadBodiesByHashV1/test_01.json                 Skipped
0340. http           ::engine_getPayloadBodiesByHashV1/test_02.json                 Skipped
0341. http           ::engine_getPayloadBodiesByRangeV1/test_01.json                Skipped
0342. http           ::engine_getPayloadBodiesByRangeV1/test_02.json                Skipped
0343. http           ::engine_getPayloadBodiesByRangeV1/test_03.json                Skipped
0344. http           ::engine_getPayloadV1/test_01.json                             Skipped
0345. http           ::engine_getPayloadV2/test_01.json                             Skipped
0346. http           ::engine_newPayloadV1/test_01.json                             Skipped
0347. http           ::engine_newPayloadV2/test_01.json                             Skipped
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

Runs all tests (excluding tests with engin_, admin_ and eth_getLogs/test_05) on main net chain in parallel way comparing response with expected json response, printing failed and skiped test
---------------------------------------------------------------------------------------------------------------------------------------------------------------

