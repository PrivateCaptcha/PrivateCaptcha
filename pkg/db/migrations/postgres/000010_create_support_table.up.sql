CREATE TYPE backend.support_category AS ENUM ('question', 'suggestion', 'problem', 'unknown');

CREATE TABLE IF NOT EXISTS backend.support(
    id SERIAL PRIMARY KEY,
    category backend.support_category NOT NULL,
    external_id UUID DEFAULT gen_random_uuid(),
    subject TEXT DEFAULT '',
    message TEXT DEFAULT '',
    session_id TEXT DEFAULT '',
    resolution TEXT DEFAULT '',
    user_id INTEGER REFERENCES backend.users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT current_timestamp
);
