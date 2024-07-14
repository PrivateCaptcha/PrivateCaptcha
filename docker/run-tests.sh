#!/bin/bash

set -e

echo "Compiling..."
env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -c -o tests/ ./...

echo "Migrating..."
env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go run cmd/server/main.go -mode migrate

echo "Running..."
env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -p 1 -v ./... # -run "^TestBackfillLevels"
