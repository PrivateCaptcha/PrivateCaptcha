DROP TRIGGER IF EXISTS deleted_record_insert ON subscriptions CASCADE;

DROP INDEX IF EXISTS index_subscription_paddle;

DROP TABLE IF EXISTS subscriptions;
