#!/bin/bash

set -e

echo "Started in $PWD"

# echo "Compiling... (GOCACHE=$GOCACHE)"
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -c -o tests/ ./...

TESTS_DIR="tests"
COVERAGE_DIR="coverage"
MERGED_COVERAGE_FILE="coverage_integration.out"

if [ ! -d "$TESTS_DIR" ]; then
    echo "Tests directory does not exist"
    exit 1
fi

if [ -z "$(ls -A $TESTS_DIR)" ]; then
    echo "No tests available"
    exit 2
fi

mkdir -p "$COVERAGE_DIR"
echo "mode: atomic" > "$MERGED_COVERAGE_FILE"

for f in `ls $TESTS_DIR/`; do
    echo "Running $f..."
    ./$TESTS_DIR/$f -test.v -test.parallel 1 -test.coverprofile="$COVERAGE_DIR/$f.cov" # -test.run "^TestUniqueJob"

    if [ -f "$COVERAGE_DIR/$f.cov" ]; then
        tail -n +2 "$COVERAGE_DIR/$f.cov" >> "$MERGED_COVERAGE_FILE"
    fi
done

echo "Success"
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -p 1 -v ./... -run "^TestLock"
