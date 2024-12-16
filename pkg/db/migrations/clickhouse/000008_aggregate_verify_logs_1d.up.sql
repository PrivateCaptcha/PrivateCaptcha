CREATE TABLE IF NOT EXISTS privatecaptcha.verify_logs_1d
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    timestamp DateTime,
    success_count UInt64,
    failure_count UInt64
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, property_id, timestamp)
TTL timestamp + INTERVAL 1 MONTH;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.verify_logs_1d_mv TO privatecaptcha.verify_logs_1d AS
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfDay(timestamp) AS timestamp,
    sum(success_count) AS success_count,
    sum(failure_count) AS failure_count
FROM privatecaptcha.verify_logs_1h
GROUP BY user_id, org_id, property_id, timestamp;
