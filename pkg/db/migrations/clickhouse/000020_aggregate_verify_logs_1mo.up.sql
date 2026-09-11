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

-- ClickHouse migrations are non-transactional, so rebuild the new table if this migration is retried.
DROP VIEW IF EXISTS privatecaptcha.verify_logs_1mo_mv;
TRUNCATE TABLE privatecaptcha.verify_logs_1mo;

-- Backfill before attaching the MV so concurrent historical writes can be lost, but not counted twice.
-- Current-day history is intentionally skipped, and the MV captures only rows inserted after its creation.
INSERT INTO privatecaptcha.verify_logs_1mo (user_id, org_id, timestamp, success_count, failure_count)
SELECT
    user_id,
    org_id,
    toStartOfMonth(timestamp) AS timestamp,
    sum(success_count) AS success_count,
    sum(failure_count) AS failure_count
FROM privatecaptcha.verify_logs_1d
WHERE timestamp < toStartOfDay(now('UTC'))
GROUP BY user_id, org_id, timestamp;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.verify_logs_1mo_mv TO privatecaptcha.verify_logs_1mo AS
SELECT
    user_id,
    org_id,
    toStartOfMonth(timestamp) AS timestamp,
    sum(success_count) AS success_count,
    sum(failure_count) AS failure_count
FROM privatecaptcha.verify_logs_1d
GROUP BY user_id, org_id, timestamp;
