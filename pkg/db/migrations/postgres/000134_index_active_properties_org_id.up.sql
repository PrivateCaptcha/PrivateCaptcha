CREATE INDEX index_property_active_org_id
ON backend.properties(org_id)
WHERE org_id IS NOT NULL AND deleted_at IS NULL;
