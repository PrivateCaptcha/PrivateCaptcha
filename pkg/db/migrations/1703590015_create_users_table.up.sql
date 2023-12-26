CREATE TABLE IF NOT EXISTS users(
    id SERIAL PRIMARY KEY,
    external_id UUID DEFAULT uuid_generate_v4(),
    user_name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp
);

CREATE UNIQUE INDEX IF NOT EXISTS index_user_external_id ON users(external_id);
