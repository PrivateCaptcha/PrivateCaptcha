ALTER TABLE backend.user_notifications ADD COLUMN persist_until TIMESTAMPTZ DEFAULT NULL;

-- Backfill: set persist_until to far future for existing persistent=true rows
UPDATE backend.user_notifications SET persist_until = NOW() + INTERVAL '100 years' WHERE persistent = true;

-- Drop the old partial unique index and create a new one using persist_until
DROP INDEX IF EXISTS backend.index_unique_reference_per_user;

CREATE UNIQUE INDEX index_unique_reference_per_user
ON backend.user_notifications (user_id, reference_id)
WHERE (persist_until IS NOT NULL) OR (processed_at IS NULL);
