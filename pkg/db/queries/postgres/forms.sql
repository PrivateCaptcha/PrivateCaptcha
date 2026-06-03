-- name: CreateForm :one
INSERT INTO backend.forms (name, url, property_id, org_id, org_owner_id, creator_id, fields, enabled, requests_per_minute, retry_request_count, method)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetFormByID :one
SELECT * FROM backend.forms WHERE id = $1;

-- name: GetFormsByID :many
SELECT * FROM backend.forms WHERE id = ANY($1::INT[]);

-- name: GetFormsByExternalID :many
SELECT * FROM backend.forms WHERE external_id = ANY($1::UUID[]);

-- name: GetOrgFormByName :one
SELECT * FROM backend.forms WHERE org_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: GetOrgForms :many
SELECT *
FROM backend.forms
WHERE org_id = $1 AND deleted_at IS NULL AND enabled = TRUE
ORDER BY created_at
OFFSET $2
LIMIT $3;

-- name: GetOrgFormsCount :one
SELECT COUNT(*) as count FROM backend.forms WHERE org_id = $1 AND deleted_at IS NULL;

-- name: GetSoftDeletedForms :many
SELECT sqlc.embed(f)
FROM backend.forms f
JOIN backend.organizations o ON f.org_id = o.id
JOIN backend.users u ON o.user_id = u.id
WHERE f.deleted_at IS NOT NULL
  AND f.deleted_at < $1
  AND o.deleted_at IS NULL
  AND u.deleted_at IS NULL
LIMIT $2;

-- name: UpdateForm :one
WITH old AS (
    SELECT * FROM backend.forms f
    WHERE f.id = $1 AND (f.creator_id = $8 OR f.org_owner_id = $8) AND (f.org_id = $9 OR $9 IS NULL) AND f.enabled = TRUE
    FOR UPDATE
),
upd AS (
    UPDATE backend.forms f
    SET name = $2,
        url = $3,
        active = $4,
        retry_request_count = $5,
        requests_per_minute = $6,
        method = $7,
        updated_at = NOW()
    WHERE f.id = (SELECT id FROM old)
    RETURNING *
)
SELECT
    upd.*,
    old.name AS old_name,
    old.url AS old_url,
    old.active AS old_active,
    old.retry_request_count AS old_retry_request_count,
    old.requests_per_minute AS old_requests_per_minute,
    old.method AS old_method
FROM upd
CROSS JOIN old;

-- name: DeactivateForms :many
UPDATE backend.forms
SET active = FALSE, updated_at = NOW()
WHERE id = ANY($1::INT[])
  AND active = TRUE
  AND enabled = TRUE
  AND deleted_at IS NULL
RETURNING *;

-- name: MoveForm :one
UPDATE backend.forms
SET org_id = $2, org_owner_id = $3, updated_at = NOW()
WHERE id = $1 AND (creator_id = @user_id OR org_owner_id = @user_id)
RETURNING *;

-- name: SoftDeleteForm :one
UPDATE backend.forms
SET deleted_at = NOW(), updated_at = NOW(), name = name || ' deleted_' || substr(md5(random()::text), 1, 8)
WHERE id = $1
RETURNING *;

-- name: DeleteForms :execrows
DELETE FROM backend.forms WHERE id = ANY($1::INT[]);

-- name: GetUserFormsCount :one
SELECT COUNT(*) as count FROM backend.forms WHERE org_owner_id = $1 AND deleted_at IS NULL;
