#!/usr/bin/python3
"""Rebase the testing_commitBlockV1 payload timestamps on the node's current head.

testing_commitBlockV1 requires each committed block's timestamp to be greater than
its parent's. The suite commits 4 blocks; scanning the fixtures in file order, each
testing_commitBlockV1 request gets head_timestamp + OFFSETS[i] (the failed commit in
the invalid-transaction scenario reuses the slot of the next successful one, matching
the execution-apis PR #801 fixtures).

Only needed when the chain differs from the default hive chain (head timestamp
0x1c2): on the default chain the checked-in PR timestamps are already correct.
Run against the SAME node you are about to record/compare with:

    ./rebase_timestamps.py [http://localhost:8545]
"""
import glob
import json
import os
import sys
import urllib.request

URL = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8545"
HERE = os.path.dirname(os.path.abspath(__file__))

# Offsets (seconds from the current head) for each testing_commitBlockV1 request
# with payload attributes, in fixture file order:
# empty, invalid (head unchanged), extra-data, with-transactions, from-mempool
OFFSETS = [12, 24, 24, 36, 48]


def head_timestamp():
    req = urllib.request.Request(
        URL,
        data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": "eth_getBlockByNumber",
                         "params": ["latest", False]}).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req) as resp:
        return int(json.load(resp)["result"]["timestamp"], 16)


def main():
    base = head_timestamp()
    print(f"head timestamp: {hex(base)} ({base})")
    slot = 0
    for name in sorted(glob.glob(os.path.join(HERE, "test_*.json"))):
        with open(name) as f:
            fixture = json.load(f)
        changed = False
        for cmd in fixture:
            req = cmd["request"]
            if req["method"] != "testing_commitBlockV1" or not req["params"]:
                continue
            attrs = req["params"][0]
            old = attrs["timestamp"]
            attrs["timestamp"] = hex(base + OFFSETS[slot])
            print(f"{os.path.basename(name)}: {old} -> {attrs['timestamp']}")
            slot += 1
            changed = True
        if changed:
            with open(name, "w") as f:
                json.dump(fixture, f, indent=2)
                f.write("\n")
    assert slot == len(OFFSETS), f"expected {len(OFFSETS)} commit requests, found {slot}"


if __name__ == "__main__":
    main()
