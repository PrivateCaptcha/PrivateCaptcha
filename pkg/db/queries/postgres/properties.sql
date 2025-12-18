-- name: GetPropertiesByExternalID :many
SELECT * from backend.properties WHERE external_id = ANY($1::UUID[]);

-- name: GetPropertiesByID :many
SELECT * from backend.properties WHERE id = ANY($1::INT[]);

-- name: GetPropertyByExternalID :one
SELECT * from backend.properties WHERE external_id = $1;

-- name: CreateProperty :one
INSERT INTO backend.properties (name, org_id, creator_id, org_owner_id, domain, level, growth, validity_interval, allow_subdomains, allow_localhost, max_replay_count)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateProperty :one
WITH old AS (
    SELECT * FROM backend.properties p
    WHERE p.id = $1 AND (p.creator_id = $9 OR p.org_owner_id = $9)
    FOR UPDATE
),
upd AS (
    UPDATE backend.properties p
    SET name = $2,
        level = $3,
        growth = $4,
        validity_interval = $5,
        allow_subdomains = $6,
        allow_localhost = $7,
        max_replay_count = $8,
        updated_at = NOW()
    WHERE p.id = (SELECT id FROM old)
    RETURNING * -- This ensures the final SELECT only returns data if the update actually happened
)
SELECT
    upd.*,
    old.name AS old_name,
    old.level AS old_level,
    old.growth AS old_growth,
    old.validity_interval AS old_validity_interval,
    old.allow_subdomains AS old_allow_subdomains,
    old.allow_localhost AS old_allow_localhost,
    old.max_replay_count AS old_max_replay_count
FROM upd
CROSS JOIN old;

-- name: MoveProperty :one
UPDATE backend.properties SET org_id = $2, org_owner_id = $3, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetOrgPropertyByName :one
SELECT * from backend.properties WHERE org_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: GetPropertyByID :one
SELECT * from backend.properties WHERE id = $1;

-- name: GetOrgProperties :many
SELECT *
FROM backend.properties
WHERE org_id = $1 AND deleted_at IS NULL
ORDER BY created_at
OFFSET $2
LIMIT $3;

-- name: SoftDeleteProperty :one
UPDATE backend.properties SET deleted_at = NOW(), updated_at = NOW(), name = name || ' deleted_' || substr(md5(random()::text), 1, 8) WHERE id = $1 RETURNING *;

-- name: SoftDeleteProperties :many
UPDATE backend.properties SET deleted_at = NOW(), updated_at = NOW(), name = name || ' deleted_' || substr(md5(random()::text), 1, 8) WHERE id = ANY($1::INT[]) AND (creator_id = $2 OR org_owner_id = $2) AND deleted_at IS NULL RETURNING *;

-- name: GetSoftDeletedProperties :many
SELECT sqlc.embed(p)
FROM backend.properties p
JOIN backend.organizations o ON p.org_id = o.id
JOIN backend.users u ON o.user_id = u.id
WHERE p.deleted_at IS NOT NULL
  AND p.deleted_at < $1
  AND o.deleted_at IS NULL
  AND u.deleted_at IS NULL
LIMIT $2;

-- name: DeleteProperties :exec
DELETE FROM backend.properties WHERE id = ANY($1::INT[]);

-- name: GetProperties :many
SELECT * FROM backend.properties LIMIT $1;

-- name: GetUserPropertiesCount :one
SELECT COUNT(*) as count FROM backend.properties WHERE org_owner_id = $1 AND deleted_at IS NULL;

-- name: GetOrgPropertiesCount :one
SELECT COUNT(*) as count FROM backend.properties WHERE org_id = $1 AND deleted_at IS NULL;
