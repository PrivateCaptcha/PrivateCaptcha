CREATE TABLE IF NOT EXISTS locks (
    name TEXT PRIMARY KEY,
    data BYTEA,
    expires_at timestamp DEFAULT CURRENT_TIMESTAMP + INTERVAL '1 minute' NOT NULL
);
