ALTER TABLE backend.user_settings ALTER COLUMN notifications_email DROP NOT NULL;
ALTER TABLE backend.user_settings ALTER COLUMN notifications_email DROP DEFAULT;
ALTER TABLE backend.user_settings ALTER COLUMN notifications_email SET DEFAULT NULL;
UPDATE backend.user_settings SET notifications_email = NULL WHERE notifications_email = '';
