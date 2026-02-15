-- name: GetDifficultyRulesByPropertyIDs :many
SELECT * FROM backend.difficulty_rules
WHERE property_id = ANY($1::INT[]) AND org_id IS NULL
ORDER BY property_id, position ASC;

-- name: GetDifficultyRulesByOrgIDs :many
SELECT * FROM backend.difficulty_rules
WHERE org_id = ANY($1::INT[]) AND property_id IS NULL
ORDER BY org_id, position ASC;

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
    action_value,
    creator_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING *;

-- name: GetDifficultyRuleByID :one
SELECT * FROM backend.difficulty_rules
WHERE id = $1;

-- name: UpdateDifficultyRule :one
WITH old AS (
    SELECT * FROM backend.difficulty_rules dr
    WHERE dr.id = $1
    AND (dr.creator_id = $12 OR $13 = $12)
    AND ((dr.property_id IS NOT NULL AND (dr.property_id = $14 OR $14 IS NULL)) OR (dr.org_id IS NOT NULL AND (dr.org_id = $15 OR $15 IS NULL)))
    FOR UPDATE
),
upd AS (
    UPDATE backend.difficulty_rules dr SET
        name = $2,
        enabled = $3,
        condition_property = $4,
        condition_operator = $5,
        condition_operator_negated = $6,
        condition_value_str = $7,
        condition_value_int = $8,
        condition_value_separator = $9,
        action_property = $10,
        action_value = $11,
        updated_at = NOW()
    WHERE dr.id = (SELECT id FROM old)
    RETURNING *
)
SELECT
    upd.*,
    old.name AS old_name,
    old.enabled AS old_enabled,
    old.condition_property AS old_condition_property,
    old.condition_operator AS old_condition_operator,
    old.condition_operator_negated AS old_condition_operator_negated,
    old.condition_value_str AS old_condition_value_str,
    old.condition_value_int AS old_condition_value_int,
    old.action_property AS old_action_property,
    old.action_value AS old_action_value
FROM upd
CROSS JOIN old;
