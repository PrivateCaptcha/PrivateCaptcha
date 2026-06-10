-- name: GetCachedByKey :one
SELECT value FROM backend.cache WHERE key = $1 AND expires_at >= NOW();

-- name: CreateCache :execrows
INSERT INTO backend.cache (key, value, expires_at) VALUES ($1, $2, NOW() + $3::INTERVAL)
ON CONFLICT (key) DO UPDATE 
SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at;

-- name: CreateCacheMany :execrows
INSERT INTO backend.cache (key, value, expires_at)
SELECT unnest(@keys::TEXT[]) as key,
       unnest(@values::BYTEA[]) as value,
       NOW() + unnest(@intervals::INTERVAL[]) as expires_at
ON CONFLICT (key)
DO UPDATE SET
    value = EXCLUDED.value,
    expires_at = EXCLUDED.expires_at;

-- name: UpdateCacheExpiration :execrows
UPDATE backend.cache SET expires_at = NOW() + $2::INTERVAL WHERE key = $1;

-- name: DeleteCachedByKey :execrows
DELETE FROM backend.cache WHERE key = $1;

-- name: DeleteCachedByKeys :execrows
DELETE FROM backend.cache WHERE key = ANY(@keys::TEXT[]);

-- name: DeleteExpiredCache :execrows
DELETE FROM backend.cache WHERE expires_at < NOW();
