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

### Prerequisites

Using `rpc-tests` requires installing:
* [`Vegeta`](https://github.com/tsenart/vegeta) >= 12.8.4
* [`Python`](https://python.org/) >= 3.10
* [`python3-jsonpatch`] >= 1.32 (see README on integration subfolder)
* [`json-diff`] using npm (see README on integration subfolder)

After installation, make sure `vegeta`, `json-diff` and `json-patch-jsondiff ` commands are available at your shell prompt.

After installation, make sure `python3` and `pip3` commands are available at your shell prompt by running `python3 --version` and `pip3 --version`.

### Obtaining `rpc-tests`

To obtain `rpc-tests` source code for the first time:
```
git clone https://github.com/erigontech/rpc-tests.git
cd rpc-tests
```

### Create and activate Python virtual environment

Create your local virtual environment:
```
python3 -m venv .venv
```

Activate it:
```linux
source .venv/bin/activate
```
```windows
.\.venv\Scripts\activate
```

`rpc-tests` uses a few Python third-party libraries, so after you've updated to the latest code with
```
git pull
```
update the dependencies
```
pip3 install -r requirements.txt
```


## Integration Testing

Check out the dedicated guide in [Integration Tests](./integration/README.md).

## Performance Testing

Check out the dedicated guide in [Performance Tests](./perf/README.md).

## Standalone Testing Tools

To run standalone tools:
```
cd src
```

### Get Latest/Safe/Finalized Blocks
```commandline
python3 -m rpctests.block_by_number
```

### Find Empty Blocks
```commandline
python3 -m rpctests.empty_blocks
```

### Query Filter Changes
```commandline
python3 -m rpctests.filter_changes
```

### Get Latest Block Logs
```commandline
python3 -m rpctests.latest_block_logs
```

### GraphQL
```commandline
python3 -m rpctests.graphql
```

### ReplayRequest
```commandline
python3 -m rpctests.replay_request
```

### ReplayTx
```commandline
python3 -m rpctests.replay_tx
```

### Send Raw Transaction Sync
```commandline
python3 -m rpctests.send_raw_transaction_sync
```

### Subscribe For NewHeads And Call Fee History
```commandline
python3 -m rpctests.subscribe_and_call_fee_history
```

### Subscribe For NewHeads And Verify Receipts
```commandline
python3 -m rpctests.subscribe_and_check_receipts
```

### Subscribe And Listen For Notifications
```commandline
python3 -m rpctests.subscriptions
```

## Development

### Unit Tests
```commandline
pytest
```
