ALTER TABLE privatecaptcha.verify_logs_1h
    ADD COLUMN IF NOT EXISTS status_counts AggregateFunction(sumMap, Array(UInt8), Array(UInt64));

ALTER TABLE privatecaptcha.verify_logs_1d
    ADD COLUMN IF NOT EXISTS status_counts AggregateFunction(sumMap, Array(UInt8), Array(UInt64));

ALTER TABLE privatecaptcha.verify_logs_1d_mv MODIFY QUERY
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfDay(timestamp) AS timestamp,
    sum(success_count) AS success_count,
    sum(failure_count) AS failure_count,
    sumMapMergeState(status_counts) AS status_counts
FROM privatecaptcha.verify_logs_1h
GROUP BY user_id, org_id, property_id, timestamp;

ALTER TABLE privatecaptcha.verify_logs_1h_mv MODIFY QUERY
SELECT
    user_id,
    org_id,
    property_id,
    toStartOfHour(timestamp) AS timestamp,
    countIf(status = 0) AS success_count,
    countIf(status != 0) AS failure_count,
    sumMapState([status], [toUInt64(1)]) AS status_counts
FROM privatecaptcha.verify_logs
GROUP BY user_id, org_id, property_id, timestamp;
