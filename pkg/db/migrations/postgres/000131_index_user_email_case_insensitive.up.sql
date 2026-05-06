DROP INDEX IF EXISTS index_user_email;

CREATE UNIQUE INDEX IF NOT EXISTS index_user_email_lower ON backend.users(LOWER(email));
