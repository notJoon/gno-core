#!/bin/bash
# Sweep PebbleDB cache sizes for Get benchmark.
#
# Usage:
#   ./gnovm/cmd/benchstore/bench_cache_sweep.sh -keys 100000000 -caches 500,1024,2048,4096,8192

set -e

KEYS=100000000
CACHES="500,1024,2048,4096,8192"

while [[ $# -gt 0 ]]; do
    case $1 in
        -keys)   KEYS="$2"; shift 2;;
        -caches) CACHES="$2"; shift 2;;
        *) echo "Usage: $0 [-keys N] [-caches MB,MB,...]"; exit 1;;
    esac
done

echo "=== PebbleDB Get cache sweep: keys=$KEYS caches=${CACHES}MB ==="
echo ""

IFS=',' read -ra CACHE_LIST <<< "$CACHES"
for mb in "${CACHE_LIST[@]}"; do
    echo "--- cache=${mb}MB ---"
    go test ./gnovm/cmd/benchstore/ \
        -bench="PebbleGet/keys=${KEYS}" \
        -benchmem -benchtime=5s -timeout=2h -count=1 \
        -cache-mb="$mb" -max-keys="$KEYS" 2>&1
    echo ""
done
