-- name: GetSystemNotificationById :one
SELECT * FROM backend.system_notifications WHERE id = $1;

-- name: GetLastActiveSystemNotification :one
SELECT * FROM backend.system_notifications
 WHERE is_active = TRUE AND
   start_date <= $1::timestamptz AND
   (end_date IS NULL OR end_date > $1::timestamptz) AND
   (user_id = $2 OR user_id IS NULL)
 ORDER BY
   CASE WHEN user_id = $2 THEN 0 ELSE 1 END,
   start_date DESC
 LIMIT 1;

-- name: CreateSystemNotification :one
INSERT INTO backend.system_notifications (message, start_date, end_date, user_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateNotificationTemplate :one
INSERT INTO backend.notification_templates (name, content_html, content_text, external_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (external_id) DO UPDATE SET updated_at = NOW()
RETURNING *;

-- name: GetNotificationTemplateByHash :one
SELECT * FROM backend.notification_templates WHERE external_id = $1;

-- name: CreateUserNotification :one
INSERT INTO backend.user_notifications (user_id, reference_id, template_id, subject, payload, scheduled_at, persistent, persist_until, requires_subscription, email_from, reply_to_email, email_to)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (user_id, reference_id) WHERE (persist_until IS NOT NULL) OR (processed_at IS NULL)
DO UPDATE SET
  template_id = EXCLUDED.template_id,
  subject = EXCLUDED.subject,
  payload = EXCLUDED.payload,
  scheduled_at = EXCLUDED.scheduled_at,
  persistent = EXCLUDED.persistent,
  persist_until = EXCLUDED.persist_until,
  requires_subscription = EXCLUDED.requires_subscription,
  email_from = EXCLUDED.email_from,
  reply_to_email = EXCLUDED.reply_to_email,
  email_to = EXCLUDED.email_to,
  processed_at = NULL,
  processing_attempts = 0,
  updated_at = NOW()
WHERE backend.user_notifications.processed_at IS NOT NULL
  AND backend.user_notifications.persist_until < NOW()
RETURNING *;

-- name: DeletePendingUserNotification :exec
DELETE FROM backend.user_notifications WHERE processed_at IS NULL AND user_id = $1 AND reference_id = $2;

-- name: UpdateProcessedUserNotifications :exec
UPDATE backend.user_notifications SET
  processed_at = $1,
  processing_attempts = processing_attempts + 1,
  updated_at = NOW()
WHERE id = ANY($2::INT[]);

-- name: UpdateAttemptedUserNotifications :exec
UPDATE backend.user_notifications SET
  processing_attempts = processing_attempts + 1,
  updated_at = NOW()
WHERE id = ANY($1::INT[]);

-- name: GetPendingUserNotifications :many
SELECT sqlc.embed(un), un.email_to as notification_email, u.email AS user_email, u.subscription_id, s.status
FROM backend.user_notifications un
JOIN backend.users u ON un.user_id = u.id
LEFT JOIN backend.subscriptions s ON u.subscription_id = s.id
WHERE un.processed_at IS NULL
  AND un.scheduled_at >= $1
  AND un.scheduled_at <= NOW()
  AND u.deleted_at IS NULL
  AND un.processing_attempts < $2
  AND (un.requires_subscription IS NULL OR u.subscription_id IS NOT NULL)
ORDER BY un.scheduled_at ASC
LIMIT $3;

-- name: DeleteUnusedNotificationTemplates :exec
DELETE FROM backend.notification_templates nt
WHERE nt.id IN (
    SELECT nt2.id
    FROM backend.notification_templates nt2
    LEFT JOIN backend.user_notifications un ON un.template_id = nt2.external_id
    WHERE ((un.template_id IS NULL) OR (un.processed_at < $1))
    AND (nt2.updated_at < $2)
);

-- name: DeleteProcessedUserNotifications :exec
DELETE FROM backend.user_notifications
WHERE processed_at IS NOT NULL
AND (persist_until IS NULL OR persist_until < NOW())
AND processed_at < $1;

-- name: DeleteUnprocessedUserNotifications :exec
DELETE FROM backend.user_notifications
WHERE processed_at IS NULL
AND (persist_until IS NULL OR persist_until < NOW())
AND scheduled_at < $1;
