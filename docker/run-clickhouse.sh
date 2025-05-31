#!/bin/sh

set -e

echo "Running clickhouse..."

docker run -d --rm \
    -p 8123:8123 \
    -p 9000:9000 \
    --ulimit nofile=262144:262144 \
    -e CLICKHOUSE_DB=privatecaptcha \
    -v $(pwd)/docker/clickhouse-config.xml:/etc/clickhouse-server/config.d/myconfig.xml \
    -v $(pwd)/docker/clickhouse-users.xml:/etc/clickhouse-server/users.d/myusers.xml \
    -v $(pwd)/pkg/db/migrations/init/clickhouse.sql:/docker-entrypoint-initdb.d/init.sql \
    clickhouse/clickhouse-server:24.8.7-alpine

echo "Waiting for clickhouse healthcheck..."

wget --no-verbose --tries=10 --timeout=1 -O - http://localhost:8123/?query=SELECT%201 || exit 1

echo "Done"
