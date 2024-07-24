-- name: DeleteDeletedRecords :exec
DELETE FROM deleted_record WHERE deleted_at < $1;
