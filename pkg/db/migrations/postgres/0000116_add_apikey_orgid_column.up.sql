ALTER TABLE backend.apikeys ADD COLUMN org_id INT REFERENCES backend.organizations(id) ON DELETE CASCADE;
