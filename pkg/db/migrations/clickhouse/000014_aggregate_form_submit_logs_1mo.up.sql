CREATE TABLE IF NOT EXISTS privatecaptcha.form_submit_logs_1mo
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    form_id UInt32,
    timestamp DateTime,
    success_count UInt64,
    failure_count UInt64
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, property_id, form_id, timestamp)
TTL timestamp + INTERVAL 1 YEAR;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.form_submit_logs_1mo_mv TO privatecaptcha.form_submit_logs_1mo AS
SELECT
    user_id,
    org_id,
    property_id,
    form_id,
    toStartOfMonth(timestamp) AS timestamp,
    sum(success_count) AS success_count,
    sum(failure_count) AS failure_count
FROM privatecaptcha.form_submit_logs_1d
GROUP BY user_id, org_id, property_id, form_id, timestamp;
