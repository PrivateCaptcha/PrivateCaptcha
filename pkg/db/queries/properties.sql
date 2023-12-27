-- name: GetPropertyByExternalID :one
SELECT * FROM properties WHERE external_id = $1;
