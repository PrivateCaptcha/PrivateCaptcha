CREATE TYPE backend.challenge_type AS ENUM ('blake2b', 'argon2id');

ALTER TABLE backend.properties ADD COLUMN challenge backend.challenge_type NOT NULL DEFAULT 'blake2b';
