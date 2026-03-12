ALTER TABLE backend.user_notifications ADD COLUMN email_from TEXT DEFAULT NULL;
ALTER TABLE backend.user_notifications ADD COLUMN reply_to_email TEXT DEFAULT NULL;
