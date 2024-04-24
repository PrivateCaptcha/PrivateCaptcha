package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	accessLogTableName   = "privatecaptcha.request_logs"
	accessLogTableName5m = "privatecaptcha.request_logs_5m"
)

type TimeSeriesStore struct {
	clickhouse *sql.DB
}

func NewTimeSeries(clickhouse *sql.DB) *TimeSeriesStore {
	return &TimeSeriesStore{
		clickhouse: clickhouse,
	}
}

func (ts *TimeSeriesStore) WriteAccessLogBatch(ctx context.Context, records []*common.AccessRecord) error {
	scope, err := ts.clickhouse.Begin()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to begin batch insert", common.ErrAttr(err))
		return err
	}

	batch, err := scope.Prepare(fmt.Sprintf("INSERT INTO %s", accessLogTableName))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to prepare insert query", common.ErrAttr(err))
		return err
	}

	for i, r := range records {
		_, err = batch.Exec(r.UserID, r.OrgID, r.PropertyID, r.Fingerprint, r.Timestamp)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to exec insert for record", common.ErrAttr(err), "index", i)
			return err
		}
	}

	return scope.Commit()
}

func (ts *TimeSeriesStore) ReadPropertyStats(ctx context.Context, r *common.BackfillRequest, bucketSize time.Duration) ([]*common.TimeCount, error) {
	timeFrom := time.Now().UTC().Add(-time.Duration(5) * bucketSize)
	query := `SELECT timestamp, sum(count) as count
FROM %s FINAL
WHERE user_id = {user_id:UInt32} AND org_id = {org_id:UInt32} AND property_id = {property_id:UInt32} AND timestamp >= {timestamp:DateTime}
GROUP BY timestamp
ORDER BY timestamp`
	rows, err := ts.clickhouse.Query(fmt.Sprintf(query, accessLogTableName5m),
		clickhouse.Named("user_id", strconv.Itoa(int(r.UserID))),
		clickhouse.Named("org_id", strconv.Itoa(int(r.OrgID))),
		clickhouse.Named("property_id", strconv.Itoa(int(r.PropertyID))),
		clickhouse.Named("timestamp", timeFrom.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute backfill stats query", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*common.TimeCount, 0)

	for rows.Next() {
		bc := &common.TimeCount{}
		if err := rows.Scan(&bc.Timestamp, &bc.Count); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from backfill property stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	return results, nil
}

func (ts *TimeSeriesStore) ReadFingerprintStats(ctx context.Context, r *common.BackfillRequest, bucketSize time.Duration) ([]*common.TimeCount, error) {
	timeFrom := time.Now().UTC().Add(-bucketSize)
	query := `SELECT timestamp, count
FROM %s FINAL
WHERE user_id = {user_id:UInt32} AND org_id = {org_id:UInt32} AND property_id = {property_id:UInt32} AND fingerprint = {fingerprint:UInt64} AND timestamp >= {timestamp:DateTime}
ORDER BY timestamp`
	rows, err := ts.clickhouse.Query(fmt.Sprintf(query, accessLogTableName5m),
		clickhouse.Named("user_id", strconv.Itoa(int(r.UserID))),
		clickhouse.Named("org_id", strconv.Itoa(int(r.OrgID))),
		clickhouse.Named("property_id", strconv.Itoa(int(r.PropertyID))),
		clickhouse.Named("fingerprint", strconv.FormatUint(r.Fingerprint, 10)),
		clickhouse.Named("timestamp", timeFrom.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute backfill user stats query", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*common.TimeCount, 0)

	for rows.Next() {
		bc := &common.TimeCount{}
		if err := rows.Scan(&bc.Timestamp, &bc.Count); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from backfill stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	return results, nil
}
