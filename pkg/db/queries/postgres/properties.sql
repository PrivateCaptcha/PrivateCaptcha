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
    WHERE p.id = $1 AND (p.creator_id = $9 OR p.org_owner_id = $9) AND (p.org_id = $10 OR $10 IS NULL) AND p.enabled = TRUE
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
UPDATE backend.properties p SET org_id = $2, org_owner_id = $3, updated_at = NOW()
WHERE p.id = $1 AND (p.creator_id = @user_id OR p.org_owner_id = @user_id)
  AND NOT EXISTS (SELECT 1 FROM backend.forms f WHERE f.property_id = p.id)
RETURNING *;

-- name: MovePropertyWithForm :one
UPDATE backend.properties p SET org_id = $2, org_owner_id = $3, updated_at = NOW()
WHERE p.id = $1 AND (creator_id = @user_id OR org_owner_id = @user_id)
RETURNING *;

-- name: GetOrgPropertyByName :one
SELECT * from backend.properties WHERE org_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: GetPropertyByID :one
SELECT * from backend.properties WHERE id = $1;

-- name: GetOrgPropertiesByDateAscending :many
SELECT *
FROM backend.properties
WHERE org_id = $1 AND deleted_at IS NULL AND enabled = TRUE
ORDER BY created_at ASC, id ASC
OFFSET $2
LIMIT $3;

-- name: GetOrgPropertiesByDateDescending :many
SELECT *
FROM backend.properties
WHERE org_id = $1 AND deleted_at IS NULL AND enabled = TRUE
ORDER BY created_at DESC, id DESC
OFFSET $2
LIMIT $3;

-- name: GetOrgPropertiesByNameAscending :many
SELECT *
FROM backend.properties
WHERE org_id = $1 AND deleted_at IS NULL AND enabled = TRUE
ORDER BY name ASC, id ASC
OFFSET $2
LIMIT $3;

-- name: GetOrgPropertiesByNameDescending :many
SELECT *
FROM backend.properties
WHERE org_id = $1 AND deleted_at IS NULL AND enabled = TRUE
ORDER BY name DESC, id DESC
OFFSET $2
LIMIT $3;

-- name: SoftDeleteProperty :one
UPDATE backend.properties p SET deleted_at = NOW(), updated_at = NOW(), name = name || ' deleted_' || substr(md5(random()::text), 1, 8)
WHERE p.id = $1
  AND NOT EXISTS (SELECT 1 FROM backend.forms f WHERE f.property_id = p.id AND f.deleted_at IS NULL)
RETURNING *;

-- name: SoftDeletePropertyWithForm :one
UPDATE backend.properties p SET deleted_at = NOW(), updated_at = NOW(), name = name || ' deleted_' || substr(md5(random()::text), 1, 8)
WHERE p.id = $1
RETURNING *;

-- name: SoftDeleteProperties :many
UPDATE backend.properties p SET deleted_at = NOW(), updated_at = NOW(), name = name || ' deleted_' || substr(md5(random()::text), 1, 8)
WHERE p.id = ANY($1::INT[])
  AND (p.creator_id = $2 OR p.org_owner_id = $2)
  AND (p.org_id = $3 OR $3 IS NULL)
  AND deleted_at IS NULL
  AND enabled = TRUE
  AND NOT EXISTS (SELECT 1 FROM backend.forms f WHERE f.property_id = p.id AND f.deleted_at IS NULL)
RETURNING *;

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

-- name: DeleteProperties :execrows
DELETE FROM backend.properties WHERE id = ANY($1::INT[]);

-- name: GetProperties :many
SELECT * FROM backend.properties LIMIT $1;

-- name: GetUserPropertiesCount :one
SELECT COUNT(*) as count FROM backend.properties WHERE org_owner_id = $1 AND deleted_at IS NULL;

-- name: GetOrgPropertiesCount :one
SELECT COUNT(*) as count FROM backend.properties WHERE org_id = $1 AND deleted_at IS NULL AND enabled = TRUE;

-- name: TransferOrgProperties :execrows
UPDATE backend.properties SET org_owner_id = $2, updated_at = NOW() WHERE org_id = $1 AND org_owner_id = $3;

-- name: GetPropertyAccessViolations :many
SELECT p.id
FROM backend.properties p
LEFT JOIN backend.organization_users ou ON p.org_id = ou.org_id AND ou.user_id = $2 AND ou.level != 'invited'
WHERE p.id = ANY($1::INT[])
  AND NOT (
    p.org_owner_id = $2 
    OR (p.creator_id = $2 AND ou.user_id IS NOT NULL)
  );
