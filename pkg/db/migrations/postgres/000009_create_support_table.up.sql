CREATE TYPE support_category AS ENUM ('question', 'suggestion', 'problem', 'unknown');

CREATE TABLE IF NOT EXISTS support(
    id SERIAL PRIMARY KEY,
    category support_category NOT NULL,
    external_id UUID DEFAULT gen_random_uuid(),
    message TEXT DEFAULT '',
    resolution TEXT DEFAULT '',
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT current_timestamp
);
