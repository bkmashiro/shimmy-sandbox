#!/bin/bash
# Builds shimmy_filter.so given DYNAMORIO_HOME is set.
# Run from the repo root or from the filter/ directory.
set -e

DYNAMORIO_HOME=${DYNAMORIO_HOME:-/opt/dynamorio}

if [ ! -d "$DYNAMORIO_HOME" ]; then
    echo "ERROR: DYNAMORIO_HOME=$DYNAMORIO_HOME does not exist." >&2
    echo "Run 'shimmy-sandbox setup' or set DYNAMORIO_HOME manually." >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "$SCRIPT_DIR/build"
cd "$SCRIPT_DIR/build"

cmake .. \
    -DDynamoRIO_DIR="$DYNAMORIO_HOME/cmake" \
    -DCMAKE_BUILD_TYPE=Release

make -j"$(nproc)"

cp libshimmy_filter.so "$SCRIPT_DIR/shimmy_filter.so"
echo "Built: $SCRIPT_DIR/shimmy_filter.so"
