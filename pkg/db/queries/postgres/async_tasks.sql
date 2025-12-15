-- name: GetAsyncTask :one
SELECT * FROM backend.async_tasks WHERE id = $1;

-- name: CreateAsyncTask :one
INSERT INTO backend.async_tasks (input, handler, user_id, reference_id, scheduled_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: GetPendingAsyncTasks :many
SELECT sqlc.embed(ar)
FROM backend.async_tasks ar
INNER JOIN backend.users u ON ar.user_id = u.id
WHERE ar.processed_at IS NULL
  AND ar.scheduled_at >= $1
  AND ar.scheduled_at <= NOW()
  AND u.deleted_at IS NULL
  AND ar.processing_attempts < $2
ORDER BY
    (ar.processing_attempts > 0),  -- false (0 attempts) first
    random()
LIMIT $3;

-- name: UpdateAsyncTask :exec
UPDATE backend.async_tasks SET
  processed_at = $2,
  processing_attempts = processing_attempts + 1,
  output = $3
WHERE id = $1;

-- name: DeleteOldAsyncTasks :exec
DELETE FROM backend.async_tasks WHERE created_at < $1;
