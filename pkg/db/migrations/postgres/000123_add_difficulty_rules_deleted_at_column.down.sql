DROP INDEX IF EXISTS backend.index_difficulty_rules_deleted_at;

ALTER TABLE backend.difficulty_rules DROP COLUMN IF EXISTS deleted_at;
