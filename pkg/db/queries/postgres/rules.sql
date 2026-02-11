-- name: GetDifficultyRulesForProperties :many
WITH property_orgs AS (
    SELECT DISTINCT p.org_id FROM backend.properties p WHERE p.id = ANY($1::INT[]) AND p.org_id IS NOT NULL
)
SELECT sqlc.embed(dr) FROM backend.difficulty_rules dr
WHERE dr.property_id = ANY($1::INT[])
   OR (dr.property_id IS NULL AND dr.org_id IN (SELECT org_id FROM property_orgs))
ORDER BY dr.property_id NULLS LAST, dr.position ASC;
