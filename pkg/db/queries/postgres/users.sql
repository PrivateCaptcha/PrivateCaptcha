-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 AND deleted_at IS NULL;

-- name: GetUserBySubscriptionID :one
SELECT * FROM users WHERE subscription_id = $1;

-- name: CreateUser :one
INSERT INTO users (name, email, subscription_id) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateUserData :one
UPDATE users SET name = $2, email = $3, updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: UpdateUserSubscription :one
UPDATE users SET subscription_id = $2, updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = NOW() WHERE id = $1;
