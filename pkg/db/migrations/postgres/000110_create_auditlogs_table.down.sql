DROP INDEX IF EXISTS index_audit_logs_entity;

DROP INDEX IF EXISTS index_audit_logs_user_created_at;

DROP TABLE IF EXISTS backend.audit_logs;

DROP TYPE backend.audit_log_action;
