-- name: PropertyAndOrgByExternalID :one
SELECT sqlc.embed(p), sqlc.embed(o) FROM properties p
INNER JOIN organizations o ON p.org_id = o.id
WHERE p.external_id = $1;

-- name: CreateProperty :one
INSERT INTO properties (name, org_id, domain, level, growth)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateProperty :one
UPDATE properties SET name = $1, level = $2, growth = $3, updated_at = NOW()
WHERE id = $4
RETURNING *;

-- name: GetOrgPropertyByName :one
SELECT * from properties WHERE org_id = $1 AND name = $2;

-- name: GetPropertyByID :one
SELECT * from properties WHERE id = $1;

-- name: GetOrgProperties :many
SELECT * from properties WHERE org_id = $1 ORDER BY created_at;
