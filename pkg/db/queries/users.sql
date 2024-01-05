-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByExternalID :one
SELECT * FROM users WHERE external_id = $1;

-- name: CreateUser :one
INSERT INTO users (
  user_name
) VALUES (
  $1
)
RETURNING *;
