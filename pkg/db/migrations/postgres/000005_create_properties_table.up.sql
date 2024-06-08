CREATE TYPE difficulty_level AS ENUM ('small', 'medium', 'high');
CREATE TYPE difficulty_growth AS ENUM ('slow', 'medium', 'fast');

CREATE TABLE IF NOT EXISTS properties(
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    external_id UUID DEFAULT gen_random_uuid(),
    org_id INT REFERENCES organizations(id),
    creator_id INT REFERENCES users(id),
    org_owner_id INT REFERENCES users(id),
    domain VARCHAR(255) NOT NULL,
    level difficulty_level NOT NULL DEFAULT 'medium',
    growth difficulty_growth NOT NULL DEFAULT 'medium',
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp,
    deleted_at TIMESTAMPTZ NULL DEFAULT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS index_property_external_id ON properties(external_id);

ALTER TABLE properties ADD CONSTRAINT unique_property_name_per_organization UNIQUE (name, org_id);
