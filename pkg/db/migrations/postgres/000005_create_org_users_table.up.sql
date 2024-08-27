-- NOTE: owner is not actually stored here
CREATE TYPE backend.access_level AS ENUM ('member', 'invited', 'owner');

CREATE TABLE IF NOT EXISTS backend.organization_users(
    org_id INT REFERENCES backend.organizations(id) ON DELETE CASCADE,
    user_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    level backend.access_level NOT NULL,
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp,
    PRIMARY KEY (org_id, user_id)
);
