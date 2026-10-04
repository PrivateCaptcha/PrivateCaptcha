-- name: GetEdgeSettingsBySitekey :one
SELECT p.id AS property_id, p.external_id,
    COALESCE(e.edge_widget_start_mode, 'click'::backend.edge_widget_start_mode)::backend.edge_widget_start_mode AS edge_widget_start_mode,
    COALESCE(e.updated_at, p.updated_at)::timestamptz AS updated_at,
    p.domain, p.challenge
FROM backend.properties p
LEFT JOIN backend.edge_property_settings e ON e.property_id = p.id
WHERE p.external_id = $1 AND p.enabled = TRUE AND p.deleted_at IS NULL
    AND p.edge_token_validity_interval > INTERVAL '0 seconds';

-- name: GetEdgeSettingsBySitekeys :many
SELECT e.*, p.domain, p.challenge
FROM backend.edge_property_settings e
JOIN backend.properties p ON p.id = e.property_id
WHERE e.external_id = ANY($1::UUID[]) AND p.enabled = TRUE AND p.deleted_at IS NULL
    AND p.edge_token_validity_interval > INTERVAL '0 seconds';

-- name: UpsertEdgeSettings :one
WITH property AS (
    SELECT p.id, p.external_id FROM backend.properties p
    WHERE p.id = @property_id AND p.org_id = @org_id
        AND (p.creator_id = @user_id OR p.org_owner_id = @user_id)
        AND p.enabled = TRUE AND p.deleted_at IS NULL
    FOR UPDATE
), old AS (
    SELECT COALESCE(e.edge_widget_start_mode, 'click'::backend.edge_widget_start_mode) AS edge_widget_start_mode
    FROM property p LEFT JOIN backend.edge_property_settings e ON e.property_id = p.id
), upd AS (
    INSERT INTO backend.edge_property_settings AS e (property_id, external_id, edge_widget_start_mode)
    SELECT p.id, p.external_id, COALESCE(sqlc.narg(edge_widget_start_mode)::backend.edge_widget_start_mode, 'click'::backend.edge_widget_start_mode)
    FROM property p
    ON CONFLICT (property_id) DO UPDATE
        SET edge_widget_start_mode = COALESCE(sqlc.narg(edge_widget_start_mode)::backend.edge_widget_start_mode, e.edge_widget_start_mode),
            updated_at = NOW()
    RETURNING e.*
)
SELECT upd.*, old.edge_widget_start_mode AS old_edge_widget_start_mode FROM upd CROSS JOIN old;
