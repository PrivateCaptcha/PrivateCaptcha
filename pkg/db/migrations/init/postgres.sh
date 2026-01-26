#!/bin/bash
# Postgres initialization script for PrivateCaptcha
# Usage: ./postgres.sh [db_name] [user_name] [user_password]
# Or with environment variables: PC_DB_NAME, PC_DB_USER, PC_DB_PASSWORD
#
# Defaults match Docker setup for backward compatibility
# Requires: PGPASSWORD set or ~/.pgpass configured for admin access

set -euo pipefail

# Prevent passwords from being saved in shell history
HISTCONTROL=ignorespace
export HISTIGNORE

# this allows for smth like host-based auth
# ssh host -- 'export PSQL_CMD="sudo -u postgres psql"; bash -s' < path/to/entrypoint.sh
 PSQL_CMD=${PSQL_CMD:-"psql"}

 DB_NAME="${1:-${PC_DB_NAME:-privatecaptcha}}"
 USER_NAME="${2:-${PC_DB_USER:-captchasrv}}"
 USER_PASSWORD="${3:-${PC_DB_PASSWORD:-QMS0fJmTHS8Gzq}}"

# Note: space before psql commands with passwords prevents shell history logging
 $PSQL_CMD -v ON_ERROR_STOP=1 <<-EOSQL
\set HISTCONTROL ignorespace
\c ${DB_NAME}
-- Create user if not exists (DO block for conditional execution)
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '${USER_NAME}') THEN
        -- Space-prefixed in bash, but SQL doesn't have history concerns
        CREATE USER ${USER_NAME} WITH ENCRYPTED PASSWORD '${USER_PASSWORD}';
    ELSE
        -- Update password if user exists
        ALTER USER ${USER_NAME} WITH ENCRYPTED PASSWORD '${USER_PASSWORD}';
    END IF;
END
\$\$;

CREATE SCHEMA IF NOT EXISTS backend;

REVOKE ALL ON SCHEMA backend FROM public;

GRANT USAGE ON SCHEMA backend TO ${USER_NAME};

-- NOTE: it is assumed 'FOR ROLE current_user' when altering default privileges below
-- same role must create tables and be schema owner
ALTER DEFAULT PRIVILEGES IN SCHEMA backend GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ${USER_NAME};
ALTER DEFAULT PRIVILEGES IN SCHEMA backend GRANT USAGE, UPDATE ON SEQUENCES TO ${USER_NAME};
ALTER DEFAULT PRIVILEGES IN SCHEMA backend GRANT EXECUTE ON ROUTINES TO ${USER_NAME};
ALTER DEFAULT PRIVILEGES IN SCHEMA backend GRANT USAGE ON TYPES TO ${USER_NAME};

ALTER USER ${USER_NAME} SET search_path TO backend;
EOSQL
