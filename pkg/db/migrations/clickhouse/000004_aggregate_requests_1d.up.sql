CREATE TABLE IF NOT EXISTS privatecaptcha.request_logs_1d
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    timestamp DateTime,
    count UInt64
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, property_id, timestamp)
TTL timestamp + INTERVAL 3 YEAR;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.request_logs_1d_mv TO privatecaptcha.request_logs_1d AS
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfDay(timestamp) AS timestamp,
    sum(count) AS count
FROM privatecaptcha.request_logs_1h
GROUP BY user_id, org_id, property_id, timestamp;
