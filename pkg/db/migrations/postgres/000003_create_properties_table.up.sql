CREATE TYPE difficulty_level AS ENUM ('small', 'medium', 'high');
CREATE TYPE difficulty_growth AS ENUM ('slow', 'medium', 'fast');

CREATE TABLE IF NOT EXISTS properties(
    id SERIAL PRIMARY KEY,
    external_id UUID DEFAULT gen_random_uuid(),
    user_id INTEGER REFERENCES users(id),
    difficulty_level difficulty_level NOT NULL DEFAULT 'medium',
    difficulty_growth difficulty_growth NOT NULL DEFAULT 'medium',
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp
);

CREATE UNIQUE INDEX IF NOT EXISTS index_property_external_id ON properties(external_id);
