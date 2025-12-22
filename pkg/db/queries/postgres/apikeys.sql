-- name: GetAPIKeyByExternalID :one
SELECT * FROM backend.apikeys WHERE external_id = $1;

-- name: GetUserAPIKeys :many
SELECT * FROM backend.apikeys WHERE user_id = $1 AND expires_at > NOW();

-- name: GetUserAPIKeyByName :one
SELECT * FROM backend.apikeys WHERE user_id = $1 AND name = $2 AND expires_at > NOW();

-- name: CreateAPIKey :one
INSERT INTO backend.apikeys (name, user_id, expires_at, requests_per_second, requests_burst, period, scope, readonly, org_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING *;

-- name: UpdateAPIKey :one
UPDATE backend.apikeys SET expires_at = $1, enabled = $2, updated_at = NOW() WHERE external_id = $3 RETURNING *;

-- name: RotateAPIKey :one
UPDATE backend.apikeys SET external_id = gen_random_uuid(), expires_at = NOW() + period, updated_at = NOW() WHERE id = $1 AND user_id = $2 RETURNING *;

-- name: DeleteUserAPIKeys :exec
DELETE FROM backend.apikeys WHERE user_id = $1;

-- name: DeleteAPIKey :one
DELETE FROM backend.apikeys WHERE id=$1 AND user_id = $2 RETURNING *;
