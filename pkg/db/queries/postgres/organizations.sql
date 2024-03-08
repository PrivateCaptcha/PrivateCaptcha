-- name: CreateOrganization :one
INSERT INTO organizations (org_name, user_id) VALUES ($1, $2) RETURNING *;
