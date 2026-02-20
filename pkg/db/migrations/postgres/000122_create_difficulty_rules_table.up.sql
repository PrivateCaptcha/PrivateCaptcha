CREATE TYPE backend.rule_condition_property AS ENUM ('user_agent', 'ip_address', 'country_code', 'domain');
CREATE TYPE backend.rule_condition_operator AS ENUM ('equals', 'contains', 'matches', 'empty', 'in');
CREATE TYPE backend.rule_action_property AS ENUM ('difficulty_level_percent', 'http_request', 'difficulty_growth');

CREATE TABLE IF NOT EXISTS backend.difficulty_rules(
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    property_id INT REFERENCES backend.properties(id) ON DELETE CASCADE,
    org_id INT REFERENCES backend.organizations(id) ON DELETE CASCADE,
    creator_id INT REFERENCES backend.users(id) ON DELETE SET NULL,
    enabled BOOL NOT NULL DEFAULT TRUE,
    condition_property backend.rule_condition_property NOT NULL,
    condition_operator backend.rule_condition_operator NOT NULL,
    condition_operator_negated BOOL NOT NULL DEFAULT FALSE,
    condition_value_str VARCHAR(512),
    condition_value_int INT,
    condition_value_separator CHAR(1),
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    action_property backend.rule_action_property NOT NULL,
    action_value INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    CONSTRAINT difficulty_rules_scope CHECK (
        (property_id IS NOT NULL AND org_id IS NULL) OR
        (property_id IS NULL AND org_id IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS index_difficulty_rules_property_position ON backend.difficulty_rules(property_id, position) WHERE property_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS index_difficulty_rules_org_position ON backend.difficulty_rules(org_id, position) WHERE org_id IS NOT NULL;

CREATE OR REPLACE TRIGGER deleted_record_insert AFTER DELETE ON backend.difficulty_rules
    FOR EACH ROW EXECUTE FUNCTION backend.deleted_record_insert();
