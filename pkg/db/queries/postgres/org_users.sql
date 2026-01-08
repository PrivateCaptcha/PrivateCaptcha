-- name: GetOrganizationUsers :many
SELECT sqlc.embed(u), ou.level
FROM backend.organization_users ou
JOIN backend.users u ON ou.user_id = u.id
WHERE ou.org_id = $1 AND u.deleted_at IS NULL;

-- name: InviteUserToOrg :one
INSERT INTO backend.organization_users (org_id, user_id, level) VALUES ($1, $2, 'invited') RETURNING *;

-- name: UpdateOrgMembershipLevel :exec
UPDATE backend.organization_users SET level = $1, updated_at = NOW() WHERE org_id = $2 AND user_id = $3 AND level = $4;

-- name: RemoveUserFromOrg :exec
DELETE FROM backend.organization_users WHERE org_id = $1 AND user_id = $2;

-- name: SwapOrgOwnership :exec
WITH delete_new_owner AS (
    DELETE FROM backend.organization_users ou WHERE ou.org_id = $1 AND ou.user_id = $2
),
insert_old_owner AS (
    INSERT INTO backend.organization_users (org_id, user_id, level) VALUES ($1, $3, 'member')
    ON CONFLICT (org_id, user_id) DO UPDATE SET level = 'member', updated_at = NOW()
)
SELECT 1;
