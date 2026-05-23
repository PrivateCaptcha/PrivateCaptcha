-- name: CreateForm :one
INSERT INTO backend.forms (url, property_id, org_id, org_owner_id, creator_id, fields, enabled, requests_per_second, requests_burst, retry_request_count, method)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetFormByExternalID :one
SELECT * FROM backend.forms WHERE external_id = $1 AND deleted_at IS NULL;

-- name: GetFormsByExternalID :many
SELECT * FROM backend.forms WHERE external_id = ANY($1::UUID[]) AND deleted_at IS NULL;

-- name: GetFormByPropertyID :one
SELECT * FROM backend.forms WHERE property_id = $1 AND deleted_at IS NULL;
