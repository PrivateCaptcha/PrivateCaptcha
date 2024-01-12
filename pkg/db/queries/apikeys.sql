-- name: GetAPIKeyByExternalID :one
SELECT * FROM apikeys WHERE external_id = $1;

-- name: CreateAPIKey :one
INSERT INTO apikeys (
  user_id
) VALUES (
  $1
)
RETURNING *;
