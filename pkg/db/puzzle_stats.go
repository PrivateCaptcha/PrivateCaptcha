package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

const (
	puzzleStatsOutcomesTable         = "privatecaptcha.puzzle_outcomes_recent"
	puzzleStatsIPDailyTable          = "privatecaptcha.puzzle_stats_ip_daily"
	puzzleStatsUserAgentDailyTable   = "privatecaptcha.puzzle_stats_user_agent_daily"
	puzzleStatsFingerprintDailyTable = "privatecaptcha.puzzle_stats_fingerprint_daily"
)

const insertPuzzleStats = `
INSERT INTO privatecaptcha.puzzle_stats_finalized
WITH outcomes AS
(
    SELECT
        expires_on,
        property_id,
        puzzle_id,
        maxMerge(fingerprint) AS fingerprint,
        maxMerge(ip_family) AS ip_family,
        maxMerge(ip_prefix) AS ip_prefix,
        maxMerge(browser) AS browser,
        maxMerge(browser_major) AS browser_major,
        maxMerge(os) AS os,
        maxMerge(device) AS device,
        tupleElement(sumMapMerge(status_counts), 1) AS outcome_status_codes,
        tupleElement(sumMapMerge(status_counts), 2) AS outcome_status_counts,
        maxMerge(issued) AS issued
    FROM privatecaptcha.puzzle_outcomes_recent
    WHERE expires_on = {source_expires_on:Date}
    GROUP BY expires_on, property_id, puzzle_id
    HAVING issued = 1
)
SELECT
    expires_on,
    fingerprint,
    ip_family,
    ip_prefix,
    browser,
    browser_major,
    os,
    device,
    toUInt64(1) AS issued_puzzles,
    toUInt64(length(outcome_status_codes) > 0) AS attempted_puzzles,
    toUInt64(has(outcome_status_codes, toUInt8(0))) AS successful_puzzles,
    toUInt64(length(outcome_status_codes) > 0 AND NOT has(outcome_status_codes, toUInt8(0))) AS failed_only_puzzles,
    outcome_status_codes AS status_codes,
    outcome_status_counts AS status_counts
FROM outcomes
SETTINGS materialized_views_ignore_errors = 0`

// FinalizeNextPuzzleStats publishes one fully elapsed UTC outcome partition.
func (ts *TimeSeriesDB) FinalizeNextPuzzleStats(ctx context.Context, before time.Time) (bool, error) {
	before = before.UTC().Truncate(24 * time.Hour)
	var expiresOn time.Time
	err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT expires_on
FROM privatecaptcha.puzzle_outcomes_recent
WHERE expires_on < {before:Date}
GROUP BY expires_on
ORDER BY expires_on
LIMIT 1`, clickhouse.Named("before", puzzleStatsDate(before))).Scan(&expiresOn)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := ts.PublishPuzzleStats(ctx, expiresOn); err != nil {
		return false, err
	}
	if err := ts.dropPuzzleStatsPartition(ctx, puzzleStatsOutcomesTable, expiresOn); err != nil {
		return false, err
	}

	return true, nil
}

// PublishPuzzleStats replaces all daily aggregates for one outcome partition.
func (ts *TimeSeriesDB) PublishPuzzleStats(ctx context.Context, expiresOn time.Time) error {
	expiresOn = expiresOn.UTC().Truncate(24 * time.Hour)
	for _, table := range []string{
		puzzleStatsIPDailyTable,
		puzzleStatsUserAgentDailyTable,
		puzzleStatsFingerprintDailyTable,
	} {
		if err := ts.dropPuzzleStatsPartition(ctx, table, expiresOn); err != nil {
			return err
		}
	}

	_, err := ts.Clickhouse.ExecContext(ctx, insertPuzzleStats,
		clickhouse.Named("source_expires_on", puzzleStatsDate(expiresOn)))
	return err
}

func (ts *TimeSeriesDB) dropPuzzleStatsPartition(ctx context.Context, table string, expiresOn time.Time) error {
	_, err := ts.Clickhouse.ExecContext(ctx,
		"ALTER TABLE "+table+" DROP PARTITION {source_expires_on:Date}",
		clickhouse.Named("source_expires_on", puzzleStatsDate(expiresOn)))
	return err
}

func puzzleStatsDate(t time.Time) string {
	return t.UTC().Format(time.DateOnly)
}
