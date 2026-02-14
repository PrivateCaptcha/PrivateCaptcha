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

-- name: GetDifficultyRuleByIDAndProperty :one
SELECT dr.* FROM backend.difficulty_rules dr
WHERE dr.id = $1 AND dr.property_id = $2;

-- name: GetDifficultyRuleByIDAndOrg :one
SELECT dr.* FROM backend.difficulty_rules dr
WHERE dr.id = $1 AND dr.org_id = $2;

-- name: UpdateDifficultyRuleByProperty :one
UPDATE backend.difficulty_rules SET
    name = $3,
    enabled = $4,
    condition_property = $5,
    condition_operator = $6,
    condition_operator_negated = $7,
    condition_value_str = $8,
    condition_value_int = $9,
    condition_value_separator = $10,
    action_property = $11,
    action_value = $12,
    updated_at = NOW()
WHERE id = $1 AND property_id = $2
RETURNING *;

-- name: UpdateDifficultyRuleByOrg :one
UPDATE backend.difficulty_rules SET
    name = $3,
    enabled = $4,
    condition_property = $5,
    condition_operator = $6,
    condition_operator_negated = $7,
    condition_value_str = $8,
    condition_value_int = $9,
    condition_value_separator = $10,
    action_property = $11,
    action_value = $12,
    updated_at = NOW()
WHERE id = $1 AND org_id = $2
RETURNING *;
