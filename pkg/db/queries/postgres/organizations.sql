-- name: CreateOrganization :one
INSERT INTO organizations (name, user_id) VALUES ($1, $2) RETURNING *;

-- name: GetOrganizationByID :one
SELECT * from organizations WHERE id = $1;

-- name: FindUserOrgByName :one
SELECT * from organizations WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: UpdateOrganization :one
UPDATE organizations SET name = $1, updated_at = NOW()
WHERE id = $2
RETURNING *;

-- name: GetUserOrganizations :many
SELECT sqlc.embed(o), 'owner' as level FROM organizations o WHERE o.user_id = $1 AND o.deleted_at IS NULL
UNION ALL
SELECT sqlc.embed(o), ou.level
FROM organizations o
JOIN organization_users ou ON o.id = ou.org_id
WHERE ou.user_id = $1 AND o.deleted_at IS NULL;

-- name: SoftDeleteOrganization :exec
UPDATE organizations SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1;

-- name: SoftDeleteUserOrganizations :exec
UPDATE organizations SET deleted_at = NOW(), updated_at = NOW() WHERE user_id = $1;
