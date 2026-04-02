UPDATE backend.user_settings SET notifications_email = '' WHERE notifications_email IS NULL;
ALTER TABLE backend.user_settings ALTER COLUMN notifications_email SET NOT NULL;
ALTER TABLE backend.user_settings ALTER COLUMN notifications_email SET DEFAULT '';
