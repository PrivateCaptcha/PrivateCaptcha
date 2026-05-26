-- name: CreateForm :one
INSERT INTO backend.forms (name, url, property_id, org_id, org_owner_id, creator_id, fields, enabled, requests_per_second, requests_burst, retry_request_count, method)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetFormByID :one
SELECT * FROM backend.forms WHERE id = $1;

-- name: GetFormsByID :many
SELECT * FROM backend.forms WHERE id = ANY($1::INT[]);

-- name: GetFormsByExternalID :many
SELECT * FROM backend.forms WHERE external_id = ANY($1::UUID[]);

-- name: GetOrgFormByName :one
SELECT * FROM backend.forms WHERE org_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: GetOrgForms :many
SELECT *
FROM backend.forms
WHERE org_id = $1 AND deleted_at IS NULL AND enabled = TRUE
ORDER BY created_at
OFFSET $2
LIMIT $3;

-- name: GetOrgFormsCount :one
SELECT COUNT(*) as count FROM backend.forms WHERE org_id = $1 AND deleted_at IS NULL;

-- name: GetSoftDeletedForms :many
SELECT sqlc.embed(f)
FROM backend.forms f
JOIN backend.organizations o ON f.org_id = o.id
JOIN backend.users u ON o.user_id = u.id
WHERE f.deleted_at IS NOT NULL
  AND f.deleted_at < $1
  AND o.deleted_at IS NULL
  AND u.deleted_at IS NULL
LIMIT $2;

-- name: DeleteForms :execrows
DELETE FROM backend.forms WHERE id = ANY($1::INT[]);
