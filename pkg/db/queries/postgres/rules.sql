-- name: GetDifficultyRulesByPropertyIDs :many
SELECT dr.* FROM backend.difficulty_rules dr
WHERE dr.property_id = ANY($1::INT[]) AND dr.org_id IS NULL
ORDER BY dr.property_id, dr.position ASC;

-- name: GetDifficultyRulesByOrgIDs :many
SELECT dr.* FROM backend.difficulty_rules dr
WHERE dr.org_id = ANY($1::INT[]) AND dr.property_id IS NULL
ORDER BY dr.org_id, dr.position ASC;
