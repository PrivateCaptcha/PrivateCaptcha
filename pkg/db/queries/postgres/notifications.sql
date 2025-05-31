-- name: GetNotificationById :one
SELECT * FROM backend.system_notifications WHERE id = $1;

-- name: GetLastActiveNotification :one
SELECT * FROM backend.system_notifications
 WHERE is_active = TRUE AND
   start_date <= $1::timestamptz AND
   (end_date IS NULL OR end_date > $1::timestamptz) AND
   (user_id = $2 OR user_id IS NULL)
 ORDER BY
   CASE WHEN user_id = $2 THEN 0 ELSE 1 END,
   start_date DESC
 LIMIT 1;

-- name: CreateNotification :one
INSERT INTO backend.system_notifications (message, start_date, end_date, user_id)
VALUES ($1, $2, $3, $4)
RETURNING *;
