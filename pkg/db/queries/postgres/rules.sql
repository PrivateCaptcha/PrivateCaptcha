-- name: GetDifficultyRulesByPropertyID :many
SELECT * FROM backend.difficulty_rules WHERE property_id = $1 ORDER BY position ASC;

-- name: GetDifficultyRulesByOrgID :many
SELECT * FROM backend.difficulty_rules WHERE org_id = $1 AND property_id IS NULL ORDER BY position ASC;

-- name: GetDifficultyRulesByPropertyIDs :many
SELECT * FROM backend.difficulty_rules WHERE property_id = ANY($1::INT[]) ORDER BY property_id, position ASC;

-- name: GetDifficultyRulesByOrgIDs :many
SELECT * FROM backend.difficulty_rules WHERE org_id = ANY($1::INT[]) AND property_id IS NULL ORDER BY org_id, position ASC;
