# rpc-tests
Collection of JSON RPC black-box testing tools

## Table of Contents
1. ### [Installation](#installation)
    1. [Prerequisites](#prerequisites)
    2. [Obtaining rpc-tests](#obtaining-rpc-tests)
2. ### [Integration Testing](#integration-testing)
3. ### [Performance Testing](#performance-testing)
4. ### [Contributing](#contributing)

## Installation

#### Prerequisites

Using `rpc-tests` requires installing:
* [`Vegeta`](https://github.com/tsenart/vegeta) >= 12.8.4
* [`Python`](https://python.org/) >= 3.10
* [`python3-jsonpatch`] >= 1.32 (see README on integration subfolder)
* [`json-diff`] using npm (see README on integration subfolder)

After installation, make sure `vegeta`, `json-diff` and `json-patch-jsondiff ` commands are available at your shell prompt.

After installation, make sure `python3` and `pip3` commands are available at your shell prompt by running `python3 -h` and `pip3 -h`.

#### Obtaining `rpc-tests`

To obtain `rpc-tests` source code for the first time:
```
git clone https://github.com/erigontech/rpc-tests.git
```

`rpc-tests` uses a few Python third-party libraries, so after you've updated to the latest code with
```
git pull
```
update the dependencies as well by running
```
sudo apt install python3-pip python3-venv
python3 -m venv .
./bin/pip3 install -r requirements.txt
```

## Integration Testing

Check out the dedicated guide in [Integration Tests](./integration/README.md).

## Performance Testing

Check out the dedicated guide in [Performance Tests](./perf/README.md).
