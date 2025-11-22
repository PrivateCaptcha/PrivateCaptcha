-------------------------------------------------
--------------------- USERS ---------------------
-------------------------------------------------
INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, new_value, created_at)
SELECT
    id AS user_id,
    'create'::backend.audit_log_action,
    id AS entity_id,
    'users',
    '',
    jsonb_build_object(
        'name', name,
        'email', email,
        'subscription_id', subscription_id
    ) AS new_value,
    created_at
FROM backend.users;

INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, old_value, created_at)
SELECT
    id AS user_id,
    'softdelete'::backend.audit_log_action,
    id AS entity_id,
    'users',
    '',
    jsonb_build_object(
        'name', name,
        'email', email,
        'subscription_id', subscription_id
    ) AS old_value,
    deleted_at
FROM backend.users
WHERE deleted_at IS NOT NULL;


---------------------------------------------------------
--------------------- ORGANIZATIONS ---------------------
---------------------------------------------------------
INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, new_value, created_at)
SELECT
    user_id,
    'create'::backend.audit_log_action,
    id,
    'organizations',
    '',
    jsonb_build_object(
        'name', name
    ) AS new_value,
    created_at
FROM backend.organizations;

INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, old_value, created_at)
SELECT
    user_id,
    'softdelete'::backend.audit_log_action,
    id,
    'organizations',
    '',
    jsonb_build_object(
        'name', name
    ) AS old_value,
    deleted_at
FROM backend.organizations
WHERE deleted_at IS NOT NULL;

--------------------------------------------------------------
--------------------- ORGANIZATION_USERS ---------------------
--------------------------------------------------------------
INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, new_value, created_at)
SELECT
    org.user_id AS user_id,
    'create'::backend.audit_log_action,
    ou.user_id AS entity_id,
    'organization_users',
    '',
    jsonb_build_object(
        'org_name', org.name,
        'user_id', ou.user_id,
        'level', ou.level
    ) AS new_value,
    ou.created_at
FROM backend.organization_users ou
JOIN backend.organizations org ON org.id = ou.org_id;

------------------------------------------------------
--------------------- PROPERTIES ---------------------
------------------------------------------------------
INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, new_value, created_at)
SELECT
    creator_id AS user_id,
    'create'::backend.audit_log_action,
    id,
    'properties',
    '',
    jsonb_build_object(
        'name', name,
        'org_id', org_id,
        'creator_id', creator_id,
        'org_owner_id', org_owner_id,
        'domain', domain,
        'level', level,
        'growth', growth,
        'validity_interval_s', EXTRACT(seconds FROM validity_interval)::int,
        'allow_subdomains', allow_subdomains,
        'allow_localhost', allow_localhost,
        'max_replay_count', max_replay_count
    ) AS new_value,
    created_at
FROM backend.properties;

INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, old_value, created_at)
SELECT
    creator_id AS user_id,
    'softdelete'::backend.audit_log_action,
    id,
    'properties',
    '',
    jsonb_build_object(
        'name', name,
        'org_id', org_id,
        'org_owner_id', org_owner_id,
        'domain', domain,
        'level', level,
        'growth', growth,
        'validity_interval_s', EXTRACT(seconds FROM validity_interval)::int,
        'allow_subdomains', allow_subdomains,
        'allow_localhost', allow_localhost,
        'max_replay_count', max_replay_count
    ) AS old_value,
    deleted_at
FROM backend.properties
WHERE deleted_at IS NOT NULL;

----------------------------------------------------
--------------------- API KEYS ---------------------
----------------------------------------------------
INSERT INTO backend.audit_logs (user_id, action, entity_id, entity_table, session_id, new_value, created_at)
SELECT
    user_id,
    'create'::backend.audit_log_action,
    id,
    'apikeys',
    '',
    jsonb_build_object(
        'id', id,
        'name', name,
        'external_id', replace(external_id::text, '-', ''),
        'user_id', user_id,
        'enabled', enabled,
        'requests_per_second', requests_per_second,
        'requests_burst', requests_burst,
        'expires_at', expires_at,
        'notes', notes
    ) AS new_value,
    created_at
FROM backend.apikeys;
