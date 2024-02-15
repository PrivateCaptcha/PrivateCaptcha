CREATE TABLE IF NOT EXISTS privatecaptcha.request_logs_1h
(
    user_id UInt32,
    property_id UInt32,
    timestamp DateTime,
    count UInt32
)
ENGINE = SummingMergeTree
ORDER BY (user_id, property_id, timestamp)
TTL timestamp + INTERVAL 1 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.request_logs_1h_mv TO privatecaptcha.request_logs_1h AS
SELECT
    user_id,
    property_id,
    toStartOfHour(timestamp) AS timestamp,
    sum(count) AS count
FROM privatecaptcha.request_logs_5m
GROUP BY user_id, property_id, timestamp;
