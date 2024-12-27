-- name: CreateSupportTicket :one
INSERT INTO backend.support (category, subject, message, user_id, session_id) VALUES ($1, $2, $3, $4, $5) RETURNING *;
