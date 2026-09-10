-- name: GetOrganizationStats :many
SELECT
    o.id AS org_id,
    COUNT(DISTINCT ou.id) + 1 AS members,
    COUNT(DISTINCT p.id) AS properties,
    COUNT(DISTINCT f.id) AS forms,
    COUNT(DISTINCT dr.id) AS rules
FROM backend.organizations o
LEFT JOIN backend.organization_users ou ON ou.org_id = o.id
LEFT JOIN backend.properties p ON p.org_id = o.id AND p.deleted_at IS NULL
LEFT JOIN backend.forms f ON f.org_id = o.id AND f.deleted_at IS NULL
LEFT JOIN backend.difficulty_rules dr ON dr.org_id = o.id OR dr.property_id = p.id
WHERE o.id = ANY($1::INT[]) AND o.deleted_at IS NULL
GROUP BY o.id
ORDER BY o.id;
