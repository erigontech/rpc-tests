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
  (( dec < 0xFFFFFFFF )) && return 0 || return 1
}

tag_file() {
  local file="$1"

  if grep -q '"@pruned"' "$file"; then
    return 0
  elif grep -q '"tags"' "$file"; then
    perl -0777 -pi -e 's/("tags"[ \t]*:[ \t]*\[)/$1 "\@pruned",/' "$file"
  elif grep -q '"test"' "$file"; then
    perl -0777 -pi -e 's/^([ \t]*)("test"[ \t]*:[ \t]*\{)/$1$2\n$1    "tags": ["\@pruned"],/m' "$file"
  else
    perl -0777 -pi -e '
      s/\[\s*\{\s*/[
    {
        "test": {
            "tags": ["\@pruned"]
        },
/s
    ' "$file"
  fi

  echo "  tagged  $file"
  (( TAGGED++ )) || true
}

process() {
  local file="$1" expr="$2"
  local block
  block=$(jq -r ".[0].request | $expr // empty" "$file" 2>/dev/null)

  if ! is_historic "$block"; then
    tag_file "$file"
  fi
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

      eth_baseFee|eth_chainId|eth_getFilterChanges|\
      eth_getWork|eth_mining|eth_protocolVersion|eth_syncing|\
      eth_sendRawTransaction|eth_submitHashrate|eth_submitWork)
        tag_file "$file" ;;
    esac
  done
done

echo "Tagged $TAGGED files."