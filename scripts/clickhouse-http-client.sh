#!/bin/bash

set -euo pipefail

if [ "$#" -ne 1 ] || [ "$1" != "-n" ]; then
    echo "Usage: clickhouse-http-client.sh -n" >&2
    exit 1
fi

CH_HOST="${CH_HOST:-localhost}"
CH_HTTP_PORT="${CH_HTTP_PORT:-8123}"

while IFS= read -r query; do
    if [[ "$query" =~ ^[[:space:]]*$ ]]; then
        continue
    fi

    curl --fail --silent --show-error --data-binary "$query" "http://${CH_HOST}:${CH_HTTP_PORT}/"
done
