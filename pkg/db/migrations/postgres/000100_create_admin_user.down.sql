WITH admin_user AS (
    SELECT creator_id
    FROM backend.properties
    WHERE external_id = '{{ .PortalLoginPropertyID }}'
), delete_subscription AS (
    -- First, delete the subscription associated with the user
    DELETE FROM backend.subscriptions
    WHERE id = (
        SELECT subscription_id
        FROM backend.users
        WHERE id = (SELECT creator_id FROM admin_user)
    )
)
-- Then, delete the user (this will cascade delete everything else)
DELETE FROM backend.users
WHERE id = (SELECT creator_id FROM admin_user);
