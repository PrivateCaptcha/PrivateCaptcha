DROP TRIGGER IF EXISTS deleted_record_insert ON backend.properties CASCADE;

ALTER TABLE backend.properties DROP CONSTRAINT IF EXISTS unique_property_name_per_organization;
DROP INDEX IF EXISTS index_property_external_id;

DROP TABLE IF EXISTS backend.properties;

DROP TYPE backend.difficulty_growth;
DROP TYPE backend.difficulty_level;
