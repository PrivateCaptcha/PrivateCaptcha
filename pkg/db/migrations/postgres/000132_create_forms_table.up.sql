CREATE TYPE backend.form_method AS ENUM ('post', 'put', 'delete', 'patch');

CREATE TABLE IF NOT EXISTS backend.forms(
    id SERIAL PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT gen_random_uuid(),
    org_id INT REFERENCES backend.organizations(id) ON DELETE CASCADE,
    creator_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    org_owner_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    deleted_at TIMESTAMPTZ NULL DEFAULT NULL,
    property_id INT NOT NULL REFERENCES backend.properties(id) ON DELETE CASCADE,
    fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    requests_per_second FLOAT NOT NULL DEFAULT 1,
    requests_burst INTEGER NOT NULL DEFAULT 5,
    retry_request_count INTEGER NOT NULL DEFAULT 0,
    method backend.form_method NOT NULL DEFAULT 'post'
);

CREATE UNIQUE INDEX IF NOT EXISTS index_form_external_id ON backend.forms(external_id);
CREATE UNIQUE INDEX IF NOT EXISTS index_form_property_id_unique ON backend.forms(property_id);
