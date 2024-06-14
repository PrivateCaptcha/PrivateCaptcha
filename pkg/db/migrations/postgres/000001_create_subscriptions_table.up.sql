CREATE TABLE IF NOT EXISTS subscriptions(
    id SERIAL PRIMARY KEY,
    paddle_product_id TEXT NOT NULL,
    paddle_price_id TEXT NOT NULL,
    paddle_subscription_id TEXT NOT NULL,
    paddle_customer_id TEXT NOT NULL,
    status VARCHAR(255) NOT NULL,
    trial_ends_at TIMESTAMPTZ,
    next_billed_at TIMESTAMPTZ,
    cancel_from TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp
);

CREATE UNIQUE INDEX IF NOT EXISTS index_subscription_paddle ON subscriptions(paddle_subscription_id);
