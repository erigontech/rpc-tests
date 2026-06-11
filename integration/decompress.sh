#!/usr/bin/env bash
set -euo pipefail

folder="${1:?Usage: $0 <folder>}"

find "$folder" -maxdepth 1 -type f -name "*.tar" -print0 | while IFS= read -r -d '' file; do
    base=$(basename "$file" .tar)
    inner=$(tar -tf "$file" | head -1)
    tar -xf "$file" -C "$folder"
    [[ "$folder/$inner" != "$folder/${base}.json" ]] && mv "$folder/$inner" "$folder/${base}.json"
    rm "$file"
    echo "Extracted: ${base}.json"
done
