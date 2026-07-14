-- name: SearchOrg :many
WITH org_properties AS MATERIALIZED (
    SELECT p.id, p.org_id, p.name, p.domain, p.external_id, p.updated_at
    FROM backend.properties p
    WHERE p.org_id = sqlc.arg(org_id)::int
      AND p.deleted_at IS NULL
      AND p.enabled = TRUE
),
search_results AS (
    SELECT
        p.id,
        'property'::text AS type,
        p.name,
        p.domain AS description,
        p.external_id,
        p.updated_at
    FROM org_properties p

    UNION ALL

    SELECT
        f.id,
        'form'::text AS type,
        f.name,
        f.url AS description,
        f.external_id,
        f.updated_at
    FROM org_properties p
    JOIN backend.forms f ON f.property_id = p.id AND f.org_id = p.org_id
    WHERE f.deleted_at IS NULL AND f.enabled = TRUE
)
SELECT id, type, name, description, updated_at
FROM search_results
WHERE strpos(lower(name), lower(sqlc.arg(search_term)::text)) > 0
   OR strpos(lower(description), lower(sqlc.arg(search_term)::text)) > 0
   OR strpos(replace(external_id::text, '-', ''), lower(sqlc.arg(search_term)::text)) > 0
ORDER BY updated_at DESC, type ASC, id ASC
OFFSET sqlc.arg(result_offset)::int
LIMIT sqlc.arg(result_limit)::int;
