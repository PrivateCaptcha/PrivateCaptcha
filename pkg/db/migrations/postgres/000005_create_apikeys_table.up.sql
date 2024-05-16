CREATE TABLE IF NOT EXISTS apikeys(
    id SERIAL PRIMARY KEY,
    external_id UUID DEFAULT gen_random_uuid(),
    user_id INTEGER REFERENCES users(id),
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    expires_at TIMESTAMPTZ DEFAULT current_timestamp + INTERVAL '2 year',
    deleted_at TIMESTAMPTZ NULL DEFAULT NULL,
    notes TEXT DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS index_apikey_external_id ON apikeys(external_id);
