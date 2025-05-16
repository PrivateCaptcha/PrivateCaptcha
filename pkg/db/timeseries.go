package db

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	VerifyLogTableName    = "privatecaptcha.verify_logs"
	VerifyLogTable1h      = "privatecaptcha.verify_logs_1h"
	VerifyLogTable1d      = "privatecaptcha.verify_logs_1d"
	AccessLogTableName    = "privatecaptcha.request_logs"
	AccessLogTableName5m  = "privatecaptcha.request_logs_5m"
	AccessLogTableName1h  = "privatecaptcha.request_logs_1h"
	AccessLogTableName1d  = "privatecaptcha.request_logs_1d"
	AccessLogTableName1mo = "privatecaptcha.request_logs_1mo"
)

type TimeSeriesStore struct {
	Clickhouse         *sql.DB
	statsQueryTemplate *template.Template
	maintenanceMode    atomic.Bool
}

func idsToString(ids []int32) string {
	idStrings := make([]string, len(ids))
	for i, id := range ids {
		idStrings[i] = fmt.Sprintf("%d", id)
	}
	idsStr := strings.Join(idStrings, ",")
	return idsStr
}

func NewTimeSeries(clickhouse *sql.DB) *TimeSeriesStore {
	// ClickHouse docs:
	// The join (a search in the right table) is run before filtering in WHERE and before aggregation.
	const statsQuery = `WITH requests AS
(
SELECT
toDateTime({{.TimeFuncRequests}}) AS agg_time,
sum(count) AS count
FROM {{.RequestsTable}} FINAL
WHERE org_id = {org_id:UInt32} AND property_id = {property_id:UInt32} AND timestamp >= {timestamp:DateTime}
GROUP BY agg_time
ORDER BY agg_time
),
verifies AS (
SELECT
toDateTime({{.TimeFuncVerifies}}) AS agg_time,
sum(success_count) AS count
FROM {{.VerifiesTable}} FINAL
WHERE org_id = {org_id:UInt32} AND property_id = {property_id:UInt32} AND timestamp >= {timestamp:DateTime}
GROUP BY agg_time
ORDER BY agg_time
)
SELECT
requests.agg_time AS agg_time,
sum(requests.count) AS requests_count,
sum(verifies.count) AS verifies_count
FROM requests
LEFT OUTER JOIN verifies ON verifies.agg_time = requests.agg_time
GROUP BY agg_time
ORDER BY agg_time WITH FILL FROM toDateTime({{.FillFrom}}) TO now() STEP {{.Interval}}
SETTINGS use_query_cache = true, query_cache_nondeterministic_function_handling = 'save'`

	return &TimeSeriesStore{
		statsQueryTemplate: template.Must(template.New("stats").Parse(statsQuery)),
		Clickhouse:         clickhouse,
	}
}

func (ts *TimeSeriesStore) UpdateConfig(maintenanceMode bool) {
	ts.maintenanceMode.Store(maintenanceMode)
}

func (ts *TimeSeriesStore) Ping(ctx context.Context) error {
	rows, err := ts.Clickhouse.Query("SELECT 1")
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute ping query", common.ErrAttr(err))
		return err
	}

	defer rows.Close()

	if rows.Next() {
		var v int32
		if err := rows.Scan(&v); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from ping query", common.ErrAttr(err))
			return err
		}

		slog.Log(ctx, common.LevelTrace, "Pinged ClickHouse", "result", v)
	}

	return nil
}

func (ts *TimeSeriesStore) isAvailable() bool {
	return !ts.maintenanceMode.Load()
}

func (ts *TimeSeriesStore) WriteAccessLogBatch(ctx context.Context, records []*common.AccessRecord) error {
	if len(records) == 0 {
		slog.WarnContext(ctx, "Attempt to insert empty access log batch")
		return nil
	}

	if !ts.isAvailable() {
		return ErrMaintenance
	}

	scope, err := ts.Clickhouse.Begin()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to begin batch insert", common.ErrAttr(err))
		return err
	}

	batch, err := scope.Prepare(fmt.Sprintf("INSERT INTO %s", AccessLogTableName))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to prepare insert query", common.ErrAttr(err))
		return err
	}

	for i, r := range records {
		_, err = batch.Exec(r.UserID, r.OrgID, r.PropertyID, r.Fingerprint, r.Timestamp.UTC())
		if err != nil {
			slog.ErrorContext(ctx, "Failed to exec insert for record", common.ErrAttr(err), "index", i)
			return err
		}
	}

	err = scope.Commit()
	if err == nil {
		slog.DebugContext(ctx, "Inserted batch of access records", "size", len(records))
	} else {
		slog.ErrorContext(ctx, "Failed to insert access log batch", common.ErrAttr(err))
	}

	return err
}

func (ts *TimeSeriesStore) WriteVerifyLogBatch(ctx context.Context, records []*common.VerifyRecord) error {
	if len(records) == 0 {
		slog.WarnContext(ctx, "Attempt to insert empty verify batch")
		return nil
	}

	if !ts.isAvailable() {
		return ErrMaintenance
	}

	scope, err := ts.Clickhouse.Begin()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to begin batch insert", common.ErrAttr(err))
		return err
	}

	batch, err := scope.Prepare(fmt.Sprintf("INSERT INTO %s", VerifyLogTableName))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to prepare insert query", common.ErrAttr(err))
		return err
	}

	for i, r := range records {
		_, err = batch.Exec(r.UserID, r.OrgID, r.PropertyID, r.PuzzleID, r.Status, r.Timestamp)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to exec insert for record", common.ErrAttr(err), "index", i)
			return err
		}
	}

	err = scope.Commit()
	if err == nil {
		slog.DebugContext(ctx, "Inserted batch of verify records", "size", len(records))
	} else {
		slog.ErrorContext(ctx, "Failed to insert verify log batch", common.ErrAttr(err))
	}

	return err
}

