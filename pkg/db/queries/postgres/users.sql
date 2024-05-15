-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: CreateUser :one
INSERT INTO users (name, email) VALUES ($1, $2) RETURNING *;

-- name: UpdateUser :one
UPDATE users SET name = $1, email = $2 WHERE id = $3 RETURNING *;
