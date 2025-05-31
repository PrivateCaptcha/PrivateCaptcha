-- name: GetAPIKeyByExternalID :one
SELECT * FROM backend.apikeys WHERE external_id = $1;

-- name: GetUserAPIKeys :many
SELECT * FROM backend.apikeys WHERE user_id = $1 AND expires_at > NOW();

-- name: CreateAPIKey :one
INSERT INTO backend.apikeys (name, user_id, expires_at, requests_per_second, requests_burst) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: UpdateAPIKey :one
UPDATE backend.apikeys SET expires_at = $1, enabled = $2 WHERE external_id = $3 RETURNING *;

-- name: UpdateUserAPIKeysRateLimits :exec
UPDATE backend.apikeys SET requests_per_second = $1 WHERE user_id = $2;

-- name: DeleteUserAPIKeys :exec
DELETE FROM backend.apikeys WHERE user_id = $1;

-- name: DeleteAPIKey :one
DELETE FROM backend.apikeys WHERE id=$1 AND user_id = $2 RETURNING *;
