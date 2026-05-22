#!/bin/bash
# Local Postgres Database Cleanup Script
# Usage: ./cleanup-postgres.sh

set -euo pipefail

# Prevent passwords from being saved in shell history
HISTCONTROL=ignorespace
export HISTIGNORE

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

ENV_FILE="${REPO_ROOT}/.postgres-test-env"

if [ -f "$ENV_FILE" ]; then
    source "$ENV_FILE"
fi

PG_ADMIN_USER="${PG_ADMIN_USER:-postgres}"
PG_HOST="${PG_HOST:-${PC_POSTGRES_HOST:-localhost}}"
PG_PORT="${PG_PORT:-${PC_POSTGRES_PORT:-5432}}"
PG_DATABASE="${PG_DATABASE:-postgres}"

if [ -z "${PC_DB_NAME:-}" ]; then
    echo "Warning: No Postgres database name found to clean up"
    exit 0
fi

echo "=== Cleaning up Local Postgres Test Database ==="
echo "Database: $PC_DB_NAME"

# This allows for smth like host-based auth
# ssh host -- 'export PSQL_CMD="sudo -u postgres psql"; bash -s' < path/to/entrypoint.sh
PSQL_CMD=${PSQL_CMD:-"psql -h ${PG_HOST} -p ${PG_PORT} -U ${PG_ADMIN_USER} -d ${PG_DATABASE}"}

$PSQL_CMD -v ON_ERROR_STOP=0 <<-EOSQL || true
SELECT pg_terminate_backend(pg_stat_activity.pid)
FROM pg_stat_activity
WHERE pg_stat_activity.datname = '${PC_DB_NAME}'
AND pid <> pg_backend_pid();
EOSQL

$PSQL_CMD -v ON_ERROR_STOP=0 <<-EOSQL || true
DROP DATABASE IF EXISTS ${PC_DB_NAME};
EOSQL

if [ -n "${PC_DB_USER:-}" ]; then
$PSQL_CMD -v ON_ERROR_STOP=0 <<-EOSQL || true
DROP USER IF EXISTS ${PC_DB_USER};
EOSQL
fi

rm -vf "$ENV_FILE"
echo "=== Postgres Cleanup Complete ==="
