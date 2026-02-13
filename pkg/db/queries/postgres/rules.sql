-- name: GetDifficultyRulesByPropertyIDs :many
SELECT dr.* FROM backend.difficulty_rules dr
WHERE dr.property_id = ANY($1::INT[]) AND dr.org_id IS NULL
ORDER BY dr.property_id, dr.position ASC;

-- name: GetDifficultyRulesByOrgIDs :many
SELECT dr.* FROM backend.difficulty_rules dr
WHERE dr.org_id = ANY($1::INT[]) AND dr.property_id IS NULL
ORDER BY dr.org_id, dr.position ASC;

-- name: CreateDifficultyRule :one
INSERT INTO backend.difficulty_rules (
    name,
    property_id,
    org_id,
    enabled,
    condition_property,
    condition_operator,
    condition_operator_negated,
    condition_value_str,
    condition_value_int,
    condition_value_separator,
    position,
    action_property,
    action_value
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING *;
