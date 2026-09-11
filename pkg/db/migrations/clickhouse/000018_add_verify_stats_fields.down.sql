ALTER TABLE privatecaptcha.verify_logs_1h_mv MODIFY QUERY
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfHour(timestamp) AS timestamp,
    countIf(status = 0) AS success_count,
    countIf(status != 0) AS failure_count
FROM privatecaptcha.verify_logs
GROUP BY user_id, org_id, property_id, timestamp;

ALTER TABLE privatecaptcha.verify_logs_1d_mv MODIFY QUERY
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfDay(timestamp) AS timestamp,
    sum(success_count) AS success_count,
    sum(failure_count) AS failure_count
FROM privatecaptcha.verify_logs_1h
GROUP BY user_id, org_id, property_id, timestamp;

ALTER TABLE privatecaptcha.verify_logs_1d DROP COLUMN IF EXISTS status_counts;
ALTER TABLE privatecaptcha.verify_logs_1h DROP COLUMN IF EXISTS status_counts;

ALTER TABLE privatecaptcha.verify_logs_1h
    MODIFY ORDER BY (user_id, org_id, property_id, timestamp);
ALTER TABLE privatecaptcha.verify_logs_1h DROP COLUMN IF EXISTS user_agent;
