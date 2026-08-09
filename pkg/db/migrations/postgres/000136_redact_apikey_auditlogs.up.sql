UPDATE backend.audit_logs
SET
    old_value = old_value - 'external_id',
    new_value = new_value - 'external_id'
WHERE entity_table = 'apikeys'
  AND (old_value ? 'external_id' OR new_value ? 'external_id');
