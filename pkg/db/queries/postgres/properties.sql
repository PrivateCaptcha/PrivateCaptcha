-- name: PropertyAndOrgByExternalID :one
SELECT sqlc.embed(p), sqlc.embed(o) FROM properties p
INNER JOIN organizations o ON p.org_id = o.id
WHERE p.external_id = $1;

-- name: CreateProperty :one
INSERT INTO properties (
  org_id
) VALUES (
  $1
)
RETURNING *;
