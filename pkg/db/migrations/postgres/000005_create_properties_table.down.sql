ALTER TABLE properties DROP CONSTRAINT IF EXISTS unique_property_name_per_organization;
DROP INDEX IF EXISTS index_property_external_id;

DROP TABLE IF EXISTS properties;

DROP TYPE difficulty_growth;
DROP TYPE difficulty_level;
