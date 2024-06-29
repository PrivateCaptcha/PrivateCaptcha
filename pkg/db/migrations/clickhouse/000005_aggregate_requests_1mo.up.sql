CREATE TABLE IF NOT EXISTS privatecaptcha.request_logs_1mo
(
    user_id UInt32,
    org_id UInt32,
    timestamp DateTime,
    count UInt64
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, timestamp)
TTL timestamp + INTERVAL 1 YEAR;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.request_logs_1mo_mv TO privatecaptcha.request_logs_1mo AS
SELECT
    user_id,
    org_id,
    toStartOfMonth(timestamp) AS timestamp,
    sum(count) AS count
FROM privatecaptcha.request_logs_1d
GROUP BY user_id, org_id, timestamp;
