-- name: GetAPIKeyByExternalID :one
SELECT * FROM apikeys WHERE external_id = $1;

-- name: CreateAPIKey :one
INSERT INTO apikeys (
  user_id
) VALUES (
  $1
)
RETURNING *;

-- name: UpdateAPIKey :exec
UPDATE apikeys SET expires_at = $1, enabled = $2
WHERE external_id = $3;
