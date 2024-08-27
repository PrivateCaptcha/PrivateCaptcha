-- name: CreateSupportTicket :one
INSERT INTO backend.support (category, message, user_id) VALUES ($1, $2, $3) RETURNING *;
