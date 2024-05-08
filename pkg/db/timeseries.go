package db

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"text/template"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	verifyLogTableName   = "privatecaptcha.verify_logs"
	accessLogTableName   = "privatecaptcha.request_logs"
	accessLogTableName5m = "privatecaptcha.request_logs_5m"
)

type TimeSeriesStore struct {
	clickhouse         *sql.DB
	statsQueryTemplate *template.Template
}

func NewTimeSeries(clickhouse *sql.DB) *TimeSeriesStore {
	// ClickHouse docs:
	// The join (a search in the right table) is run before filtering in WHERE and before aggregation.
	const statsQuery = `SELECT 
toDateTime({{.TimeFuncRequests}}) AS agg_time,
sum(requests.count) AS requests_count,
sum(verifies.count) AS verifies_count
FROM {{.RequestsTable}} AS requests
LEFT OUTER JOIN {{.VerifiesTable}} AS verifies ON {{.TimeFuncRequests}} = {{.TimeFuncVerifies}} AND requests.org_id = verifies.org_id AND requests.property_id = verifies.property_id
WHERE requests.org_id = {org_id:UInt32} AND requests.property_id = {property_id:UInt32} AND requests.timestamp >= {timestamp:DateTime}
GROUP BY agg_time
ORDER BY agg_time WITH FILL FROM toDateTime({{.FillFrom}}) TO now() STEP {{.Interval}}`

	return &TimeSeriesStore{
		statsQueryTemplate: template.Must(template.New("stats").Parse(statsQuery)),
		clickhouse:         clickhouse,
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

func (ts *TimeSeriesStore) WriteVerifyLogBatch(ctx context.Context, records []*common.VerifyRecord) error {
	scope, err := ts.clickhouse.Begin()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to begin batch insert", common.ErrAttr(err))
		return err
	}

	batch, err := scope.Prepare(fmt.Sprintf("INSERT INTO %s", verifyLogTableName))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to prepare insert query", common.ErrAttr(err))
		return err
	}

	for i, r := range records {
		_, err = batch.Exec(r.UserID, r.OrgID, r.PropertyID, r.PuzzleID, r.Timestamp)
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

func (ts *TimeSeriesStore) RetrievePropertyStats(ctx context.Context, orgID, propertyID int32, period common.TimePeriod) ([]*common.TimePeriodStat, error) {
	tnow := time.Now().UTC()
	var timeFrom time.Time
	var requestsTable string
	var verificationsTable string
	var timeFunction string
	var interval string

	switch period {
	case common.TimePeriodToday:
		timeFrom = tnow.AddDate(0, 0, -1)
		requestsTable = "request_logs_1h"
		verificationsTable = "verify_logs_1h"
		timeFunction = "toStartOfHour(%s)"
		interval = "INTERVAL 1 HOUR"
	case common.TimePeriodWeek:
		timeFrom = tnow.AddDate(0, 0, -7)
		requestsTable = "request_logs_1d"
		verificationsTable = "verify_logs_1d"
		timeFunction = "toStartOfInterval(%s, INTERVAL 6 HOUR)"
		interval = "INTERVAL 6 HOUR"
	case common.TimePeriodMonth:
		timeFrom = tnow.AddDate(0, -1, 0)
		requestsTable = "request_logs_1d"
		verificationsTable = "verify_logs_1d"
		timeFunction = "toStartOfDay(%s)"
		interval = "INTERVAL 1 DAY"
	case common.TimePeriodYear:
		timeFrom = tnow.AddDate(-1, 0, 0)
		requestsTable = "request_logs_1d"
		verificationsTable = "verify_logs_1d"
		timeFunction = "toStartOfMonth(%s)"
		interval = "INTERVAL 1 MONTH"
	}

	data := struct {
		RequestsTable    string
		VerifiesTable    string
		TimeFuncRequests string
		TimeFuncVerifies string
		Interval         string
		FillFrom         string
	}{
		RequestsTable:    "privatecaptcha." + requestsTable,
		VerifiesTable:    "privatecaptcha." + verificationsTable,
		TimeFuncRequests: fmt.Sprintf(timeFunction, requestsTable+".timestamp"),
		TimeFuncVerifies: fmt.Sprintf(timeFunction, verificationsTable+".timestamp"),
		Interval:         interval,
		FillFrom:         fmt.Sprintf(timeFunction, "{timestamp:DateTime}"),
	}

	buf := &bytes.Buffer{}
	if err := ts.statsQueryTemplate.Execute(buf, data); err != nil {
		slog.ErrorContext(ctx, "Failed to execute stats query template", common.ErrAttr(err))
		return nil, err
	}
	query := buf.String()

	rows, err := ts.clickhouse.Query(query,
		clickhouse.Named("org_id", strconv.Itoa(int(orgID))),
		clickhouse.Named("property_id", strconv.Itoa(int(propertyID))),
		clickhouse.Named("timestamp", timeFrom.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to query property stats", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*common.TimePeriodStat, 0)

	for rows.Next() {
		bc := &common.TimePeriodStat{}
		if err := rows.Scan(&bc.Timestamp, &bc.RequestsCount, &bc.VerifiesCount); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from property stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	slog.InfoContext(ctx, "Fetched time period stats", "count", len(results), "orgID", orgID, "propID", propertyID,
		"from", timeFrom, "period", period)

	return results, nil
}
