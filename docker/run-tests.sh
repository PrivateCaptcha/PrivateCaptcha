#!/bin/bash

set -e

export GORACE="halt_on_error=1"

echo "Started in $PWD"

# echo "Compiling... (GOCACHE=$GOCACHE)"
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -c -o tests/ ./...

TESTS_DIR="tests"
COVERAGE_DIR="coverage_reports"
TEST_NAME="${TEST_NAME:-}"

if [ ! -d "$TESTS_DIR" ]; then
    echo "Tests directory does not exist"
    exit 1
fi

if [ -z "$(ls -A $TESTS_DIR)" ]; then
    echo "No tests available"
    exit 2
fi

mkdir -p "$COVERAGE_DIR"

for f in `ls $TESTS_DIR/`; do
    echo "Running $f..."
    if [ -n "$TEST_NAME" ]; then
        ./$TESTS_DIR/$f -test.v -test.parallel 1 -test.coverprofile="$COVERAGE_DIR/$f.cov" -test.run "^${TEST_NAME}$"
    else
        ./$TESTS_DIR/$f -test.v -test.parallel 1 -test.coverprofile="$COVERAGE_DIR/$f.cov"
    fi
done

echo "Success"
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -p 1 -v ./... -run "^TestLock"
