CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_finalized
(
    expires_on Date,
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    fingerprint UInt64,
    ip_family UInt8,
    ip_prefix UInt64,
    browser LowCardinality(String),
    browser_major UInt16,
    os LowCardinality(String),
    device LowCardinality(String),
    issued_puzzles UInt64,
    attempted_puzzles UInt64,
    successful_puzzles UInt64
)
ENGINE = Null;

CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_ip_daily
(
    expires_on Date,
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    ip_family UInt8,
    ip_prefix UInt64,
    issued_puzzles UInt64,
    attempted_puzzles UInt64,
    successful_puzzles UInt64
)
ENGINE = SummingMergeTree((issued_puzzles, attempted_puzzles, successful_puzzles))
PARTITION BY expires_on
ORDER BY (expires_on, user_id, org_id, property_id, ip_family, ip_prefix)
TTL expires_on + INTERVAL 1 YEAR DELETE;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.puzzle_stats_ip_daily_mv
TO privatecaptcha.puzzle_stats_ip_daily AS
SELECT
    expires_on,
    user_id,
    org_id,
    property_id,
    ip_family,
    ip_prefix,
    sum(issued_puzzles) AS issued_puzzles,
    sum(attempted_puzzles) AS attempted_puzzles,
    sum(successful_puzzles) AS successful_puzzles
FROM privatecaptcha.puzzle_stats_finalized
WHERE ip_family != 0
GROUP BY expires_on, user_id, org_id, property_id, ip_family, ip_prefix;

CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_user_agent_daily
(
    expires_on Date,
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    browser LowCardinality(String),
    browser_major UInt16,
    os LowCardinality(String),
    device LowCardinality(String),
    issued_puzzles UInt64,
    attempted_puzzles UInt64,
    successful_puzzles UInt64
)
ENGINE = SummingMergeTree((issued_puzzles, attempted_puzzles, successful_puzzles))
PARTITION BY expires_on
ORDER BY (expires_on, user_id, org_id, property_id, browser, browser_major, os, device)
TTL expires_on + INTERVAL 1 YEAR DELETE;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.puzzle_stats_user_agent_daily_mv
TO privatecaptcha.puzzle_stats_user_agent_daily AS
SELECT
    expires_on,
    user_id,
    org_id,
    property_id,
    browser,
    browser_major,
    os,
    device,
    sum(issued_puzzles) AS issued_puzzles,
    sum(attempted_puzzles) AS attempted_puzzles,
    sum(successful_puzzles) AS successful_puzzles
FROM privatecaptcha.puzzle_stats_finalized
GROUP BY expires_on, user_id, org_id, property_id, browser, browser_major, os, device;

CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_fingerprint_daily
(
    expires_on Date,
    fingerprint UInt64,
    property_uniq AggregateFunction(uniq, Int32)
)
ENGINE = AggregatingMergeTree
PARTITION BY expires_on
ORDER BY (expires_on, fingerprint)
TTL expires_on + INTERVAL 30 DAY DELETE;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.puzzle_stats_fingerprint_daily_mv
TO privatecaptcha.puzzle_stats_fingerprint_daily AS
SELECT
    expires_on,
    fingerprint,
    uniqState(toInt32(property_id)) AS property_uniq
FROM privatecaptcha.puzzle_stats_finalized
GROUP BY expires_on, fingerprint;

CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_ip_monthly
(
    expires_on Date,
    user_id UInt32,
    org_id UInt32,
    ip_family UInt8,
    ip_prefix UInt64,
    success_count UInt64,
    failure_count UInt64
)
ENGINE = MergeTree
PARTITION BY expires_on
ORDER BY (expires_on, user_id, org_id, ip_family, ip_prefix)
TTL expires_on + INTERVAL 3 YEAR DELETE;

CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_user_agent_monthly
(
    expires_on Date,
    user_id UInt32,
    org_id UInt32,
    browser LowCardinality(String),
    os LowCardinality(String),
    device LowCardinality(String),
    success_count UInt64,
    failure_count UInt64
)
ENGINE = MergeTree
PARTITION BY expires_on
ORDER BY (expires_on, user_id, org_id, browser, os, device)
TTL expires_on + INTERVAL 3 YEAR DELETE;
