#!/bin/bash
# Creates an isolated local Postgres database and user for one integration test run.
# Does NOT create schemas - that's done by init scripts.
# Usage: ./provision-postgres.sh [test_db_id]
#
# Environment variables:
#   PGPASSWORD       - Postgres admin password (required by psql)
#   PG_ADMIN_USER    - Postgres admin user (default: postgres)
#   PG_HOST          - Postgres host (default: localhost)
#   PG_PORT          - Postgres port (default: 5432)
#
# Outputs env file: .postgres-test-env

set -euo pipefail

# Prevent passwords from being saved in shell history
HISTCONTROL=ignorespace
export HISTIGNORE

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

make_safe_id() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -c '[:alnum:]' '_' | sed -E 's/^_+//; s/_+$//; s/_+/_/g; s/^$/local/' | cut -c 1-40
}

current_branch() {
    git branch --show-current 2>/dev/null || git rev-parse --abbrev-ref HEAD 2>/dev/null || basename "$REPO_ROOT"
}

TEST_DB_ID="${1:-${POSTGRES_TEST_RUN_ID:-}}"
if [ -z "$TEST_DB_ID" ]; then
    TEST_DB_ID="$(make_safe_id "$(current_branch)")_$(openssl rand -hex 4)"
else
    TEST_DB_ID="$(make_safe_id "$TEST_DB_ID")"
fi

PC_DB_NAME="pc_test_${TEST_DB_ID}"
PC_DB_USER="pc_test_user_${TEST_DB_ID}"
PC_DB_PASSWORD=$(openssl rand -base64 16 | tr -d '/+=' | head -c 16)

PG_ADMIN_USER="${PG_ADMIN_USER:-postgres}"
PG_HOST="${PG_HOST:-localhost}"
PG_PORT="${PG_PORT:-5432}"
PG_DATABASE="${PG_DATABASE:-postgres}"

echo "=== Provisioning Local Postgres Test Database ==="
echo "Run ID: $TEST_DB_ID"
echo "Database: $PC_DB_NAME"
echo "User: $PC_DB_USER"
echo ""

# This allows for smth like host-based auth
# ssh host -- 'export PSQL_CMD="sudo -u postgres psql"; bash -s' < path/to/entrypoint.sh
PSQL_CMD=${PSQL_CMD:-"psql -h ${PG_HOST} -p ${PG_PORT} -U ${PG_ADMIN_USER} -d ${PG_DATABASE}"}

$PSQL_CMD -v ON_ERROR_STOP=1 <<-EOSQL
CREATE DATABASE ${PC_DB_NAME};
CREATE USER ${PC_DB_USER} WITH ENCRYPTED PASSWORD '${PC_DB_PASSWORD}';
EOSQL

# Write env file for test scripts to source.
ENV_FILE="${REPO_ROOT}/.postgres-test-env"
cat > "$ENV_FILE" <<EOF
POSTGRES_TEST_RUN_ID=${TEST_DB_ID}
PC_DB_NAME=${PC_DB_NAME}
PC_DB_USER=${PC_DB_USER}
PC_DB_PASSWORD=${PC_DB_PASSWORD}
PC_POSTGRES_HOST=${PG_HOST}
PC_POSTGRES_PORT=${PG_PORT}
PC_POSTGRES_DB=${PC_DB_NAME}
PC_POSTGRES=postgres://${PC_DB_USER}:${PC_DB_PASSWORD}@${PG_HOST}:${PG_PORT}/${PC_DB_NAME}?search_path=backend
PC_POSTGRES_BACKEND=postgres://${PC_DB_USER}:${PC_DB_PASSWORD}@${PG_HOST}:${PG_PORT}/${PC_DB_NAME}?search_path=backend
EOF

echo "=== Postgres Provisioning Complete ==="
echo "Environment file: $ENV_FILE"
