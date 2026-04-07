ALTER TABLE backend.user_notifications ADD COLUMN persistent BOOL NOT NULL DEFAULT false;

UPDATE backend.user_notifications SET persistent = true WHERE persist_until IS NOT NULL;
