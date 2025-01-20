CREATE TYPE backend.difficulty_growth AS ENUM ('slow', 'medium', 'fast');

CREATE TABLE IF NOT EXISTS backend.properties(
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    external_id UUID DEFAULT gen_random_uuid(),
    org_id INT REFERENCES backend.organizations(id) ON DELETE CASCADE,
    creator_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    org_owner_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    domain VARCHAR(255) NOT NULL,
    level SMALLINT CHECK (level >= 0 AND level < 256),
    growth backend.difficulty_growth NOT NULL DEFAULT 'medium',
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp,
    deleted_at TIMESTAMPTZ NULL DEFAULT NULL,
    allow_subdomains BOOL NOT NULL DEFAULT FALSE
);

CREATE UNIQUE INDEX IF NOT EXISTS index_property_external_id ON backend.properties(external_id);

ALTER TABLE backend.properties ADD CONSTRAINT unique_property_name_per_organization UNIQUE (name, org_id);

CREATE OR REPLACE TRIGGER deleted_record_insert AFTER DELETE ON backend.properties
   FOR EACH ROW EXECUTE FUNCTION backend.deleted_record_insert();
