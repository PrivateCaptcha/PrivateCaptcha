DROP INDEX IF EXISTS backend.organization_users_org_email_lower_idx;

CREATE UNIQUE INDEX IF NOT EXISTS organization_users_org_email_idx
ON backend.organization_users (org_id, email) WHERE email IS NOT NULL;
