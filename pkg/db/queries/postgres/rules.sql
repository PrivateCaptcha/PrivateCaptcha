-- name: GetDifficultyRulesByPropertyIDs :many
SELECT * FROM backend.difficulty_rules
WHERE property_id = ANY($1::INT[]) AND org_id IS NULL
ORDER BY property_id, position ASC;

-- name: GetDifficultyRulesByOrgIDs :many
SELECT * FROM backend.difficulty_rules
WHERE org_id = ANY($1::INT[]) AND property_id IS NULL
ORDER BY org_id, position ASC;

-- name: CreateDifficultyRule :one
WITH scope_lock AS (
    SELECT pg_advisory_xact_lock(COALESCE($2, -1), COALESCE($3, -1))
),
max_pos AS (
    SELECT COALESCE(MAX(position), -$15::float8) + $15::float8 AS new_position
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
    creator_id,
    terminal
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, (SELECT new_position FROM max_pos), $11, $12, $13, $14
)
RETURNING *;

-- name: GetDifficultyRuleByID :one
SELECT * FROM backend.difficulty_rules
WHERE id = $1;

-- name: UpdateDifficultyRule :one
WITH old AS (
    SELECT * FROM backend.difficulty_rules dr
    WHERE dr.id = $1
    AND (dr.creator_id = $13 OR $13 = $14)
    AND (($15 IS NOT NULL AND dr.property_id = $15 AND dr.org_id IS NULL)
      OR ($16 IS NOT NULL AND dr.org_id = $16 AND dr.property_id IS NULL))
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
        terminal = $12,
        updated_at = NOW()
    WHERE dr.id = (SELECT id FROM old)
    RETURNING *
)
SELECT
    upd.*,
    old.name AS old_name,
    old.enabled AS old_enabled,
    old.position AS old_position,
    old.condition_property AS old_condition_property,
    old.condition_operator AS old_condition_operator,
    old.condition_operator_negated AS old_condition_operator_negated,
    old.condition_value_str AS old_condition_value_str,
    old.condition_value_int AS old_condition_value_int,
    old.action_property AS old_action_property,
    old.action_value AS old_action_value,
    old.terminal AS old_terminal
FROM upd
CROSS JOIN old;

-- name: DeleteDifficultyRule :exec
DELETE FROM backend.difficulty_rules dr
WHERE dr.id = $1
AND (dr.creator_id = $2 OR $2 = $3);

-- name: MoveDifficultyRule :one
UPDATE backend.difficulty_rules
SET position = $2, updated_at = NOW()
WHERE id = $1 AND (creator_id = $3 OR $3 = $4)
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
      AND dr.id != $1
)
SELECT
    prev_row.position AS prev_position,
    next_row.position AS next_position
FROM
    (SELECT 1) AS dummy
LEFT JOIN rules_list prev_row ON prev_row.idx = $2 - 1
LEFT JOIN rules_list next_row ON next_row.idx = $2;

-- name: RebalanceDifficultyRules :exec
WITH rules_list AS (
    SELECT dr.id, ROW_NUMBER() OVER (ORDER BY dr.position ASC) AS row_num
    FROM backend.difficulty_rules dr
    WHERE (dr.property_id = $1 OR (dr.property_id IS NULL AND $1 IS NULL))
      AND (dr.org_id = $2 OR (dr.org_id IS NULL AND $2 IS NULL))
)
UPDATE backend.difficulty_rules dr
SET position = (rl.row_num - 1) * $3::float8, updated_at = NOW()
FROM rules_list rl
WHERE dr.id = rl.id;
