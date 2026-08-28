CREATE TYPE backend.session_state AS ENUM (
    'pending',
    'registration_processing',
    'authenticated',
    'revoked'
);

CREATE TYPE backend.session_challenge_kind AS ENUM (
    'sign_in',
    'registration',
    'email_change'
);

CREATE TABLE IF NOT EXISTS backend.sessions(
    session_id TEXT PRIMARY KEY,
    state backend.session_state NOT NULL,
    version INT NOT NULL DEFAULT 1 CHECK (version > 0),
    user_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    data BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    challenge_kind backend.session_challenge_kind,
    challenge_code TEXT,
    challenge_email VARCHAR(255),
    challenge_expires_at TIMESTAMPTZ,
    failed_attempts INT NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0)
);

CREATE INDEX IF NOT EXISTS index_session_expires_at ON backend.sessions(expires_at);
