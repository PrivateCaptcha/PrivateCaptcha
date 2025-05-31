-- name: DeleteDeletedRecords :exec
DELETE FROM backend.deleted_records WHERE deleted_at < $1;
