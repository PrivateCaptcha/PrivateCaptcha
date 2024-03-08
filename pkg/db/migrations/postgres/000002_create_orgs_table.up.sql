CREATE TABLE IF NOT EXISTS organizations(
    id SERIAL PRIMARY KEY,
    org_name VARCHAR(255) NOT NULL,
    user_id INTEGER REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp,
    deleted_at TIMESTAMPTZ NULL DEFAULT NULL
);
