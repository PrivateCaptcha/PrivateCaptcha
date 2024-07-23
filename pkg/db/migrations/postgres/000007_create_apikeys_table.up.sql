CREATE TABLE IF NOT EXISTS apikeys(
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    external_id UUID DEFAULT gen_random_uuid(),
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    expires_at TIMESTAMPTZ DEFAULT current_timestamp + INTERVAL '2 year',
    notes TEXT DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS index_apikey_external_id ON apikeys(external_id);
