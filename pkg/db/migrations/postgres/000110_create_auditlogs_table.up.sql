CREATE TYPE backend.audit_log_action AS ENUM ('unknown', 'create', 'update', 'softdelete', 'delete', 'recover', 'login', 'logout', 'access');

CREATE TABLE IF NOT EXISTS backend.audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    action backend.audit_log_action NOT NULL,
    entity_id BIGINT,
    entity_table VARCHAR(100) NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    old_value JSONB,
    new_value JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp
);

CREATE INDEX IF NOT EXISTS index_audit_logs_user_created_at
    ON backend.audit_logs (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS index_audit_logs_entity
    ON backend.audit_logs (entity_table, entity_id, created_at DESC);
