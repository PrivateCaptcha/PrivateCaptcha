CREATE TABLE IF NOT EXISTS privatecaptcha.verify_logs_1mo
(
    user_id UInt32,
    org_id UInt32,
    timestamp DateTime,
    success_count UInt64,
    failure_count UInt64
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, timestamp)
TTL timestamp + INTERVAL 3 YEAR;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.verify_logs_1mo_mv TO privatecaptcha.verify_logs_1mo AS
SELECT
    user_id,
    org_id,
    toStartOfMonth(timestamp) AS timestamp,
    sum(success_count) AS success_count,
    sum(failure_count) AS failure_count
FROM privatecaptcha.verify_logs_1d
GROUP BY user_id, org_id, timestamp;
