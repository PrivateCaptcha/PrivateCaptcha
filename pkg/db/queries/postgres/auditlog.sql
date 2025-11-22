-- name: CreateAuditLogs :copyfrom
INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, old_value, new_value, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: DeleteOldAuditLogs :exec
DELETE FROM backend.audit_logs WHERE created_at < $1;

-- name: GetUserAuditLogs :many
SELECT sqlc.embed(a), u.name, u.email
FROM backend.audit_logs a
LEFT JOIN backend.users u ON u.id = a.user_id
WHERE (a.user_id = $1 OR
    (
        a.entity_table = 'users' AND a.entity_id = $1
    ) OR
    (
        a.entity_table = 'organization_users'
        AND ((a.old_value ->> 'user_id')::bigint = $1 OR (a.new_value ->> 'user_id')::bigint = $1)
    ) OR
    (
        a.entity_table = 'properties'
        AND ((a.old_value ->> 'creator_id')::bigint = $1 OR (a.new_value ->> 'creator_id')::bigint = $1)
    )
)
AND a.created_at >= $2
ORDER BY a.created_at DESC
OFFSET $3
LIMIT $4;

-- name: GetPropertyAuditLogs :many
SELECT sqlc.embed(a), u.name, u.email
FROM backend.audit_logs a
LEFT JOIN backend.users u ON u.id = a.user_id
WHERE a.entity_table = 'properties' AND a.entity_id = $1 AND a.created_at >= $2
ORDER BY a.created_at DESC
OFFSET $3
LIMIT $4;


-- name: GetOrgAuditLogs :many
SELECT sqlc.embed(a), u.name, u.email
FROM backend.audit_logs a
LEFT JOIN backend.users u ON u.id = a.user_id
WHERE (
    ((a.entity_table = 'organizations' OR a.entity_table = 'organization_users') AND a.entity_id = $1)
    OR (
        a.entity_table = 'properties'
        AND ((a.old_value ->> 'org_id')::bigint = $1 OR (a.new_value ->> 'org_id')::bigint = $1)
    )
)
AND a.created_at >= $2
ORDER BY a.created_at DESC
OFFSET $3
LIMIT $4;
