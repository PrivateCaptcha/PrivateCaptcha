CREATE TABLE IF NOT EXISTS usage_limit_violations(
    user_id INTEGER REFERENCES users(id),
    paddle_product_id TEXT NOT NULL,
    requests_limit BIGINT NOT NULL,
    requests_count BIGINT NOT NULL,
    detection_month date NOT NULL,
    last_violated_at date NOT NULL,
    PRIMARY KEY (user_id, paddle_product_id, detection_month)
);
