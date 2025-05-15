-- name: GetUserByID :one
SELECT * FROM backend.users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM backend.users WHERE email = $1 AND deleted_at IS NULL;

-- name: GetUserBySubscriptionID :one
SELECT * FROM backend.users WHERE subscription_id = $1;

-- name: CreateUser :one
INSERT INTO backend.users (name, email, subscription_id) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateUserData :one
UPDATE backend.users SET name = $2, email = $3, updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: UpdateUserSubscription :one
UPDATE backend.users SET subscription_id = $2, updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: SoftDeleteUser :one
UPDATE backend.users SET deleted_at = NOW() WHERE id = $1 RETURNING *;

-- name: GetSoftDeletedUsers :many
SELECT sqlc.embed(u)
FROM backend.users u
WHERE u.deleted_at IS NOT NULL
  AND u.deleted_at < $1
LIMIT $2;

-- name: DeleteUsers :exec
DELETE FROM backend.users WHERE id = ANY($1::INT[]);

-- name: GetUsersWithoutSubscription :many
SELECT * FROM backend.users where id = ANY($1::INT[]) AND (subscription_id IS NULL OR deleted_at IS NOT NULL);
