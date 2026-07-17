DELETE FROM backend.organization_users duplicate_invite
USING backend.organization_users first_invite
WHERE duplicate_invite.org_id = first_invite.org_id
  AND LOWER(duplicate_invite.email) = LOWER(first_invite.email)
  AND duplicate_invite.id > first_invite.id;

DROP INDEX IF EXISTS backend.organization_users_org_email_idx;

CREATE UNIQUE INDEX IF NOT EXISTS organization_users_org_email_lower_idx
ON backend.organization_users (org_id, LOWER(email)) WHERE email IS NOT NULL;
