-- name: GetDifficultyRulesByPropertyIDs :many
SELECT sqlc.embed(dr) FROM backend.difficulty_rules dr
WHERE dr.property_id = ANY($1::INT[])
ORDER BY dr.property_id, dr.position ASC;

-- name: GetDifficultyRulesByOrgIDs :many
SELECT sqlc.embed(dr) FROM backend.difficulty_rules dr
WHERE dr.org_id = ANY($1::INT[]) AND dr.property_id IS NULL
ORDER BY dr.org_id, dr.position ASC;
