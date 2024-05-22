-- name: CreateSupportTicket :one
INSERT INTO support (category, message, user_id) VALUES ($1, $2, $3) RETURNING *;
