-- name: GetCachedByKey :one
SELECT value FROM cache WHERE key = $1 AND expires_at >= NOW();

-- name: CreateCache :exec
INSERT INTO cache (key, value, expires_at) VALUES ($1, $2, NOW() + $3::INTERVAL);

-- name: UpdateCacheExpiration :exec
UPDATE cache SET expires_at = NOW() + $2::INTERVAL WHERE key = $1;

-- name: DeleteExpiredCache :exec
DELETE FROM cache WHERE expires_at < NOW();
