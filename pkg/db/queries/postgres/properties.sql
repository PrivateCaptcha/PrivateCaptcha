-- name: GetPropertiesByExternalID :many
SELECT * from properties WHERE external_id = ANY($1::UUID[]);

-- name: CreateProperty :one
INSERT INTO properties (name, org_id, creator_id, org_owner_id, domain, level, growth)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateProperty :one
UPDATE properties SET name = $1, level = $2, growth = $3, updated_at = NOW()
WHERE id = $4
RETURNING *;

-- name: GetOrgPropertyByName :one
SELECT * from properties WHERE org_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: GetPropertyByID :one
SELECT * from properties WHERE id = $1;

-- name: GetOrgProperties :many
SELECT * from properties WHERE org_id = $1 AND deleted_at IS NULL ORDER BY created_at;

-- name: SoftDeleteProperty :one
UPDATE properties SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: GetSoftDeletedProperties :many
SELECT sqlc.embed(p)
FROM properties p
JOIN organizations o ON p.org_id = o.id
JOIN users u ON o.user_id = u.id
WHERE p.deleted_at IS NOT NULL
  AND p.deleted_at < $1
  AND o.deleted_at IS NULL
  AND u.deleted_at IS NULL
LIMIT $2;

-- name: DeleteProperties :exec
DELETE FROM properties WHERE id = ANY($1::INT[]);
