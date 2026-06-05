#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BASE="$SCRIPT_DIR/../integration/mainnet"
NON_PRUNED="latest|pending|earliest|safe|finalized"
TAGGED=0

is_historic() {
  local block="$1"
  [[ -z "$block" || "$block" == "null" ]] && return 1
  echo "$block" | grep -qiE "^($NON_PRUNED)$" && return 1
  local dec
  dec=$(printf "%d" "$block" 2>/dev/null) || return 1
  (( dec < 0x2000000 )) && return 0 || return 1
}

tag_file() {
  local file="$1"

  jq '
    .[0].test.tags |= (
      if . == null then ["@pruned"]
      elif index("@pruned") then .
      else . + ["@pruned"]
      end
    )
  ' "$file" > "${file}.tmp" &&
  mv "${file}.tmp" "$file"

  echo "  tagged  $file"
  (( TAGGED++ )) || true
}

process() {
  local file="$1" expr="$2"
  local block
  block=$(jq -r ".[0].request | $expr // empty" "$file" 2>/dev/null)
  is_historic "$block" && tag_file "$file" || true
}

for method_dir in "$BASE"/eth_*/; do
  method=$(basename "$method_dir")

  for file in "$method_dir"test_*.json; do
    [[ -f "$file" ]] || continue

    case "$method" in
      eth_call|eth_callBundle|eth_createAccessList|eth_estimateGas|\
      eth_feeHistory|eth_simulateV1|\
      eth_getBalance|eth_getCode|eth_getTransactionCount)
        process "$file" ".params[1]" ;;

      eth_callMany)
        process "$file" ".params[1].blockNumber" ;;

      eth_getBlockByNumber|eth_getBlockReceipts|\
      eth_getBlockTransactionCountByNumber|\
      eth_getRawTransactionByBlockNumberAndIndex|\
      eth_getTransactionByBlockNumberAndIndex|\
      eth_getUncleByBlockNumberAndIndex|\
      eth_getUncleCountByBlockNumber)
        process "$file" ".params[0]" ;;

      eth_getProof|eth_getStorageAt)
        process "$file" ".params[2]" ;;

      eth_getLogs)
        tag_file "$file" ;;
    esac
  done
done

echo "Tagged $TAGGED files."