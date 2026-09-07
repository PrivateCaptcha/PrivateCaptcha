#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ClickHouse migrations and runtime queries currently qualify this database name.
DB_NAME="privatecaptcha"
USER_NAME="captchasrv"
USER_PASSWORD="uwnhNn4YW01"
ROLE_SUFFIX=""
LOCK_FILE="${TMPDIR:-/tmp}/privatecaptcha-clickhouse-test.lock"

acquire_lock() {
    if ! command -v flock > /dev/null; then
        echo "Error: flock is required to run local ClickHouse tests" >&2
        return 1
    fi

    exec {LOCK_FD}> "$LOCK_FILE"
    if ! flock -n "$LOCK_FD"; then
        echo "Error: another local ClickHouse test run is active" >&2
        return 1
    fi
}

cleanup() {
    local exit_code="$?"
    trap - EXIT

    if [ "${KEEP_CLICKHOUSE_TEST_DB:-}" = "1" ]; then
        echo "Keeping ClickHouse test database because KEEP_CLICKHOUSE_TEST_DB=1"
    elif ! "$SCRIPT_DIR/cleanup-clickhouse.sh" "$DB_NAME" "$USER_NAME" "$ROLE_SUFFIX"; then
        echo "Warning: failed to clean up ClickHouse test database" >&2
    fi

    exit "$exit_code"
}

pushd "$REPO_ROOT" > /dev/null

export CH_HOST="${CH_HOST:-localhost}"
export CH_PORT="${CH_PORT:-9000}"
export CH_ADMIN_PASSWORD="${CH_ADMIN_PASSWORD:-}"

if ! acquire_lock; then
    exit 1
fi

trap cleanup EXIT

# Fail if stale data cannot be removed; the test database must start clean.
"$SCRIPT_DIR/cleanup-clickhouse.sh" "$DB_NAME" "$USER_NAME" "$ROLE_SUFFIX"

echo "=== Initializing ClickHouse Test Database ==="
pkg/db/migrations/init/clickhouse.sh "$DB_NAME" "$USER_NAME" "$USER_PASSWORD" "$ROLE_SUFFIX"
pkg/db/migrations/tests/clickhouse.sh "$DB_NAME" "$USER_NAME" "$USER_PASSWORD" "$ROLE_SUFFIX"

if [ -z "${POSTGRES_MIGRATION_URL:-}" ]; then
    echo "Error: POSTGRES_MIGRATION_URL is required" >&2
    exit 1
fi

echo "=== Migrating ClickHouse Test Database ==="
PC_POSTGRES="$POSTGRES_MIGRATION_URL" \
PC_CLICKHOUSE_HOST="$CH_HOST" \
PC_CLICKHOUSE_PORT="$CH_PORT" \
PC_CLICKHOUSE_DB="$DB_NAME" \
PC_CLICKHOUSE_USER="default" \
PC_CLICKHOUSE_PASSWORD="$CH_ADMIN_PASSWORD" \
PC_CLICKHOUSE_ADMIN="" \
PC_CLICKHOUSE_ADMIN_PASSWORD="" \
PC_CLICKHOUSE_OPTIONAL="false" \
PC_DOMAIN="privatecaptcha.local" \
PC_ADMIN_EMAIL="admin@privatecaptcha.local" \
./bin/server -mode migrate -migrate-hash ignore

echo "=== Executing Target Command: $* ==="

unset POSTGRES_MIGRATION_URL
export PC_CLICKHOUSE_HOST="$CH_HOST"
export PC_CLICKHOUSE_PORT="$CH_PORT"
export PC_CLICKHOUSE_DB="$DB_NAME"
export PC_CLICKHOUSE_USER="$USER_NAME"
export PC_CLICKHOUSE_PASSWORD="$USER_PASSWORD"
export PC_CLICKHOUSE_OPTIONAL="false"
unset PC_CLICKHOUSE_ADMIN PC_CLICKHOUSE_ADMIN_PASSWORD

"$@"

popd > /dev/null
