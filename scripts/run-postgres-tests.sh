#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

make_safe_id() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -c '[:alnum:]' '_' | sed -E 's/^_+//; s/_+$//; s/_+/_/g; s/^$/local/' | cut -c 1-40
}

current_branch() {
    git branch --show-current 2>/dev/null || git rev-parse --abbrev-ref HEAD 2>/dev/null || basename "$REPO_ROOT"
}

TEST_RUN_ID="$(make_safe_id "$(current_branch)")_$(openssl rand -hex 4)"

cleanup() {
    if [ "${KEEP_POSTGRES_TEST_DB:-}" = "1" ]; then
        echo "Keeping Postgres test database because KEEP_POSTGRES_TEST_DB=1"
        return
    fi
    "$SCRIPT_DIR/cleanup-postgres.sh" "$TEST_RUN_ID"
}

pushd "$REPO_ROOT"

export PG_ADMIN_USER="${PG_ADMIN_USER:-postgres}"
export PGPASSWORD="${PGPASSWORD:-postgres}"
export PG_HOST="${PG_HOST:-localhost}"
export PG_PORT="${PG_PORT:-5432}"

trap cleanup EXIT
"$SCRIPT_DIR/provision-postgres.sh" "$TEST_RUN_ID"

ENV_FILE="${REPO_ROOT}/.postgres-test-env-${TEST_RUN_ID}"
source "$ENV_FILE"

echo "=== Initializing Postgres Test Database ==="
PGHOST="$PG_HOST" \
PGPORT="$PG_PORT" \
PGUSER="$PG_ADMIN_USER" \
PGPASSWORD="$PGPASSWORD" \
PGDATABASE="$PC_DB_NAME" \
PGOPTIONS="--search_path=public" \
pkg/db/migrations/init/postgres.sh "$PC_DB_NAME" "$PC_DB_USER" "$PC_DB_PASSWORD"

echo "=== Migrating Postgres Test Database ==="
PC_POSTGRES="postgres://${PG_ADMIN_USER}:${PGPASSWORD}@${PG_HOST}:${PG_PORT}/${PC_DB_NAME}?search_path=public" \
PC_CLICKHOUSE_OPTIONAL="true" \
PC_DOMAIN="privatecaptcha.local" \
PC_ADMIN_EMAIL="admin@privatecaptcha.local" \
./bin/server -mode migrate -migrate-hash ignore

echo "=== Running Integration Tests ==="
PC_POSTGRES="$PC_POSTGRES_BACKEND" \
PC_CLICKHOUSE_OPTIONAL="true" \
PC_ADMIN_EMAIL="admin@privatecaptcha.local" \
PC_USER_FINGERPRINT_KEY="ea3ad6863f0ba598c01bb561eda18c24fa72b75629baed833fb92a7fde29a5dd3ce1cbd466e5c0a2762034b43127bb11a4dd86f1c8ea3c24ea70da21f5b2201c" \
PC_RATE_LIMIT_HEADER="X-REAL-IP" \
PC_REGISTRATION_ALLOWED="1" \
./docker/run-tests.sh

popd