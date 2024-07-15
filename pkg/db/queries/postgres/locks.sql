-- name: InsertLock :one
INSERT INTO locks (name, data, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (name) DO UPDATE
SET expires_at = EXCLUDED.expires_at
WHERE locks.expires_at <= NOW()
RETURNING *;

-- name: DeleteLock :exec
DELETE FROM locks WHERE name = $1;
