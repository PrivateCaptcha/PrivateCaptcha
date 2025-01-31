-- name: CreateOrganization :one
INSERT INTO backend.organizations (name, user_id) VALUES ($1, $2) RETURNING *;

-- name: GetOrganizationWithAccess :one
 SELECT sqlc.embed(o), ou.level
 FROM backend.organizations o
 LEFT JOIN backend.organization_users ou ON
     o.id = ou.org_id
     AND ou.user_id = $2
     AND o.user_id != $2  -- Only do the join if user isn't the owner
 WHERE o.id = $1;

-- name: FindUserOrgByName :one
SELECT * from backend.organizations WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: UpdateOrganization :one
UPDATE backend.organizations SET name = $1, updated_at = NOW()
WHERE id = $2
RETURNING *;

-- name: GetUserOrganizations :many
SELECT sqlc.embed(o), 'owner'::backend.access_level as level FROM backend.organizations o WHERE o.user_id = $1 AND o.deleted_at IS NULL
UNION ALL
SELECT sqlc.embed(o), ou.level
FROM backend.organizations o
JOIN backend.organization_users ou ON o.id = ou.org_id
WHERE ou.user_id = $1 AND o.deleted_at IS NULL;

-- name: SoftDeleteUserOrganization :exec
UPDATE backend.organizations SET deleted_at = NOW(), updated_at = NOW(), name = name || ' deleted_' || substr(md5(random()::text), 1, 8) WHERE id = $1 AND user_id = $2;

-- name: SoftDeleteUserOrganizations :exec
UPDATE backend.organizations SET deleted_at = NOW(), updated_at = NOW(), name = name || ' deleted_' || substr(md5(random()::text), 1, 8) WHERE user_id = $1;

-- name: GetSoftDeletedOrganizations :many
SELECT sqlc.embed(o)
FROM backend.organizations o
JOIN backend.users u ON o.user_id = u.id
WHERE o.deleted_at IS NOT NULL
  AND o.deleted_at < $1
  AND u.deleted_at IS NULL
LIMIT $2;

-- name: DeleteOrganizations :exec
DELETE FROM backend.organizations WHERE id = ANY($1::INT[]);
