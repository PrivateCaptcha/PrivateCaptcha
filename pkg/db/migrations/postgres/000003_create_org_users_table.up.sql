CREATE TYPE access_level AS ENUM ('read', 'write');

CREATE TABLE IF NOT EXISTS organization_users(
    org_id INT REFERENCES organizations(id),
    user_id INT REFERENCES users(id),
    level access_level NOT NULL DEFAULT 'read',
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    PRIMARY KEY (org_id, user_id)
);
