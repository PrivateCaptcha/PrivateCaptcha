#!/bin/sh

set -e

echo "Migrating databases..."

VERBOSE=1 \
    PC_POSTGRES=$PC_POSTGRES_MIGRATE \
    PC_CLICKHOUSE_USER=$PC_CLICKHOUSE_MIGRATE_USER \
    PC_CLICKHOUSE_PASSWORD=$PC_CLICKHOUSE_MIGRATE_PASSWORD \
    /app/server migrate

echo "Migrations finished"

unset PC_POSTGRES_MIGRATE
unset PC_CLICKHOUSE_MIGRATE_USER
unset PC_CLICKHOUSE_MIGRATE_PASSWORD

exec /app/server run
