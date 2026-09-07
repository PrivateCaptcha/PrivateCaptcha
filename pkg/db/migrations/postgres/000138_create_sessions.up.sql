CREATE TYPE backend.session_state AS ENUM (
    'pending',
    'authenticated',
    'revoked'
);

CREATE TYPE backend.session_challenge_kind AS ENUM (
    'sign_in',
    'registration',
    'email_change'
);

CREATE TABLE backend.sessions (
    session_id TEXT PRIMARY KEY,
    state backend.session_state NOT NULL,
    version INT NOT NULL DEFAULT 1,
    user_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    data BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,

    challenge_kind backend.session_challenge_kind,
    challenge_code TEXT,
    challenge_email TEXT,
    challenge_expires_at TIMESTAMPTZ,
    failed_attempts INT NOT NULL DEFAULT 0,

    verify_registration BOOL DEFAULT FALSE,
    registration_invite_id INT
);

CREATE INDEX sessions_expires_at_idx ON backend.sessions (expires_at);
CREATE INDEX sessions_user_id_idx ON backend.sessions (user_id);
