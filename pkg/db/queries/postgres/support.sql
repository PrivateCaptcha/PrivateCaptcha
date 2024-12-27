-- name: CreateSupportTicket :one
INSERT INTO backend.support (category, message, user_id, session_id) VALUES ($1, $2, $3, $4) RETURNING *;
