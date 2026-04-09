-- name: DeleteDeletedRecords :execrows
DELETE FROM backend.deleted_records WHERE deleted_at < $1;
