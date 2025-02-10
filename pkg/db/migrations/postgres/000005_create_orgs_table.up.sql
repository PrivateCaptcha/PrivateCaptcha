CREATE TABLE IF NOT EXISTS backend.organizations(
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    user_id INTEGER REFERENCES backend.users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    deleted_at TIMESTAMPTZ NULL DEFAULT NULL
);

CREATE OR REPLACE TRIGGER deleted_record_insert AFTER DELETE ON backend.organizations
   FOR EACH ROW EXECUTE FUNCTION backend.deleted_record_insert();
