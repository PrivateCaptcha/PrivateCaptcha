-- name: DeleteDeletedRecords :exec
DELETE FROM deleted_record WHERE deleted_at < NOW() - '1 year'::interval;
