CREATE TABLE IF NOT EXISTS backend.apikeys(
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    external_id UUID DEFAULT gen_random_uuid(),
    user_id INTEGER REFERENCES backend.users(id) ON DELETE CASCADE,
    enabled BOOLEAN DEFAULT TRUE,
    requests_per_second FLOAT NOT NULL,
    requests_burst INTEGER NOT NULL,
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    expires_at TIMESTAMPTZ DEFAULT current_timestamp + INTERVAL '2 year',
    notes TEXT DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS index_apikey_external_id ON backend.apikeys(external_id);
