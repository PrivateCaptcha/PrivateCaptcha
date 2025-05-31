DROP TRIGGER IF EXISTS deleted_record_insert ON backend.users CASCADE;

DROP INDEX IF EXISTS index_user_email;

DROP TABLE IF EXISTS backend.users;
