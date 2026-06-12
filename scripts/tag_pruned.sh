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

  # Add "@pruned" with a text edit only (jq would re-print/reformat the whole
  # file). Three cases, mirroring the old jq logic:
  if grep -q '"@pruned"' "$file"; then
    return 0                                              # already tagged
  elif grep -q '"tags"' "$file"; then
    perl -0777 -pi -e 's/("tags"[ \t]*:[ \t]*\[)/$1 "\@pruned",/' "$file"    # append
  else
    perl -0777 -pi -e 's/^([ \t]*)("test"[ \t]*:[ \t]*\{)/$1$2\n$1    "tags": ["\@pruned"],/m' "$file"  # create
  fi

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

      eth_getLogs|eth_getLogs@forked_block|\
      eth_getUncleCountByBlockHash|eth_getUncleByBlockHashAndIndex|\
      eth_getTransactionCount|eth_getTransactionByBlockNumberAndIndex|\
      eth_getTransactionByBlockHashAndIndex|eth_getRawTransactionByHash|\
      eth_getRawTransactionByBlockHashAndIndex|eth_getRawTransactionByBlockNumberAndIndex|\
      eth_getBlockTransactionCountByHash|eth_getBlockTransactionCountByNumber)
        tag_file "$file" ;;
    esac
  done
done

echo "Tagged $TAGGED files."