CREATE TABLE IF NOT EXISTS properties(
    id SERIAL PRIMARY KEY,
    external_id UUID DEFAULT gen_random_uuid(),
    user_id INTEGER REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp
);

CREATE UNIQUE INDEX IF NOT EXISTS index_property_external_id ON properties(external_id);
