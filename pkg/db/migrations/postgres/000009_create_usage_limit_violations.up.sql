CREATE TABLE IF NOT EXISTS usage_limit_violations(
    user_id INTEGER REFERENCES users(id),
    paddle_product_id TEXT NOT NULL,
    requests_limit BIGINT NOT NULL,
    requests_count BIGINT NOT NULL,
    detection_date date NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp,
    PRIMARY KEY (user_id, paddle_product_id, detection_date)
);
