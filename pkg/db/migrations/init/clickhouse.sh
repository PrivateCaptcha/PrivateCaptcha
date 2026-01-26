#!/bin/bash
# Usage: ./provision-clickhouse.sh [db_name] [user_name] [user_password] [role_suffix]
# Defaults match Docker setup for backward compatibility
#
# Environment variables (alternative to positional args):
#   CH_DB_NAME, CH_USER_NAME, CH_USER_PASSWORD, CH_ROLE_SUFFIX

set -euo pipefail

# Prevent passwords from being saved in shell history
HISTCONTROL=ignorespace
export HISTIGNORE

DB_NAME="${1:-${CH_DB_NAME:-privatecaptcha}}"
USER_NAME="${2:-${CH_USER_NAME:-captchasrv}}"
 USER_PASSWORD="${3:-${CH_USER_PASSWORD:-uwnhNn4YW01}}"
ROLE_SUFFIX="${4:-${CH_ROLE_SUFFIX:-}}"
CH_HOST="${CH_HOST:-localhost}"
 CH_ADMIN_PASSWORD="${CH_ADMIN_PASSWORD:-}"

ROLE_NAME="pc_backend_role${ROLE_SUFFIX}"

DEFAULT_CLICKHOUSE_CLIENT_CMD="clickhouse-client --host ${CH_HOST}"
if [[ -n "${CH_ADMIN_PASSWORD}" ]]; then
  DEFAULT_CLICKHOUSE_CLIENT_CMD+=" --password ${CH_ADMIN_PASSWORD}"
fi

CLICKHOUSE_CLIENT_CMD=${CLICKHOUSE_CLIENT_CMD:-"$DEFAULT_CLICKHOUSE_CLIENT_CMD"}
 $CLICKHOUSE_CLIENT_CMD -n <<-EOSQL
CREATE DATABASE IF NOT EXISTS ${DB_NAME};

CREATE ROLE IF NOT EXISTS ${ROLE_NAME};
GRANT SELECT, INSERT, DELETE ON ${DB_NAME}.* TO ${ROLE_NAME};
GRANT ALTER DELETE ON ${DB_NAME}.* TO ${ROLE_NAME};
GRANT ALTER UPDATE(_row_exists) ON ${DB_NAME}.* TO ${ROLE_NAME};

-- space: skip line from history
 CREATE USER IF NOT EXISTS ${USER_NAME} IDENTIFIED BY '${USER_PASSWORD}';
GRANT ${ROLE_NAME} TO ${USER_NAME};
EOSQL
