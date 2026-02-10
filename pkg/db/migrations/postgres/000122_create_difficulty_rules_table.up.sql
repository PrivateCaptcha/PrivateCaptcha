CREATE TYPE backend.rule_condition_property AS ENUM ('user_agent', 'ip_address', 'country_code');
CREATE TYPE backend.rule_condition_operator AS ENUM ('equals', 'contains', 'matches');
CREATE TYPE backend.rule_action_property AS ENUM ('difficulty_level', 'http_request', 'difficulty_growth');

CREATE TABLE IF NOT EXISTS backend.difficulty_rules(
    id SERIAL PRIMARY KEY,
    property_id INT REFERENCES backend.properties(id) ON DELETE CASCADE,
    org_id INT REFERENCES backend.organizations(id) ON DELETE CASCADE,
    condition_property backend.rule_condition_property NOT NULL,
    condition_operator backend.rule_condition_operator NOT NULL,
    condition_value_str VARCHAR(512),
    condition_value_int INT,
    position INT NOT NULL DEFAULT 0,
    action_property backend.rule_action_property NOT NULL,
    action_value INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    CONSTRAINT difficulty_rules_scope CHECK (property_id IS NOT NULL OR org_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS index_difficulty_rules_property_id ON backend.difficulty_rules(property_id) WHERE property_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS index_difficulty_rules_org_id ON backend.difficulty_rules(org_id) WHERE org_id IS NOT NULL;
