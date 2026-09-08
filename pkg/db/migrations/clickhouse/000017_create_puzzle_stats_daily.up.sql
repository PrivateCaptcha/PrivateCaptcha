CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_finalized
(
    expires_on Date,
    fingerprint UInt64,
    ip_family UInt8,
    ip_prefix UInt64,
    browser LowCardinality(String),
    browser_major UInt16,
    os LowCardinality(String),
    device LowCardinality(String),
    issued_puzzles UInt64,
    attempted_puzzles UInt64,
    successful_puzzles UInt64,
    failed_only_puzzles UInt64,
    status_codes Array(UInt8),
    status_counts Array(UInt64)
)
ENGINE = Null;

CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_ip_daily
(
    expires_on Date,
    ip_family UInt8,
    ip_prefix UInt64,
    issued_puzzles AggregateFunction(sum, UInt64),
    attempted_puzzles AggregateFunction(sum, UInt64),
    successful_puzzles AggregateFunction(sum, UInt64),
    failed_only_puzzles AggregateFunction(sum, UInt64),
    status_counts AggregateFunction(sumMap, Array(UInt8), Array(UInt64))
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(expires_on)
ORDER BY (expires_on, ip_family, ip_prefix)
TTL expires_on + INTERVAL 3 YEAR DELETE;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.puzzle_stats_ip_daily_mv
TO privatecaptcha.puzzle_stats_ip_daily AS
SELECT
    expires_on,
    ip_family,
    ip_prefix,
    sumState(issued_puzzles) AS issued_puzzles,
    sumState(attempted_puzzles) AS attempted_puzzles,
    sumState(successful_puzzles) AS successful_puzzles,
    sumState(failed_only_puzzles) AS failed_only_puzzles,
    sumMapState(status_codes, status_counts) AS status_counts
FROM privatecaptcha.puzzle_stats_finalized
WHERE ip_family != 0
GROUP BY expires_on, ip_family, ip_prefix;

CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_user_agent_daily
(
    expires_on Date,
    browser LowCardinality(String),
    browser_major UInt16,
    os LowCardinality(String),
    device LowCardinality(String),
    issued_puzzles AggregateFunction(sum, UInt64),
    attempted_puzzles AggregateFunction(sum, UInt64),
    successful_puzzles AggregateFunction(sum, UInt64),
    failed_only_puzzles AggregateFunction(sum, UInt64),
    status_counts AggregateFunction(sumMap, Array(UInt8), Array(UInt64))
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(expires_on)
ORDER BY (expires_on, browser, browser_major, os, device)
TTL expires_on + INTERVAL 1 YEAR DELETE;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.puzzle_stats_user_agent_daily_mv
TO privatecaptcha.puzzle_stats_user_agent_daily AS
SELECT
    expires_on,
    browser,
    browser_major,
    os,
    device,
    sumState(issued_puzzles) AS issued_puzzles,
    sumState(attempted_puzzles) AS attempted_puzzles,
    sumState(successful_puzzles) AS successful_puzzles,
    sumState(failed_only_puzzles) AS failed_only_puzzles,
    sumMapState(status_codes, status_counts) AS status_counts
FROM privatecaptcha.puzzle_stats_finalized
GROUP BY expires_on, browser, browser_major, os, device;

CREATE TABLE IF NOT EXISTS privatecaptcha.puzzle_stats_fingerprint_daily
(
    expires_on Date,
    fingerprint UInt64,
    issued_puzzles AggregateFunction(sum, UInt64),
    attempted_puzzles AggregateFunction(sum, UInt64),
    successful_puzzles AggregateFunction(sum, UInt64),
    failed_only_puzzles AggregateFunction(sum, UInt64),
    status_counts AggregateFunction(sumMap, Array(UInt8), Array(UInt64))
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(expires_on)
ORDER BY (expires_on, fingerprint)
TTL expires_on + INTERVAL 7 DAY DELETE;

CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.puzzle_stats_fingerprint_daily_mv
TO privatecaptcha.puzzle_stats_fingerprint_daily AS
SELECT
    expires_on,
    fingerprint,
    sumState(issued_puzzles) AS issued_puzzles,
    sumState(attempted_puzzles) AS attempted_puzzles,
    sumState(successful_puzzles) AS successful_puzzles,
    sumState(failed_only_puzzles) AS failed_only_puzzles,
    sumMapState(status_codes, status_counts) AS status_counts
FROM privatecaptcha.puzzle_stats_finalized
GROUP BY expires_on, fingerprint;
