#!/bin/bash

set -e

echo "Started in $PWD"

# echo "Compiling... (GOCACHE=$GOCACHE)"
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -c -o tests/ ./...

echo "Migrating..."
./bin/server -mode migrate
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go run cmd/server/main.go -mode migrate

for f in `ls tests/`; do
    echo "Running $f..."
    ./tests/$f -test.v -test.parallel 1
done

echo "Success"
# env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -p 1 -v ./... -run "^TestLock"
