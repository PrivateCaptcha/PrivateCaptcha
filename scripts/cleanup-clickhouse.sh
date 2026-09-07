#!/bin/bash
# Removes the fixed ClickHouse resources used by local integration tests.

set -euo pipefail

DB_NAME="${1:-privatecaptcha}"
USER_NAME="${2:-captchasrv}"
ROLE_SUFFIX="${3:-}"
ROLE_NAME="pc_backend_role${ROLE_SUFFIX}"
CH_HOST="${CH_HOST:-localhost}"
CH_PORT="${CH_PORT:-9000}"
CH_ADMIN_PASSWORD="${CH_ADMIN_PASSWORD:-}"

for identifier in "$DB_NAME" "$USER_NAME" "$ROLE_NAME"; do
    if [[ ! "$identifier" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]]; then
        echo "Error: invalid ClickHouse identifier: $identifier" >&2
        exit 1
    fi
done

DEFAULT_CLICKHOUSE_CLIENT_CMD="clickhouse-client --host ${CH_HOST} --port ${CH_PORT}"
if [[ -n "$CH_ADMIN_PASSWORD" ]]; then
    DEFAULT_CLICKHOUSE_CLIENT_CMD+=" --password ${CH_ADMIN_PASSWORD}"
fi

CLICKHOUSE_CLIENT_CMD=${CLICKHOUSE_CLIENT_CMD:-"$DEFAULT_CLICKHOUSE_CLIENT_CMD"}

echo "=== Cleaning up Local ClickHouse Test Database ==="
$CLICKHOUSE_CLIENT_CMD -n <<-EOSQL
SYSTEM DROP QUERY CACHE;
DROP USER IF EXISTS ${USER_NAME};
DROP ROLE IF EXISTS ${ROLE_NAME};
DROP DATABASE IF EXISTS ${DB_NAME} SYNC;
EOSQL
echo "=== ClickHouse Cleanup Complete ==="
