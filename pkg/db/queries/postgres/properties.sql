-- name: PropertyAndOrgByExternalID :one
SELECT sqlc.embed(p), sqlc.embed(o) FROM properties p
INNER JOIN organizations o ON p.org_id = o.id
WHERE p.external_id = $1;

-- name: CreateProperty :one
INSERT INTO properties (name, org_id, level, growth) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetOrgPropertyByName :one
SELECT * from properties WHERE org_id = $1 AND name = $2;
