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

-- name: ClaimAsyncTask :execrows
-- Atomically claim a pending task for execution. Increments processing_attempts
-- as the claim token. Returns 0 rows if the task is already completed
-- (processed_at IS NOT NULL) or has exhausted its attempts, so concurrent
-- runners (immediate attempt vs. worker) cannot execute it more than
-- MaxAttempts times.
UPDATE backend.async_tasks
SET processing_attempts = processing_attempts + 1
WHERE id = $1
  AND processed_at IS NULL
  AND processing_attempts < $2;

-- name: UpdateAsyncTask :execrows
-- Write the result of a claimed task. The processing_attempts increment is
-- performed by ClaimAsyncTask. The processed_at IS NULL guard prevents a
-- late/stale run from clobbering a task that another runner already
-- completed.
UPDATE backend.async_tasks SET
  processed_at = $2,
  output = $3
WHERE id = $1
  AND processed_at IS NULL;

-- name: DeleteOldAsyncTasks :execrows
DELETE FROM backend.async_tasks WHERE created_at < $1;
