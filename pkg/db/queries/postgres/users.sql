-- name: GetUserByID :one
SELECT * FROM backend.users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM backend.users WHERE LOWER(email) = LOWER($1);

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

-- name: DeleteUsers :execrows
DELETE FROM backend.users WHERE id = ANY($1::INT[]);

-- name: GetUsersWithoutSubscription :many
SELECT u.*
FROM backend.users u
LEFT JOIN backend.subscriptions s ON u.subscription_id = s.id
WHERE u.id = ANY(sqlc.arg(user_ids)::INT[])
  AND (u.subscription_id IS NULL OR u.deleted_at IS NOT NULL OR s.status = sqlc.arg(expired_trial_status) OR u.enabled = FALSE);
