#!/bin/bash

set -e

echo "Started in $PWD"

# echo "Compiling... (GOCACHE=$GOCACHE)"
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -c -o tests/ ./...

echo "Migrating..."
./bin/server -mode migrate
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go run cmd/server/main.go -mode migrate

TESTS_DIR="tests"

if [ ! -d "$TESTS_DIR" ]; then
    echo "Tests directory does not exist"
    exit 1
fi

if [ -z "$(ls -A $TESTS_DIR)" ]; then
    echo "No tests available"
    exit 2
fi

for f in `ls $TESTS_DIR/`; do
    echo "Running $f..."
    ./$TESTS_DIR/$f -test.v -test.parallel 1
done

echo "Success"
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -p 1 -v ./... -run "^TestLock"
