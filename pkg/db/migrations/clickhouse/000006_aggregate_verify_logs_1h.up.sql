CREATE TABLE IF NOT EXISTS privatecaptcha.verify_logs_1h
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    timestamp DateTime,
    count UInt32
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, property_id, timestamp)
TTL timestamp + INTERVAL 1 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.verify_logs_1h_mv TO privatecaptcha.verify_logs_1h AS
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfHour(timestamp) AS timestamp,
    count() AS count
FROM privatecaptcha.verify_logs
GROUP BY user_id, org_id, property_id, timestamp;
