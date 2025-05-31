-- name: GetCachedByKey :one
SELECT value FROM backend.cache WHERE key = $1 AND expires_at >= NOW();

-- name: CreateCache :exec
INSERT INTO backend.cache (key, value, expires_at) VALUES ($1, $2, NOW() + $3::INTERVAL)
ON CONFLICT (key) DO UPDATE 
SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at;

-- name: CreateCacheMany :exec
INSERT INTO backend.cache (key, value, expires_at)
SELECT unnest(@keys::TEXT[]) as key,
       unnest(@values::BYTEA[]) as value,
       NOW() + unnest(@intervals::INTERVAL[]) as expires_at
ON CONFLICT (key)
DO UPDATE SET
    value = EXCLUDED.value,
    expires_at = EXCLUDED.expires_at;

-- name: UpdateCacheExpiration :exec
UPDATE backend.cache SET expires_at = NOW() + $2::INTERVAL WHERE key = $1;

-- name: DeleteCachedByKey :exec
DELETE FROM backend.cache WHERE key = $1;

-- name: DeleteExpiredCache :exec
DELETE FROM backend.cache WHERE expires_at < NOW();
