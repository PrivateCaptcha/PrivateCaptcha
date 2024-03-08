CREATE TABLE IF NOT EXISTS users(
    id SERIAL PRIMARY KEY,
    user_name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp,
    deleted_at TIMESTAMPTZ NULL DEFAULT NULL
);
