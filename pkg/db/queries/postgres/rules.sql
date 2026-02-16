-- name: GetDifficultyRulesByPropertyIDs :many
SELECT * FROM backend.difficulty_rules
WHERE property_id = ANY($1::INT[]) AND org_id IS NULL
ORDER BY property_id, position ASC;

-- name: GetDifficultyRulesByOrgIDs :many
SELECT * FROM backend.difficulty_rules
WHERE org_id = ANY($1::INT[]) AND property_id IS NULL
ORDER BY org_id, position ASC;

-- name: CreateDifficultyRule :one
WITH max_pos AS (
    SELECT COALESCE(MAX(position), -1000.0) + 1000.0 AS new_position
    FROM backend.difficulty_rules
    WHERE (property_id = $2 OR (property_id IS NULL AND $2 IS NULL))
      AND (org_id = $3 OR (org_id IS NULL AND $3 IS NULL))
)
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
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, (SELECT new_position FROM max_pos), $11, $12, $13
)
RETURNING *;

-- name: GetDifficultyRuleByID :one
SELECT * FROM backend.difficulty_rules
WHERE id = $1;

-- name: UpdateDifficultyRule :one
WITH old AS (
    SELECT * FROM backend.difficulty_rules dr
    WHERE dr.id = $1
    AND (dr.creator_id = $12 OR $12 = $13)
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

-- name: DeleteDifficultyRule :exec
DELETE FROM backend.difficulty_rules WHERE id = $1;

-- name: MoveDifficultyRule :one
UPDATE backend.difficulty_rules
SET position = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetDifficultyRulePositionNeighbors :one
WITH target AS (
    SELECT t.position, t.property_id, t.org_id
    FROM backend.difficulty_rules t
    WHERE t.id = $1
),
rules_list AS (
    SELECT dr.id AS id, dr.position AS position,
           ROW_NUMBER() OVER (ORDER BY dr.position ASC) - 1 AS idx
    FROM backend.difficulty_rules dr
    CROSS JOIN target
    WHERE (dr.property_id = target.property_id OR (dr.property_id IS NULL AND target.property_id IS NULL))
      AND (dr.org_id = target.org_id OR (dr.org_id IS NULL AND target.org_id IS NULL))
)
SELECT
    (SELECT position FROM rules_list WHERE idx = $2 - 1) AS prev_position,
    (SELECT position FROM rules_list WHERE idx = $2) AS next_position;

-- name: RebalanceDifficultyRulesForProperty :exec
WITH rules_list AS (
    SELECT dr.id, ROW_NUMBER() OVER (ORDER BY dr.position ASC) AS row_num
    FROM backend.difficulty_rules dr
    WHERE dr.property_id = $1 AND dr.org_id IS NULL
)
UPDATE backend.difficulty_rules dr
SET position = (rl.row_num - 1) * $2::float8, updated_at = NOW()
FROM rules_list rl
WHERE dr.id = rl.id;

-- name: RebalanceDifficultyRulesForOrg :exec
WITH rules_list AS (
    SELECT dr.id, ROW_NUMBER() OVER (ORDER BY dr.position ASC) AS row_num
    FROM backend.difficulty_rules dr
    WHERE dr.org_id = $1 AND dr.property_id IS NULL
)
UPDATE backend.difficulty_rules dr
SET position = (rl.row_num - 1) * $2::float8, updated_at = NOW()
FROM rules_list rl
WHERE dr.id = rl.id;
