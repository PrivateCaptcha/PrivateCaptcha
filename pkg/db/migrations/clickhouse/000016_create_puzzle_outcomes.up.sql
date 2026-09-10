CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_outcomes_recent
(
    expires_on Date,
    property_id UInt32,
    puzzle_id UInt64,
    user_id AggregateFunction(max, UInt32),
    org_id AggregateFunction(max, UInt32),
    fingerprint AggregateFunction(max, UInt64),
    ip_family AggregateFunction(max, UInt8),
    ip_prefix AggregateFunction(max, UInt64),
    browser AggregateFunction(max, String),
    browser_major AggregateFunction(max, UInt16),
    os AggregateFunction(max, String),
    device AggregateFunction(max, String),
    issued AggregateFunction(max, UInt8),
    status_counts AggregateFunction(sumMap, Array(UInt8), Array(UInt64))
)
ENGINE = AggregatingMergeTree
PARTITION BY expires_on
ORDER BY (property_id, puzzle_id);

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.request_logs_puzzle_outcomes_mv
TO privatecaptcha.puzzle_outcomes_recent AS
SELECT
    toDate(expires_at, 'UTC') AS expires_on,
    property_id,
    puzzle_id,
    maxState(user_id) AS user_id,
    maxState(org_id) AS org_id,
    maxState(fingerprint) AS fingerprint,
    maxState(ip_family) AS ip_family,
    maxState(ip_prefix) AS ip_prefix,
    maxState(browser) AS browser,
    maxState(browser_major) AS browser_major,
    maxState(os) AS os,
    maxState(device) AS device,
    maxState(toUInt8(1)) AS issued,
    sumMapState(CAST([], 'Array(UInt8)'), CAST([], 'Array(UInt64)')) AS status_counts
FROM privatecaptcha.request_logs
WHERE puzzle_id != 0
    AND expires_at != toDateTime(0)
    AND expires_on >= toDate(nowInBlock('UTC') - INTERVAL 1 HOUR, 'UTC')
GROUP BY expires_on, property_id, puzzle_id;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.verify_logs_puzzle_outcomes_mv
TO privatecaptcha.puzzle_outcomes_recent AS
SELECT
    toDate(expires_at, 'UTC') AS expires_on,
    property_id,
    puzzle_id,
    maxState(toUInt32(0)) AS user_id,
    maxState(toUInt32(0)) AS org_id,
    maxState(toUInt64(0)) AS fingerprint,
    maxState(toUInt8(0)) AS ip_family,
    maxState(toUInt64(0)) AS ip_prefix,
    maxState('') AS browser,
    maxState(toUInt16(0)) AS browser_major,
    maxState('') AS os,
    maxState('') AS device,
    maxState(toUInt8(0)) AS issued,
    sumMapState([status], [toUInt64(1)]) AS status_counts
FROM privatecaptcha.verify_logs
WHERE puzzle_id != 0
    AND expires_at != toDateTime(0)
    AND expires_on >= toDate(nowInBlock('UTC') - INTERVAL 1 HOUR, 'UTC')
GROUP BY expires_on, property_id, puzzle_id;
