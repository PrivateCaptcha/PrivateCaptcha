#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="$REPO_ROOT/.postgres-test-env"

cleanup() {
    if [ "${KEEP_POSTGRES_TEST_DB:-}" = "1" ]; then
        echo "Keeping Postgres test database because KEEP_POSTGRES_TEST_DB=1"
        return
    fi
    "$SCRIPT_DIR/cleanup-postgres.sh"
}

cd "$REPO_ROOT"

export PG_ADMIN_USER="${PG_ADMIN_USER:-postgres}"
export PGPASSWORD="${PGPASSWORD:-postgres}"
export PG_HOST="${PG_HOST:-localhost}"
export PG_PORT="${PG_PORT:-5432}"

"$SCRIPT_DIR/provision-postgres.sh"
trap cleanup EXIT

source "$ENV_FILE"

POSTGRES_ADMIN_URL="postgres://${PG_ADMIN_USER}:${PGPASSWORD}@${PG_HOST}:${PG_PORT}/${PC_DB_NAME}?sslmode=disable&search_path=public"
POSTGRES_TEST_URL="$PC_POSTGRES"

echo "=== Initializing Postgres Test Database ==="
PGHOST="$PG_HOST" \
PGPORT="$PG_PORT" \
PGUSER="$PG_ADMIN_USER" \
PGPASSWORD="$PGPASSWORD" \
PGDATABASE="$PC_DB_NAME" \
PGOPTIONS="--search_path=public" \
pkg/db/migrations/init/postgres.sh "$PC_DB_NAME" "$PC_DB_USER" "$PC_DB_PASSWORD"

echo "=== Migrating Postgres Test Database ==="
PC_POSTGRES="$POSTGRES_ADMIN_URL" \
PC_CLICKHOUSE_OPTIONAL="true" \
PC_DOMAIN="privatecaptcha.local" \
PC_ADMIN_EMAIL="admin@privatecaptcha.local" \
PC_VERBOSE="1" \
./bin/server -mode migrate -migrate-hash ignore

echo "=== Running Integration Tests ==="
PC_POSTGRES="$POSTGRES_TEST_URL" \
PC_CLICKHOUSE_OPTIONAL="true" \
PC_ADMIN_EMAIL="admin@privatecaptcha.local" \
PC_USER_FINGERPRINT_KEY="ea3ad6863f0ba598c01bb561eda18c24fa72b75629baed833fb92a7fde29a5dd3ce1cbd466e5c0a2762034b43127bb11a4dd86f1c8ea3c24ea70da21f5b2201c" \
PC_RATE_LIMIT_HEADER="X-REAL-IP" \
PC_REGISTRATION_ALLOWED="1" \
./docker/run-tests.sh
