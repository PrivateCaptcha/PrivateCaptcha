#!/bin/bash

echo "Compiling..."
env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -c -o tests/ ./...

echo "Running..."
env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -p 1 -v ./... # -run "^TestBackfillLevels"
