-- name: GetDifficultyRulesByPropertyID :many
SELECT * FROM backend.difficulty_rules WHERE property_id = $1 ORDER BY position ASC;

-- name: GetDifficultyRulesByOrgID :many
SELECT * FROM backend.difficulty_rules WHERE org_id = $1 AND property_id IS NULL ORDER BY position ASC;
