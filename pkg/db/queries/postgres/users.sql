-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: CreateUser :one
INSERT INTO users (user_name, email) VALUES ($1, $2) RETURNING *;
