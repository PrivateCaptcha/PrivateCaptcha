ALTER TABLE backend.user_notifications ADD COLUMN persist_until TIMESTAMPTZ DEFAULT NULL;

-- Backfill: set persist_until to far future for existing persistent=true rows
UPDATE backend.user_notifications SET persist_until = NOW() + INTERVAL '100 years' WHERE persistent = true;

-- Set default for persistent column so we can omit it from new INSERTs
ALTER TABLE backend.user_notifications ALTER COLUMN persistent SET DEFAULT false;

-- Drop the old partial unique index and create a new one.
-- Only unprocessed rows are covered by the index. Persist-until dedup is handled
-- at the application level via NOT EXISTS in the INSERT query.
DROP INDEX IF EXISTS backend.index_unique_reference_per_user;

CREATE UNIQUE INDEX index_unique_reference_per_user
ON backend.user_notifications (user_id, reference_id)
WHERE processed_at IS NULL;
