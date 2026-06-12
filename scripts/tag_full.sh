#!/usr/bin/env bash
# Add "@full" tag to every eth_* fixture that a full node can serve — i.e. all
# eth_* methods EXCEPT state-reading queries pinned to a specific historical
# block (those need an archive node and are left untagged).
# Usage: ./scripts/tag_archive.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BASE="$SCRIPT_DIR/../integration/mainnet"
NON_ARCHIVE="latest|pending|earliest|safe|finalized"
TAGGED=0

is_historic() {
  local block="$1"
  [[ -z "$block" || "$block" == "null" ]] && return 1
  echo "$block" | grep -qiE "^($NON_ARCHIVE)$" && return 1
  local dec
  dec=$(printf "%d" "$block" 2>/dev/null) || return 1
  (( dec < 0xFFFFFFFF )) && return 0 || return 1
}

tag_file() {
  local file="$1"

  if grep -q '"@full"' "$file"; then
    return 0
  elif grep -q '"tags"' "$file"; then
    perl -0777 -pi -e 's/("tags"[ \t]*:[ \t]*\[)/$1 "\@full",/' "$file"
  elif grep -q '"test"' "$file"; then
    perl -0777 -pi -e 's/^([ \t]*)("test"[ \t]*:[ \t]*\{)/$1$2\n$1    "tags": ["\@full"],/m' "$file"
  else
    perl -0777 -pi -e '
      s/\[\s*\{\s*/[
    {
        "test": {
            "tags": ["\@full"]
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
  ! is_historic "$block" && tag_file "$file" || true
}

for method_dir in "$BASE"/eth_*/; do
  method=$(basename "$method_dir")

  for file in "$method_dir"test_*.json; do
    [[ -f "$file" ]] || continue

    case "$method" in
      eth_call|eth_callBundle|eth_createAccessList|eth_estimateGas|\
      eth_simulateV1|\
      eth_getBalance|eth_getCode|eth_getTransactionCount)
        process "$file" ".params[1]" ;;

      eth_callMany)
        process "$file" ".params[1].blockNumber" ;;

      eth_getProof|eth_getStorageAt)
        process "$file" ".params[2]" ;;

      # Every other eth_* method runs on a full node -> blanket @full.
      *)
        tag_file "$file" ;;
    esac
  done
done

echo "Tagged $TAGGED files."