func (ts *TimeSeriesStore) ReadPropertyStats(ctx context.Context, r *common.BackfillRequest, from time.Time) ([]*common.TimeCount, error) {
	if !ts.isAvailable() {
		return nil, ErrMaintenance
	}

	query := `SELECT timestamp, sum(count) as count
FROM %s FINAL
WHERE user_id = {user_id:UInt32} AND org_id = {org_id:UInt32} AND property_id = {property_id:UInt32} AND timestamp >= {timestamp:DateTime}
GROUP BY timestamp
ORDER BY timestamp`
	rows, err := ts.Clickhouse.Query(fmt.Sprintf(query, AccessLogTableName5m),
		clickhouse.Named("user_id", strconv.Itoa(int(r.UserID))),
		clickhouse.Named("org_id", strconv.Itoa(int(r.OrgID))),
		clickhouse.Named("property_id", strconv.Itoa(int(r.PropertyID))),
		clickhouse.Named("timestamp", from.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute property stats query", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*common.TimeCount, 0)

	for rows.Next() {
		bc := &common.TimeCount{}
		if err := rows.Scan(&bc.Timestamp, &bc.Count); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from property stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	slog.DebugContext(ctx, "Read property stats", "count", len(results), "from", from)

	return results, nil
}

func (ts *TimeSeriesStore) ReadAccountStats(ctx context.Context, userID int32, from time.Time) ([]*common.TimeCount, error) {
	if !ts.isAvailable() {
		return nil, ErrMaintenance
	}

	query := `SELECT timestamp, sum(count) as count
FROM %s FINAL
WHERE user_id = {user_id:UInt32} AND timestamp >= {timestamp:DateTime}
GROUP BY timestamp
ORDER BY timestamp`
	rows, err := ts.Clickhouse.Query(fmt.Sprintf(query, AccessLogTableName1mo),
		clickhouse.Named("user_id", strconv.Itoa(int(userID))),
		clickhouse.Named("timestamp", from.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute account stats query", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*common.TimeCount, 0)

	for rows.Next() {
		bc := &common.TimeCount{}
		if err := rows.Scan(&bc.Timestamp, &bc.Count); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from account stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	return results, nil
}

func (ts *TimeSeriesStore) RetrievePropertyStats(ctx context.Context, orgID, propertyID int32, period common.TimePeriod) ([]*common.TimePeriodStat, error) {
	if !ts.isAvailable() {
		return nil, ErrMaintenance
	}

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

	rows, err := ts.Clickhouse.Query(query,
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
		//slog.Log(ctx, common.LevelTrace, "Read property stats row", "timestamp", bc.Timestamp, "verifies", bc.VerifiesCount,
		//	"requests", bc.RequestsCount)
		results = append(results, bc)
	}

	slog.InfoContext(ctx, "Fetched time period stats", "count", len(results), "orgID", orgID, "propID", propertyID,
		"from", timeFrom, "period", period)

	return results, nil
}

func (ts *TimeSeriesStore) lightDelete(ctx context.Context, tables []string, column string, ids string) error {
	for _, table := range tables {
		query := fmt.Sprintf("DELETE FROM %s WHERE %s IN (%s)", table, column, ids)
		if _, err := ts.Clickhouse.Exec(query); err != nil {
			slog.ErrorContext(ctx, "Failed to delete data", "table", table, "column", column, common.ErrAttr(err))
			return err
		}
		slog.DebugContext(ctx, "Deleted data in ClickHouse", "column", column, "table", table)
	}

	return nil
}

func (ts *TimeSeriesStore) DeletePropertiesData(ctx context.Context, propertyIDs []int32) error {
	if len(propertyIDs) == 0 {
		slog.WarnContext(ctx, "Nothing to delete from ClickHouse")
		return nil
	}

	if !ts.isAvailable() {
		return ErrMaintenance
	}

	ids := idsToString(propertyIDs)

	// NOTE: access table for 1 month is not included as it does not have property_id column
	tables := []string{
		AccessLogTableName5m, AccessLogTableName1h, AccessLogTableName1d,
		VerifyLogTable1h, VerifyLogTable1d,
	}

	return ts.lightDelete(ctx, tables, "property_id", ids)
}

func (ts *TimeSeriesStore) DeleteOrganizationsData(ctx context.Context, orgIDs []int32) error {
	if len(orgIDs) == 0 {
		slog.WarnContext(ctx, "Nothing to delete from ClickHouse")
		return nil
	}

	if !ts.isAvailable() {
		return ErrMaintenance
	}

	ids := idsToString(orgIDs)

	tables := []string{
		AccessLogTableName5m, AccessLogTableName1h, AccessLogTableName1d, AccessLogTableName1mo,
		VerifyLogTable1h, VerifyLogTable1d,
	}

	return ts.lightDelete(ctx, tables, "org_id", ids)
}

func (ts *TimeSeriesStore) DeleteUsersData(ctx context.Context, userIDs []int32) error {
	if len(userIDs) == 0 {
		slog.WarnContext(ctx, "Nothing to delete from ClickHouse")
		return nil
	}

	if !ts.isAvailable() {
		return ErrMaintenance
	}

	ids := idsToString(userIDs)

	tables := []string{
		AccessLogTableName5m, AccessLogTableName1h, AccessLogTableName1d, AccessLogTableName1mo,
		VerifyLogTable1h, VerifyLogTable1d,
	}

	return ts.lightDelete(ctx, tables, "user_id", ids)
}
