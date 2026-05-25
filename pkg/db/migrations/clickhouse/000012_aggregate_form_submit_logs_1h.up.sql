CREATE TABLE IF NOT EXISTS privatecaptcha.form_submit_logs_1h
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    form_id UInt32,
    timestamp DateTime,
    success_count UInt32,
    failure_count UInt32
)
ENGINE = SummingMergeTree
ORDER BY (user_id, org_id, property_id, form_id, timestamp)
TTL timestamp + INTERVAL 1 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.form_submit_logs_1h_mv TO privatecaptcha.form_submit_logs_1h AS
SELECT
    user_id,
    org_id,
    property_id,
    form_id,
    toStartOfHour(timestamp) AS timestamp,
    countIf(status = 0) AS success_count,
    countIf(status != 0) AS failure_count
FROM privatecaptcha.form_submit_logs
GROUP BY user_id, org_id, property_id, form_id, timestamp;
