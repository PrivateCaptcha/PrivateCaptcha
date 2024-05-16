-- name: GetOrganizationUsers :many
SELECT sqlc.embed(u), ou.level
FROM organization_users ou
JOIN users u ON ou.user_id = u.id
WHERE ou.org_id = $1 AND u.deleted_at IS NULL;

-- name: InviteUserToOrg :one
INSERT INTO organization_users (org_id, user_id, level) VALUES ($1, $2, 'invited') RETURNING *;

-- name: UpdateOrgMembershipLevel :exec
UPDATE organization_users SET level = $1, updated_at = NOW() WHERE org_id = $2 AND user_id = $3 AND level = $4;

-- name: RemoveUserFromOrg :exec
DELETE FROM organization_users WHERE org_id = $1 AND user_id = $2;
