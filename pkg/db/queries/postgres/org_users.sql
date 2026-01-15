-- name: GetOrganizationUsers :many
SELECT sqlc.embed(u), ou.level
FROM backend.organization_users ou
JOIN backend.users u ON ou.user_id = u.id
WHERE ou.org_id = $1 AND u.deleted_at IS NULL;

-- name: GetOrganizationUsersWithEmailInvites :many
SELECT sqlc.embed(ou), u.id AS linked_user_id, u.name AS user_name, u.email AS user_email
FROM backend.organization_users ou
LEFT JOIN backend.users u ON ou.user_id = u.id AND u.deleted_at IS NULL
WHERE ou.org_id = $1;

-- name: InviteUserToOrg :one
INSERT INTO backend.organization_users (org_id, user_id, level) VALUES ($1, $2, 'invited') RETURNING *;

-- name: InviteEmailToOrg :one
INSERT INTO backend.organization_users (org_id, email, level) VALUES ($1, $2, 'invited') RETURNING *;

-- name: LinkOrgInviteToUser :one
UPDATE backend.organization_users 
SET user_id = $1, email = NULL, updated_at = NOW() 
WHERE id = $2 AND user_id IS NULL
RETURNING *;

-- name: UpdateOrgMembershipLevel :exec
UPDATE backend.organization_users SET level = $1, updated_at = NOW() WHERE org_id = $2 AND user_id = $3 AND level = $4;

-- name: RemoveUserFromOrg :exec
DELETE FROM backend.organization_users WHERE org_id = $1 AND user_id = $2;

-- name: RemoveOrgInviteByID :exec
DELETE FROM backend.organization_users WHERE id = $1;

-- name: SwapOrgOwnership :exec
WITH delete_new_owner AS (
    DELETE FROM backend.organization_users ou WHERE ou.org_id = $1 AND ou.user_id = $2
),
upsert_old_owner AS (
    UPDATE backend.organization_users ou2
    SET level = 'member', updated_at = NOW() 
    WHERE ou2.org_id = $1 AND ou2.user_id = $3
    RETURNING ou2.id
),
insert_old_owner AS (
    INSERT INTO backend.organization_users (org_id, user_id, level) 
    SELECT $1, $3, 'member'
    WHERE NOT EXISTS (SELECT 1 FROM upsert_old_owner)
)
SELECT 1;
