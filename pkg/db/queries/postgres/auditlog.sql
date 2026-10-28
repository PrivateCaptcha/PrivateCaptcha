-- name: CreateAuditLogs :copyfrom
INSERT INTO backend.audit_logs (user_id, action, source, entity_id, entity_table, session_id, old_value, new_value, created_at, ip_address)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: DeleteOldAuditLogs :execrows
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
        a.entity_table IN ('properties', 'edge_property_settings')
        AND ((a.old_value ->> 'creator_id')::bigint = $1 OR (a.new_value ->> 'creator_id')::bigint = $1)
        AND EXISTS (
            SELECT 1
            FROM backend.organizations o
            LEFT JOIN backend.organization_users ou ON ou.org_id = o.id AND ou.user_id = $1
            WHERE o.id = COALESCE((a.new_value ->> 'org_id')::int, (a.old_value ->> 'org_id')::int)
              AND (o.user_id = $1 OR (ou.user_id = $1 AND ou.level IN ('member', 'owner')))
        )
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
WHERE a.entity_table IN ('properties', 'edge_property_settings') AND a.entity_id = $1 AND a.created_at >= $2
ORDER BY a.created_at DESC
OFFSET $3
LIMIT $4;

-- name: GetFormAuditLogs :many
SELECT sqlc.embed(a), u.name, u.email
FROM backend.audit_logs a
LEFT JOIN backend.users u ON u.id = a.user_id
WHERE a.entity_table = 'forms' AND a.entity_id = $1 AND a.created_at >= $2
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
        a.entity_table IN ('properties', 'edge_property_settings')
        AND ((a.old_value ->> 'org_id')::bigint = $1 OR (a.new_value ->> 'org_id')::bigint = $1)
    )
)
AND a.created_at >= $2
ORDER BY a.created_at DESC
OFFSET $3
LIMIT $4;
