-- name: GetCachedByKey :one
SELECT value FROM backend.cache WHERE key = $1 AND expires_at >= NOW();

-- name: CreateCache :execrows
INSERT INTO backend.cache (key, value, expires_at) VALUES ($1, $2, NOW() + $3::INTERVAL)
ON CONFLICT (key) DO UPDATE 
SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at;

-- name: DeleteExpiredCache :execrows
DELETE FROM backend.cache WHERE expires_at < NOW();
