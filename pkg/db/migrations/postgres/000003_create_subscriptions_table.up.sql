CREATE TYPE backend.subscription_source AS ENUM ('external', 'internal');

CREATE TABLE IF NOT EXISTS backend.subscriptions(
    id SERIAL PRIMARY KEY,
    external_product_id TEXT NOT NULL,
    external_price_id TEXT NOT NULL,
    external_subscription_id TEXT,
    external_customer_id TEXT,
    status VARCHAR(255) NOT NULL,
    source backend.subscription_source NOT NULL DEFAULT 'internal',
    trial_ends_at TIMESTAMPTZ,
    next_billed_at TIMESTAMPTZ,
    cancel_from TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp
);

CREATE UNIQUE INDEX IF NOT EXISTS index_subscription_external ON backend.subscriptions(external_subscription_id);

CREATE OR REPLACE TRIGGER deleted_record_insert AFTER DELETE ON backend.subscriptions
   FOR EACH ROW EXECUTE FUNCTION backend.deleted_record_insert();
