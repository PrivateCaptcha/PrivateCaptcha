ALTER TABLE backend.difficulty_rules ADD COLUMN creator_id INT REFERENCES backend.users(id) ON DELETE SET NULL;
