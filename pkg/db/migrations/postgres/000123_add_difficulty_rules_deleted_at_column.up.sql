ALTER TABLE backend.difficulty_rules ADD COLUMN deleted_at TIMESTAMPTZ DEFAULT NULL;

CREATE INDEX IF NOT EXISTS index_difficulty_rules_deleted_at ON backend.difficulty_rules(deleted_at) WHERE deleted_at IS NOT NULL;
