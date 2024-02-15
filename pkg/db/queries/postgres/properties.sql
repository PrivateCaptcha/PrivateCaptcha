-- name: GetPropertyByExternalID :one
SELECT * FROM properties WHERE external_id = $1;

-- name: CreateProperty :one
INSERT INTO properties (
  user_id
) VALUES (
  $1
)
RETURNING *;
