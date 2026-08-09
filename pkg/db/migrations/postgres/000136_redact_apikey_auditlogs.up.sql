UPDATE backend.audit_logs
SET
    old_value = CASE
        WHEN jsonb_typeof(old_value) = 'object' THEN old_value - 'external_id'
        ELSE old_value
    END,
    new_value = CASE
        WHEN jsonb_typeof(new_value) = 'object' THEN new_value - 'external_id'
        ELSE new_value
    END
WHERE entity_table = 'apikeys'
  AND (
      (jsonb_typeof(old_value) = 'object' AND old_value ? 'external_id')
      OR (jsonb_typeof(new_value) = 'object' AND new_value ? 'external_id')
  );
