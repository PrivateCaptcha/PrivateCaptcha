-- Create a subscription with a 100-year trial
WITH subscription_insert AS (
    INSERT INTO backend.subscriptions (external_product_id, external_price_id, external_subscription_id, external_customer_id, status, source, trial_ends_at)
    VALUES ('{{.ExternalProductID}}', '{{.ExternalPriceID}}', NULL, NULL, '{{.ExternalStatus}}', 'internal', CURRENT_TIMESTAMP + INTERVAL '100 years')
    RETURNING id
), user_insert AS (
    -- Create an admin user
    INSERT INTO backend.users (name, email, subscription_id)
    SELECT 'PC Admin', '{{ .AdminEmail }}', id FROM subscription_insert
    RETURNING id
), org_insert AS (
    -- Create an organization for the admin user
    INSERT INTO backend.organizations (name, user_id)
    SELECT 'Private Captcha', id FROM user_insert
    RETURNING id AS org_id, user_id
), notify_insert AS (
INSERT INTO backend.system_notifications (message, end_date, user_id)
    SELECT 'This is a <i>test</i> system notification for <strong>{{.Stage}}</strong>', CURRENT_TIMESTAMP + INTERVAL '1 day', id FROM user_insert
    RETURNING id as notification_id
)
INSERT INTO backend.properties (name, external_id, org_id, creator_id, org_owner_id, domain, level, growth)
SELECT
    name,
    external_id,
    org_id,
    user_id,
    user_id,
    '{{ .PortalDomain }}',
    difficulty,
    'fast'
FROM org_insert
CROSS JOIN (
    VALUES
        ('Portal login', '{{ .PortalLoginPropertyID }}'::uuid, '{{.PortalLoginDifficulty}}'::smallint),
        ('Portal register', '{{ .PortalRegisterPropertyID }}'::uuid, '{{.PortalRegisterDifficulty}}'::smallint)
) AS props(name, external_id, difficulty);

