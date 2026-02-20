CREATE TABLE IF NOT EXISTS privatecaptcha.rules_logs_1d
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    timestamp DateTime,
    count UInt64
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, property_id, timestamp)
TTL timestamp + INTERVAL 3 MONTH;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.rules_logs_1d_mv TO privatecaptcha.rules_logs_1d AS
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfDay(timestamp) AS timestamp,
    count(*) AS count
FROM privatecaptcha.request_logs
WHERE rule_id > 0
GROUP BY user_id, org_id, property_id, timestamp;
