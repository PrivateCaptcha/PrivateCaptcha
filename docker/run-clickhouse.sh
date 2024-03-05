#!/bin/sh

set -e

echo "Running clickhouse..."

docker run -d --rm \
    -p 8123:8123 \
    -p 9000:9000 \
    --ulimit nofile=262144:262144 \
    -e CLICKHOUSE_DB=privatecaptcha \
    -v $(pwd)/docker/clickhouse-config.xml:/etc/clickhouse-server/config.d/myconfig.xml \
    clickhouse/clickhouse-server:23.8.9-alpine

echo "Waiting for clickhouse healthcheck..."

wget --no-verbose --tries=10 --timeout=1 --spider http://localhost:8123/?query=SELECT%201 || exit 1

echo "Done"
