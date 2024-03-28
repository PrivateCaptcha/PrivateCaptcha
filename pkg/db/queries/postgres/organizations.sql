-- name: CreateOrganization :one
INSERT INTO organizations (org_name, user_id) VALUES ($1, $2) RETURNING *;

-- name: GetOrganizationByID :one
SELECT * from organizations WHERE id = $1;

-- name: GetUserOrganizations :many
SELECT sqlc.embed(o), 'owner' as level FROM organizations o WHERE o.user_id = $1
UNION ALL
SELECT sqlc.embed(o), ou.level
FROM organizations o
JOIN organization_users ou ON o.id = ou.org_id
WHERE ou.user_id = $1;
