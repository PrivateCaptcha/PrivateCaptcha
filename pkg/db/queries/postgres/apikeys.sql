-- name: GetAPIKeyByExternalID :one
SELECT * FROM apikeys WHERE external_id = $1;

-- name: GetUserAPIKeys :many
SELECT * FROM apikeys WHERE user_id = $1 AND deleted_at IS NULL AND expires_at > NOW();

-- name: CreateAPIKey :one
INSERT INTO apikeys (name, user_id, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateAPIKey :exec
UPDATE apikeys SET expires_at = $1, enabled = $2 WHERE external_id = $3;

-- name: SoftDeleteUserAPIKeys :exec
UPDATE apikeys SET deleted_at = NOW() WHERE user_id = $1;

-- name: SoftDeleteAPIKey :one
UPDATE apikeys SET deleted_at = NOW() WHERE id=$1 AND user_id = $2 RETURNING *;
