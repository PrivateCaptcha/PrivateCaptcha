DROP INDEX IF EXISTS backend.index_unique_reference_per_user;

CREATE UNIQUE INDEX index_unique_reference_per_user
ON backend.user_notifications (user_id, reference_id)
WHERE (persistent = true) OR (processed_at IS NULL);

ALTER TABLE backend.user_notifications DROP COLUMN IF EXISTS persist_until;
