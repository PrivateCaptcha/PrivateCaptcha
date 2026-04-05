-- name: GetUserSettings :one
SELECT * FROM backend.user_settings WHERE user_id = $1;

-- name: UpsertUserSettings :one
INSERT INTO backend.user_settings (user_id, weekly_report, monthly_report, notifications_email)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE SET
    weekly_report = EXCLUDED.weekly_report,
    monthly_report = EXCLUDED.monthly_report,
    notifications_email = EXCLUDED.notifications_email,
    updated_at = NOW()
RETURNING *;

-- name: GetUsersWithPendingWeeklyReport :many
SELECT us.user_id, us.notifications_email, u.email, COALESCE(s.status, '') as subscription_status
FROM backend.user_settings us
JOIN backend.users u ON us.user_id = u.id
LEFT JOIN backend.subscriptions s ON u.subscription_id = s.id
WHERE us.weekly_report = TRUE AND u.deleted_at IS NULL AND u.subscription_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM backend.user_notifications un
    WHERE un.user_id = us.user_id
      AND un.reference_id = sqlc.arg(reference_prefix)::TEXT || us.user_id::TEXT || sqlc.arg(reference_suffix)::TEXT
      AND un.processed_at IS NULL
  )
ORDER BY us.user_id
LIMIT $1 OFFSET $2;

-- name: GetUsersWithPendingMonthlyReport :many
SELECT us.user_id, us.notifications_email, u.email, COALESCE(s.status, '') as subscription_status
FROM backend.user_settings us
JOIN backend.users u ON us.user_id = u.id
LEFT JOIN backend.subscriptions s ON u.subscription_id = s.id
WHERE us.monthly_report = TRUE AND u.deleted_at IS NULL AND u.subscription_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM backend.user_notifications un
    WHERE un.user_id = us.user_id
      AND un.reference_id = sqlc.arg(reference_prefix)::TEXT || us.user_id::TEXT || sqlc.arg(reference_suffix)::TEXT
      AND un.processed_at IS NULL
  )
ORDER BY us.user_id
LIMIT $1 OFFSET $2;
