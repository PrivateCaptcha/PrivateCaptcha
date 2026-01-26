#!/bin/bash
# Usage: ./provision-clickhouse.sh [db_name] [user_name] [user_password] [role_suffix]
# Defaults match Docker setup for backward compatibility
#
# Environment variables (alternative to positional args):
#   CH_DB_NAME, CH_USER_NAME, CH_USER_PASSWORD, CH_ROLE_SUFFIX

set -euo pipefail

DB_NAME="${1:-${CH_DB_NAME:-privatecaptcha}}"
USER_NAME="${2:-${CH_USER_NAME:-captchasrv}}"
USER_PASSWORD="${3:-${CH_USER_PASSWORD:-uwnhNn4YW01}}"
ROLE_SUFFIX="${4:-${CH_ROLE_SUFFIX:-}}"
CH_HOST="${CH_HOST:-localhost}"
CH_ADMIN_PASSWORD="${CH_ADMIN_PASSWORD:-}"

ROLE_NAME="pc_backend_role${ROLE_SUFFIX}"

clickhouse-client --host "${CH_HOST}" --password "${CH_ADMIN_PASSWORD}" -n <<EOF
CREATE DATABASE IF NOT EXISTS ${DB_NAME};

CREATE ROLE IF NOT EXISTS ${ROLE_NAME};
GRANT SELECT, INSERT, DELETE ON ${DB_NAME}.* TO ${ROLE_NAME};
GRANT ALTER DELETE ON ${DB_NAME}.* TO ${ROLE_NAME};
GRANT ALTER UPDATE(_row_exists) ON ${DB_NAME}.* TO ${ROLE_NAME};

CREATE USER IF NOT EXISTS ${USER_NAME} IDENTIFIED BY '${USER_PASSWORD}';
GRANT ${ROLE_NAME} TO ${USER_NAME};
EOF
