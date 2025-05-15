DROP TRIGGER IF EXISTS deleted_record_insert ON backend.subscriptions CASCADE;

DROP INDEX IF EXISTS index_subscription_external;

DROP TABLE IF EXISTS backend.subscriptions;

DROP TYPE backend.subscription_source;
