CREATE TABLE IF NOT EXISTS privatecaptcha.request_logs_5m
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    timestamp DateTime,
    count UInt32
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, property_id, timestamp)
TTL timestamp + INTERVAL 1 HOUR;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.request_logs_5m_mv TO privatecaptcha.request_logs_5m AS
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfFiveMinute(timestamp) AS timestamp,
    count() AS count
FROM privatecaptcha.request_logs
GROUP BY user_id, org_id, property_id, timestamp;
