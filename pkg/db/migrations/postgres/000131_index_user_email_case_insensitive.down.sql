DROP INDEX IF EXISTS index_user_email_lower;

CREATE UNIQUE INDEX IF NOT EXISTS index_user_email ON backend.users(email);
