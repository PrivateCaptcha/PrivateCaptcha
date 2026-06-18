package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	VerifyLogTableName        = "privatecaptcha.verify_logs"
	VerifyLogTable1h          = "privatecaptcha.verify_logs_1h"
	VerifyLogTable1d          = "privatecaptcha.verify_logs_1d"
	AccessLogTableName        = "privatecaptcha.request_logs"
	AccessLogTableName5m      = "privatecaptcha.request_logs_5m"
	AccessLogTableName1h      = "privatecaptcha.request_logs_1h"
	AccessLogTableName1d      = "privatecaptcha.request_logs_1d"
	AccessLogTableName1mo     = "privatecaptcha.request_logs_1mo"
	RulesLogsTableName1d      = "privatecaptcha.rules_logs_1d"
	FormSubmitLogTableName    = "privatecaptcha.form_submit_logs"
	FormSubmitLogTableName1h  = "privatecaptcha.form_submit_logs_1h"
	FormSubmitLogTableName1d  = "privatecaptcha.form_submit_logs_1d"
	FormSubmitLogTableName1mo = "privatecaptcha.form_submit_logs_1mo"

	statsRefresh = 15 * time.Minute
)

var (
	ErrUnsupportedPeriod = errors.New("unsupported period")
)

type TimeSeriesDB struct {
	Clickhouse         *sql.DB
	Cache              common.Cache[CacheKey, any]
	statsQueryTemplate *template.Template
	maintenanceMode    atomic.Bool
}

var _ common.TimeSeriesStore = (*TimeSeriesDB)(nil)

func idsToString(ids []int32) string {
	idStrings := make([]string, len(ids))
	for i, id := range ids {
		idStrings[i] = fmt.Sprintf("%d", id)
	}
	idsStr := strings.Join(idStrings, ",")
	return idsStr
}

func NewTimeSeries(clickhouse *sql.DB, cache common.Cache[CacheKey, any]) *TimeSeriesDB {
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
SETTINGS use_query_cache = true, query_cache_nondeterministic_function_handling = 'save', query_cache_tag = 'property_stats_period'`

	return &TimeSeriesDB{
		statsQueryTemplate: template.Must(template.New("stats").Parse(statsQuery)),
		Clickhouse:         clickhouse,
		Cache:              cache,
	}
}

func (ts *TimeSeriesDB) UpdateConfig(maintenanceMode bool) {
	ts.maintenanceMode.Store(maintenanceMode)
}

func (ts *TimeSeriesDB) Ping(ctx context.Context) error {
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

func (ts *TimeSeriesDB) DropCache(ctx context.Context, tag string) error {
	if len(tag) == 0 {
		return ErrInvalidInput
	}

	_, err := ts.Clickhouse.Exec(fmt.Sprintf("SYSTEM DROP QUERY CACHE TAG '%s'", tag))
	return err
}

func (ts *TimeSeriesDB) IsAvailable() bool {
	return !ts.maintenanceMode.Load()
}

func (ts *TimeSeriesDB) WriteAccessLogBatch(ctx context.Context, records []*common.AccessRecord) error {
	if len(records) == 0 {
		slog.WarnContext(ctx, "Attempt to insert empty access log batch")
		return nil
	}

	if !ts.IsAvailable() {
		return ErrMaintenance
	}

	scope, err := ts.Clickhouse.Begin()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to begin batch insert", common.ErrAttr(err))
		return err
	}

	var committed bool
	defer func() {
		if !committed {
			if rerr := scope.Rollback(); rerr != nil {
				slog.ErrorContext(ctx, "Failed to rollback transaction", common.ErrAttr(rerr))
			}
		}
	}()

	batch, err := scope.Prepare(fmt.Sprintf("INSERT INTO %s SETTINGS async_insert = 1, wait_for_async_insert = 1", AccessLogTableName))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to prepare insert query", common.ErrAttr(err))
		return err
	}

	for i, r := range records {
		_, err = batch.Exec(r.UserID, r.OrgID, r.PropertyID, r.Fingerprint, r.Timestamp.UTC(), r.RuleID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to exec insert for record", common.ErrAttr(err), "index", i)
			return err
		}
	}

	err = scope.Commit()
	if err == nil {
		committed = true
		slog.InfoContext(ctx, "Inserted batch of access records", "size", len(records))
	} else {
		slog.ErrorContext(ctx, "Failed to insert access log batch", common.ErrAttr(err))
	}

	return err
}

func (ts *TimeSeriesDB) WriteVerifyLogBatch(ctx context.Context, records []*common.VerifyRecord) error {
	if len(records) == 0 {
		slog.WarnContext(ctx, "Attempt to insert empty verify batch")
		return nil
	}

	if !ts.IsAvailable() {
		return ErrMaintenance
	}

	scope, err := ts.Clickhouse.Begin()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to begin batch insert", common.ErrAttr(err))
		return err
	}
	var committed bool
	defer func() {
		if !committed {
			if rerr := scope.Rollback(); rerr != nil {
				slog.ErrorContext(ctx, "Failed to rollback transaction", common.ErrAttr(rerr))
			}
		}
	}()

	batch, err := scope.Prepare(fmt.Sprintf("INSERT INTO %s SETTINGS async_insert = 1, wait_for_async_insert = 1", VerifyLogTableName))
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
		committed = true
		slog.InfoContext(ctx, "Inserted batch of verify records", "size", len(records))
	} else {
		slog.ErrorContext(ctx, "Failed to insert verify log batch", common.ErrAttr(err))
	}

	return err
}

func (ts *TimeSeriesDB) WriteFormSubmitBatch(ctx context.Context, records []*common.FormSubmitRecord) error {
	if len(records) == 0 {
		slog.WarnContext(ctx, "Attempt to insert empty form submit batch")
		return nil
	}

	if !ts.IsAvailable() {
		return ErrMaintenance
	}

	scope, err := ts.Clickhouse.Begin()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to begin batch insert", common.ErrAttr(err))
		return err
	}
	var committed bool
	defer func() {
		if !committed {
			if rerr := scope.Rollback(); rerr != nil {
				slog.ErrorContext(ctx, "Failed to rollback transaction", common.ErrAttr(rerr))
			}
		}
	}()

	batch, err := scope.Prepare(fmt.Sprintf("INSERT INTO %s SETTINGS async_insert = 1, wait_for_async_insert = 1", FormSubmitLogTableName))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to prepare insert query", common.ErrAttr(err))
		return err
	}

	for i, r := range records {
		_, err = batch.Exec(r.UserID, r.OrgID, r.FormID, r.Status, r.Timestamp.UTC())
		if err != nil {
			slog.ErrorContext(ctx, "Failed to exec insert for record", common.ErrAttr(err), "index", i)
			return err
		}
	}

	err = scope.Commit()
	if err == nil {
		committed = true
		slog.InfoContext(ctx, "Inserted batch of form submit records", "size", len(records))
	} else {
		slog.ErrorContext(ctx, "Failed to insert form submit log batch", common.ErrAttr(err))
	}

	return err
}

func (ts *TimeSeriesDB) RetrievePropertyStatsSince(ctx context.Context, r *common.BackfillRequest, from time.Time) ([]*common.TimeCount, error) {
	if !ts.IsAvailable() {
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

func (ts *TimeSeriesDB) RetrieveAccountStats(ctx context.Context, userID int32, from time.Time) ([]*common.OrgTimeCount, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	fromStr := from.Format(time.DateTime)

	cacheKey := userAccountStatsCacheKey(userID, fromStr)
	if stats, err := FetchCachedArray[common.OrgTimeCount](ctx, ts.Cache, cacheKey); (err == nil) && (len(stats) > 0) {
		slog.DebugContext(ctx, "User account stats were cached", "userID", userID, "key", cacheKey, "count", len(stats))
		return stats, nil
	}

	query := `SELECT org_id, ts, max(count) as count
FROM (
    SELECT org_id, timestamp as ts, sum(count) as count
    FROM %s
    WHERE user_id = {user_id:UInt32} AND timestamp >= {timestamp:DateTime}
    GROUP BY org_id, timestamp
    UNION ALL
    SELECT org_id, toStartOfMonth(timestamp) as ts, sum(success_count + failure_count) as count
    FROM %s
    WHERE user_id = {user_id:UInt32} AND timestamp >= {timestamp:DateTime}
    GROUP BY org_id, toStartOfMonth(timestamp)
)
GROUP BY org_id, ts
ORDER BY org_id, ts`
	// Use max of request and verify counts per (org_id, month)
	rows, err := ts.Clickhouse.Query(fmt.Sprintf(query, AccessLogTableName1mo, VerifyLogTable1d),
		clickhouse.Named("user_id", strconv.Itoa(int(userID))),
		clickhouse.Named("timestamp", fromStr))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute account stats query", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*common.OrgTimeCount, 0)

	for rows.Next() {
		bc := &common.OrgTimeCount{}
		if err := rows.Scan(&bc.OrgID, &bc.Timestamp, &bc.Count); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from account stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	_ = ts.Cache.Set(ctx, cacheKey, results)

	return results, nil
}

func (ts *TimeSeriesDB) retrieveReportStats(ctx context.Context, userID int32, from, mid, to time.Time, accessTable, verifyTable string) (*common.UserReportStats, error) {
	fromStr := from.Format(time.DateTime)
	midStr := mid.Format(time.DateTime)
	toStr := to.Format(time.DateTime)
	userIDStr := strconv.Itoa(int(userID))

	query := `SELECT property_id, org_id,
    sumIf(req_count, timestamp >= {mid_ts:DateTime}) as current_requests,
    sumIf(req_count, timestamp < {mid_ts:DateTime}) as prev_requests,
    sumIf(ver_count, timestamp >= {mid_ts:DateTime}) as current_verifies,
    sumIf(ver_count, timestamp < {mid_ts:DateTime}) as prev_verifies
FROM (
    SELECT property_id, org_id, sum(count) as req_count, 0 as ver_count, toStartOfDay(timestamp) as timestamp
    FROM %s FINAL
    WHERE user_id = {user_id:UInt32} AND timestamp >= {from_ts:DateTime} AND timestamp < {to_ts:DateTime}
    GROUP BY property_id, org_id, timestamp
    UNION ALL
    SELECT property_id, org_id, 0 as req_count, sum(success_count + failure_count) as ver_count, toStartOfDay(timestamp) as timestamp
    FROM %s FINAL
    WHERE user_id = {user_id:UInt32} AND timestamp >= {from_ts:DateTime} AND timestamp < {to_ts:DateTime}
    GROUP BY property_id, org_id, timestamp
)
GROUP BY property_id, org_id
ORDER BY current_requests DESC`

	rows, err := ts.Clickhouse.Query(fmt.Sprintf(query, accessTable, verifyTable),
		clickhouse.Named("user_id", userIDStr),
		clickhouse.Named("from_ts", fromStr),
		clickhouse.Named("mid_ts", midStr),
		clickhouse.Named("to_ts", toStr))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute report stats query", "userID", userID, "accessTable", accessTable, common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	stats := &common.UserReportStats{}

	for rows.Next() {
		ps := &common.UserReportPropertyStat{}
		if err := rows.Scan(&ps.PropertyID, &ps.OrgID, &ps.CurrentRequests, &ps.PrevRequests, &ps.CurrentVerifies, &ps.PrevVerifies); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from report stats query", common.ErrAttr(err))
			return nil, err
		}
		stats.Properties = append(stats.Properties, ps)
		stats.TotalCurrentRequests += ps.CurrentRequests
		stats.TotalPrevRequests += ps.PrevRequests
		stats.TotalCurrentVerifies += ps.CurrentVerifies
		stats.TotalPrevVerifies += ps.PrevVerifies
	}

	return stats, nil
}

func (ts *TimeSeriesDB) RetrieveWeeklyPropertiesReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*common.UserReportStats, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	stats, err := ts.retrieveReportStats(ctx, userID, from, mid, to, AccessLogTableName1d, VerifyLogTable1d)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Fetched weekly report stats", "userID", userID, "properties", len(stats.Properties),
		"currentReq", stats.TotalCurrentRequests, "prevReq", stats.TotalPrevRequests,
		"currentVer", stats.TotalCurrentVerifies, "prevVer", stats.TotalPrevVerifies)

	return stats, nil
}

func (ts *TimeSeriesDB) RetrieveMonthlyPropertiesReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*common.UserReportStats, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	stats, err := ts.retrieveReportStats(ctx, userID, from, mid, to, AccessLogTableName1d, VerifyLogTable1d)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Fetched monthly report stats", "userID", userID, "properties", len(stats.Properties),
		"currentReq", stats.TotalCurrentRequests, "prevReq", stats.TotalPrevRequests,
		"currentVer", stats.TotalCurrentVerifies, "prevVer", stats.TotalPrevVerifies)

	return stats, nil
}

func (ts *TimeSeriesDB) retrieveFormsReportStats(ctx context.Context, userID int32, from, mid, to time.Time, table string) (*common.UserFormsReportStats, error) {
	fromStr := from.Format(time.DateTime)
	midStr := mid.Format(time.DateTime)
	toStr := to.Format(time.DateTime)
	userIDStr := strconv.Itoa(int(userID))

	query := `SELECT form_id, org_id,
    sumIf(success_count + failure_count, timestamp >= {mid_ts:DateTime}) as current_submissions,
    sumIf(success_count + failure_count, timestamp < {mid_ts:DateTime}) as prev_submissions,
    sumIf(failure_count, timestamp >= {mid_ts:DateTime}) as current_errors,
    sumIf(failure_count, timestamp < {mid_ts:DateTime}) as prev_errors
FROM %s FINAL
WHERE user_id = {user_id:UInt32} AND timestamp >= {from_ts:DateTime} AND timestamp < {to_ts:DateTime}
GROUP BY form_id, org_id
ORDER BY current_submissions DESC`

	rows, err := ts.Clickhouse.Query(fmt.Sprintf(query, table),
		clickhouse.Named("user_id", userIDStr),
		clickhouse.Named("from_ts", fromStr),
		clickhouse.Named("mid_ts", midStr),
		clickhouse.Named("to_ts", toStr))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute forms report stats query", "userID", userID, "table", table, common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	stats := &common.UserFormsReportStats{}

	for rows.Next() {
		fs := &common.UserReportFormStat{}
		if err := rows.Scan(&fs.FormID, &fs.OrgID, &fs.CurrentSubmissions, &fs.PrevSubmissions, &fs.CurrentErrors, &fs.PrevErrors); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from forms report stats query", common.ErrAttr(err))
			return nil, err
		}
		stats.Forms = append(stats.Forms, fs)
		stats.TotalCurrentSubmissions += fs.CurrentSubmissions
		stats.TotalPrevSubmissions += fs.PrevSubmissions
		stats.TotalCurrentErrors += fs.CurrentErrors
		stats.TotalPrevErrors += fs.PrevErrors
	}

	return stats, nil
}

func (ts *TimeSeriesDB) RetrieveWeeklyFormsReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*common.UserFormsReportStats, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	stats, err := ts.retrieveFormsReportStats(ctx, userID, from, mid, to, FormSubmitLogTableName1d)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Fetched weekly forms report stats", "userID", userID, "forms", len(stats.Forms),
		"currentSubmissions", stats.TotalCurrentSubmissions, "prevSubmissions", stats.TotalPrevSubmissions,
		"currentErrors", stats.TotalCurrentErrors, "prevErrors", stats.TotalPrevErrors)

	return stats, nil
}

func (ts *TimeSeriesDB) RetrieveMonthlyFormsReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*common.UserFormsReportStats, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	stats, err := ts.retrieveFormsReportStats(ctx, userID, from, mid, to, FormSubmitLogTableName1d)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Fetched monthly forms report stats", "userID", userID, "forms", len(stats.Forms),
		"currentSubmissions", stats.TotalCurrentSubmissions, "prevSubmissions", stats.TotalPrevSubmissions,
		"currentErrors", stats.TotalCurrentErrors, "prevErrors", stats.TotalPrevErrors)

	return stats, nil
}

func (ts *TimeSeriesDB) RetrievePropertyStatsByPeriod(ctx context.Context, orgID, propertyID int32, period common.TimePeriod) ([]*common.TimePeriodStat, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	tnow := time.Now().UTC()
	var timeFrom time.Time
	var requestsTable string
	var verificationsTable string
	var timeFunction string
	var interval string
	var cacheKey *CacheKey

	switch period {
	case common.TimePeriodToday:
		timeFrom = tnow.AddDate(0, 0, -1).Truncate(1 * time.Hour)
		requestsTable = "request_logs_1h"
		verificationsTable = "verify_logs_1h"
		timeFunction = "toStartOfHour(%s)"
		interval = "INTERVAL 1 HOUR"
		// in server we only cache the "today" as this is the default chart in the UI
		cacheKey = new(CacheKey)
		*cacheKey = propertyStatsCacheKey(propertyID, timeFrom.Format(time.DateTime))
	case common.TimePeriodWeek:
		timeFrom = tnow.AddDate(0, 0, -7).Truncate(6 * time.Hour)
		requestsTable = "request_logs_1d"
		verificationsTable = "verify_logs_1d"
		timeFunction = "toStartOfInterval(%s, INTERVAL 6 HOUR)"
		interval = "INTERVAL 6 HOUR"
	case common.TimePeriodMonth:
		timeFrom = tnow.AddDate(0, -1, 0).Truncate(24 * time.Hour)
		requestsTable = "request_logs_1d"
		verificationsTable = "verify_logs_1d"
		timeFunction = "toStartOfDay(%s)"
		interval = "INTERVAL 1 DAY"
	case common.TimePeriodYear:
		timeFrom = tnow.AddDate(-1, 0, 0).Truncate(24 * time.Hour)
		requestsTable = "request_logs_1d"
		verificationsTable = "verify_logs_1d"
		timeFunction = "toStartOfMonth(%s)"
		interval = "INTERVAL 1 MONTH"
	}

	if cacheKey != nil {
		if stats, err := FetchCachedArray[common.TimePeriodStat](ctx, ts.Cache, *cacheKey); (err == nil) && (len(stats) > 0) {
			slog.DebugContext(ctx, "Property stats were cached", "orgID", orgID, "propertyID", propertyID, "key", *cacheKey, "count", len(stats))
			return stats, nil
		}
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

	if cacheKey != nil {
		const propertyStatsCacheTTL = 5 * time.Minute
		// we have 5 min buffers for updates and we do NOT delete this cache item
		_ = ts.Cache.SetEx(ctx, *cacheKey, results, propertyStatsCacheTTL, statsRefresh)
	}

	return results, nil
}

func (ts *TimeSeriesDB) RetrieveFormStatsByPeriod(ctx context.Context, orgID, formID int32, period common.TimePeriod) ([]*common.FormSubmitStat, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	tnow := time.Now().UTC()
	var timeFrom time.Time
	var table string
	var timeFunction string
	var interval string
	var cacheKey *CacheKey

	switch period {
	case common.TimePeriodToday:
		timeFrom = tnow.AddDate(0, 0, -1).Truncate(1 * time.Hour)
		table = FormSubmitLogTableName1h
		timeFunction = "toStartOfHour(%s)"
		interval = "INTERVAL 1 HOUR"
		// in server we only cache the "today" as this is the default chart in the UI
		cacheKey = new(CacheKey)
		*cacheKey = FormStatsCacheKey(formID, timeFrom.Format(time.DateTime))
	case common.TimePeriodWeek:
		timeFrom = tnow.AddDate(0, 0, -7).Truncate(24 * time.Hour)
		table = FormSubmitLogTableName1d
		timeFunction = "toStartOfDay(%s)"
		interval = "INTERVAL 1 DAY"
	case common.TimePeriodMonth:
		timeFrom = tnow.AddDate(0, -1, 0).Truncate(24 * time.Hour)
		table = FormSubmitLogTableName1d
		timeFunction = "toStartOfDay(%s)"
		interval = "INTERVAL 1 DAY"
	case common.TimePeriodYear:
		timeFrom = tnow.AddDate(-1, 0, 0).Truncate(24 * time.Hour)
		table = FormSubmitLogTableName1d
		timeFunction = "toStartOfMonth(%s)"
		interval = "INTERVAL 1 MONTH"
	default:
		return nil, ErrUnsupportedPeriod
	}

	if cacheKey != nil {
		if stats, err := FetchCachedArray[common.FormSubmitStat](ctx, ts.Cache, *cacheKey); (err == nil) && (len(stats) > 0) {
			slog.DebugContext(ctx, "Form stats were cached", "orgID", orgID, "formID", formID, "key", *cacheKey, "count", len(stats))
			return stats, nil
		}
	}

	query := fmt.Sprintf(`SELECT
		toDateTime(%s) AS agg_time,
		sum(success_count) AS success_count,
		sum(failure_count) AS failure_count
	FROM %s FINAL
	WHERE org_id = {org_id:UInt32} AND form_id = {form_id:UInt32} AND timestamp >= {timestamp:DateTime}
	GROUP BY agg_time
	ORDER BY agg_time WITH FILL FROM toDateTime(%s) TO now() STEP %s
	SETTINGS use_query_cache = true, query_cache_nondeterministic_function_handling = 'save', query_cache_tag = 'form_stats_period';`,
		fmt.Sprintf(timeFunction, "timestamp"),
		table,
		fmt.Sprintf(timeFunction, "{timestamp:DateTime}"),
		interval)

	rows, err := ts.Clickhouse.Query(query,
		clickhouse.Named("org_id", strconv.Itoa(int(orgID))),
		clickhouse.Named("form_id", strconv.Itoa(int(formID))),
		clickhouse.Named("timestamp", timeFrom.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to query form submit stats", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*common.FormSubmitStat, 0)
	for rows.Next() {
		stat := &common.FormSubmitStat{}
		if err := rows.Scan(&stat.Timestamp, &stat.SuccessCount, &stat.FailureCount); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from form submit stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, stat)
	}

	slog.InfoContext(ctx, "Fetched form submit stats", "count", len(results), "orgID", orgID, "formID", formID,
		"from", timeFrom, "period", period)

	if cacheKey != nil {
		const formStatsCacheTTL = 5 * time.Minute
		// we have 5 min buffers for updates and we do NOT delete this cache item
		_ = ts.Cache.SetEx(ctx, *cacheKey, results, formStatsCacheTTL, statsRefresh)
	}

	return results, nil
}

func (ts *TimeSeriesDB) RetrieveFailingForms(ctx context.Context, threshold, limit int) ([]*common.FailingFormCandidate, error) {
	if (threshold <= 0) || (limit <= 0) {
		return nil, ErrInvalidInput
	}

	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	query := fmt.Sprintf(`WITH hourly AS (
    SELECT
        form_id,
        timestamp,
        sum(success_count) AS hr_success,
        sum(failure_count) AS hr_failure
    FROM %s FINAL
    WHERE timestamp >= {timestamp:DateTime}
    GROUP BY form_id, timestamp
    HAVING (hr_success + hr_failure) > 0
), ranked AS (
    SELECT
        form_id,
        timestamp,
        hr_success,
        hr_failure,
        row_number() OVER (PARTITION BY form_id ORDER BY timestamp DESC) AS rn
    FROM hourly
), last_records AS (
    SELECT
        form_id,
        count() AS record_count,
        sum(hr_success) AS total_success,
        sum(hr_failure) AS total_failure,
        countIf(hr_success = 0 AND hr_failure > 0) AS failed_record_count
    FROM ranked
    WHERE rn <= {threshold:UInt32}
    GROUP BY form_id
)
SELECT
    form_id,
    total_failure AS failure_count -- Aliased back so rows.Scan works
FROM last_records
WHERE record_count = {threshold:UInt32}
    AND total_success = 0
    AND failed_record_count = {threshold:UInt32}
ORDER BY failure_count DESC, form_id ASC
LIMIT {limit_val:UInt32};`, FormSubmitLogTableName1h)

	rows, err := ts.Clickhouse.Query(query,
		clickhouse.Named("timestamp", time.Now().UTC().Add(-24*time.Hour).Format(time.DateTime)),
		clickhouse.Named("threshold", strconv.Itoa(threshold)),
		clickhouse.Named("limit_val", strconv.Itoa(limit)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to query failing forms", common.ErrAttr(err))
		return nil, err
	}
	defer rows.Close()

	results := make([]*common.FailingFormCandidate, 0)
	for rows.Next() {
		candidate := &common.FailingFormCandidate{}
		if err := rows.Scan(&candidate.FormID, &candidate.FailureCount); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from failing forms query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, candidate)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Fetched failing forms", "count", len(results), "threshold", threshold, "limit", limit)

	return results, nil
}

func (ts *TimeSeriesDB) RetrievePropertyRuleStatsByPeriod(ctx context.Context, userID, orgID, propertyID int32, period common.TimePeriod) ([]*common.TimeCount, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	// Only support week and month periods for rule stats
	if period != common.TimePeriodWeek && period != common.TimePeriodMonth {
		return nil, ErrUnsupportedPeriod
	}

	tnow := time.Now().UTC()
	var timeFrom time.Time
	var timeFunction string
	var interval string
	var cacheKey *CacheKey

	switch period {
	case common.TimePeriodWeek:
		timeFrom = tnow.AddDate(0, 0, -7).Truncate(24 * time.Hour)
		timeFunction = "toStartOfDay(%s)"
		interval = "INTERVAL 1 DAY"
		cacheKey = new(CacheKey)
		*cacheKey = propertyRuleStatsCacheKey(propertyID, timeFrom.Format(time.DateTime))
	case common.TimePeriodMonth:
		timeFrom = tnow.AddDate(0, -1, 0).Truncate(24 * time.Hour)
		timeFunction = "toStartOfDay(%s)"
		interval = "INTERVAL 1 DAY"
		cacheKey = new(CacheKey)
		*cacheKey = propertyRuleStatsCacheKey(propertyID, timeFrom.Format(time.DateTime))
	}

	if cacheKey != nil {
		if stats, err := FetchCachedArray[common.TimeCount](ctx, ts.Cache, *cacheKey); (err == nil) && (len(stats) > 0) {
			slog.DebugContext(ctx, "Property rule stats were cached", "orgID", orgID, "propertyID", propertyID, "key", *cacheKey, "count", len(stats))
			return stats, nil
		}
	}

	query := fmt.Sprintf(`SELECT
		toDateTime(%s) AS agg_time,
		sum(count) AS count
	FROM %s FINAL
	WHERE user_id = {user_id:UInt32} AND org_id = {org_id:UInt32} AND property_id = {property_id:UInt32} AND timestamp >= {timestamp:DateTime}
	GROUP BY agg_time
	ORDER BY agg_time WITH FILL FROM toDateTime(%s) TO now() STEP %s
	SETTINGS use_query_cache = true, query_cache_nondeterministic_function_handling = 'save'`,
		fmt.Sprintf(timeFunction, "timestamp"),
		RulesLogsTableName1d,
		fmt.Sprintf(timeFunction, "{timestamp:DateTime}"),
		interval)

	rows, err := ts.Clickhouse.Query(query,
		clickhouse.Named("user_id", strconv.Itoa(int(userID))),
		clickhouse.Named("org_id", strconv.Itoa(int(orgID))),
		clickhouse.Named("property_id", strconv.Itoa(int(propertyID))),
		clickhouse.Named("timestamp", timeFrom.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to query property rule stats", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*common.TimeCount, 0)

	for rows.Next() {
		bc := &common.TimeCount{}
		if err := rows.Scan(&bc.Timestamp, &bc.Count); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from property rule stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	slog.InfoContext(ctx, "Fetched rule stats", "count", len(results), "orgID", orgID, "propID", propertyID,
		"from", timeFrom, "period", period)

	if cacheKey != nil {
		const propertyRuleStatsCacheTTL = 5 * time.Minute
		_ = ts.Cache.SetEx(ctx, *cacheKey, results, propertyRuleStatsCacheTTL, statsRefresh)
	}

	return results, nil
}

func (ts *TimeSeriesDB) RetrieveRecentTopProperties(ctx context.Context, limit int) (map[int32]uint, error) {
	if !ts.IsAvailable() {
		return nil, ErrMaintenance
	}

	// NOTE: we don't use FINAL here because this is just an approximation anyways
	// that is used to warmup cache so we don't need the most precise results
	query := `SELECT property_id
FROM %s
WHERE timestamp >= now() - INTERVAL 1 DAY
GROUP BY property_id
ORDER BY sum(success_count + failure_count) DESC
LIMIT %d`
	rows, err := ts.Clickhouse.Query(fmt.Sprintf(query, VerifyLogTable1d, limit))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute top usage query", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	properties := make(map[int32]uint, limit)

	for rows.Next() {
		var propertyID int32
		if err := rows.Scan(&propertyID); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from top usage query", common.ErrAttr(err))
			return nil, err
		}
		properties[propertyID]++
	}

	return properties, nil
}

func (ts *TimeSeriesDB) lightDelete(ctx context.Context, tables []string, column string, ids string) error {
	for _, table := range tables {
		query := fmt.Sprintf("DELETE FROM %s WHERE %s IN (%s)", table, column, ids)
		if _, err := ts.Clickhouse.Exec(query); err != nil {
			slog.ErrorContext(ctx, "Failed to delete data", "table", table, "column", column, common.ErrAttr(err))
			return err
		}
		slog.InfoContext(ctx, "Deleted data in ClickHouse", "column", column, "table", table)
	}

	return nil
}

func (ts *TimeSeriesDB) DeleteFormsData(ctx context.Context, formIDs []int32) error {
	if len(formIDs) == 0 {
		slog.WarnContext(ctx, "Nothing to delete from ClickHouse")
		return nil
	}

	if !ts.IsAvailable() {
		return ErrMaintenance
	}

	ids := idsToString(formIDs)
	tables := []string{FormSubmitLogTableName1h, FormSubmitLogTableName1d, FormSubmitLogTableName1mo}

	tnow := time.Now().UTC()
	timeFrom := tnow.AddDate(0, 0, -1).Truncate(1 * time.Hour)
	strCacheKey := timeFrom.Format(time.DateTime)
	for _, formID := range formIDs {
		cacheKey := FormStatsCacheKey(formID, strCacheKey)
		_ = ts.Cache.Delete(ctx, cacheKey)
	}

	return ts.lightDelete(ctx, tables, "form_id", ids)
}

func (ts *TimeSeriesDB) DeletePropertiesData(ctx context.Context, propertyIDs []int32) error {
	if len(propertyIDs) == 0 {
		slog.WarnContext(ctx, "Nothing to delete from ClickHouse")
		return nil
	}

	if !ts.IsAvailable() {
		return ErrMaintenance
	}

	ids := idsToString(propertyIDs)

	// NOTE: access table for 1 month is not included as it does not have property_id column
	tables := []string{
		AccessLogTableName5m, AccessLogTableName1h, AccessLogTableName1d,
		VerifyLogTable1h, VerifyLogTable1d, RulesLogsTableName1d,
	}

	tnow := time.Now().UTC()
	timeFrom := tnow.AddDate(0, 0, -1).Truncate(1 * time.Hour)
	strCacheKey := timeFrom.Format(time.DateTime)
	weekCacheKey := tnow.AddDate(0, 0, -7).Truncate(24 * time.Hour).Format(time.DateTime)
	monthCacheKey := tnow.AddDate(0, -1, 0).Truncate(24 * time.Hour).Format(time.DateTime)
	for _, propertyID := range propertyIDs {
		cacheKey := propertyStatsCacheKey(propertyID, strCacheKey)
		_ = ts.Cache.Delete(ctx, cacheKey)
		_ = ts.Cache.Delete(ctx, propertyRuleStatsCacheKey(propertyID, weekCacheKey))
		_ = ts.Cache.Delete(ctx, propertyRuleStatsCacheKey(propertyID, monthCacheKey))
	}

	return ts.lightDelete(ctx, tables, "property_id", ids)
}

func (ts *TimeSeriesDB) DeleteOrganizationsData(ctx context.Context, orgIDs []int32) error {
	if len(orgIDs) == 0 {
		slog.WarnContext(ctx, "Nothing to delete from ClickHouse")
		return nil
	}

	if !ts.IsAvailable() {
		return ErrMaintenance
	}

	ids := idsToString(orgIDs)

	tables := []string{
		AccessLogTableName5m, AccessLogTableName1h, AccessLogTableName1d, AccessLogTableName1mo,
		VerifyLogTable1h, VerifyLogTable1d, RulesLogsTableName1d,
		FormSubmitLogTableName1h, FormSubmitLogTableName1d, FormSubmitLogTableName1mo,
	}

	return ts.lightDelete(ctx, tables, "org_id", ids)
}

func (ts *TimeSeriesDB) DeleteUsersData(ctx context.Context, userIDs []int32) error {
	if len(userIDs) == 0 {
		slog.WarnContext(ctx, "Nothing to delete from ClickHouse")
		return nil
	}

	if !ts.IsAvailable() {
		return ErrMaintenance
	}

	ids := idsToString(userIDs)

	tables := []string{
		AccessLogTableName5m, AccessLogTableName1h, AccessLogTableName1d, AccessLogTableName1mo,
		VerifyLogTable1h, VerifyLogTable1d, RulesLogsTableName1d,
		FormSubmitLogTableName1h, FormSubmitLogTableName1d, FormSubmitLogTableName1mo,
	}

	return ts.lightDelete(ctx, tables, "user_id", ids)
}

type MemoryTimeSeries struct {
	mu             sync.RWMutex
	accessLogs     []*common.AccessRecord
	verifyLogs     []*common.VerifyRecord
	formSubmitLogs []*common.FormSubmitRecord
}

var _ common.TimeSeriesStore = (*MemoryTimeSeries)(nil)

func NewMemoryTimeSeries() *MemoryTimeSeries {
	return &MemoryTimeSeries{
		accessLogs:     make([]*common.AccessRecord, 0),
		verifyLogs:     make([]*common.VerifyRecord, 0),
		formSubmitLogs: make([]*common.FormSubmitRecord, 0),
	}
}

func (m *MemoryTimeSeries) Ping(ctx context.Context) error {
	return nil
}

func (m *MemoryTimeSeries) DropCache(ctx context.Context, tag string) error {
	return nil
}

func (m *MemoryTimeSeries) WriteAccessLogBatch(ctx context.Context, records []*common.AccessRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accessLogs = append(m.accessLogs, records...)
	return nil
}

func (m *MemoryTimeSeries) WriteVerifyLogBatch(ctx context.Context, records []*common.VerifyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verifyLogs = append(m.verifyLogs, records...)
	return nil
}

func (m *MemoryTimeSeries) WriteFormSubmitBatch(ctx context.Context, records []*common.FormSubmitRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.formSubmitLogs = append(m.formSubmitLogs, records...)
	return nil
}

func (m *MemoryTimeSeries) RetrievePropertyStatsSince(ctx context.Context, r *common.BackfillRequest, from time.Time) ([]*common.TimeCount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[time.Time]uint32)
	for _, log := range m.accessLogs {
		if log.OrgID == r.OrgID && log.UserID == r.UserID && log.PropertyID == r.PropertyID && !log.Timestamp.Before(from) {
			// Real DB uses request_logs_5m which is aggregated by 5 minutes
			ts := log.Timestamp.Truncate(5 * time.Minute)
			counts[ts]++
		}
	}

	return mapToTimeCount(counts), nil
}

func (m *MemoryTimeSeries) RetrieveAccountStats(ctx context.Context, userID int32, from time.Time) ([]*common.OrgTimeCount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	reqCounts := make(map[int32]map[time.Time]uint32)
	for _, log := range m.accessLogs {
		if log.UserID == userID && !log.Timestamp.Before(from) {
			// Real DB uses request_logs_1mo which is aggregated by month
			y, month, _ := log.Timestamp.Date()
			ts := time.Date(y, month, 1, 0, 0, 0, 0, log.Timestamp.Location())
			if _, ok := reqCounts[log.OrgID]; !ok {
				reqCounts[log.OrgID] = make(map[time.Time]uint32)
			}
			reqCounts[log.OrgID][ts]++
		}
	}

	verCounts := make(map[int32]map[time.Time]uint32)
	for _, log := range m.verifyLogs {
		if log.UserID == userID && !log.Timestamp.Before(from) {
			y, month, _ := log.Timestamp.Date()
			ts := time.Date(y, month, 1, 0, 0, 0, 0, log.Timestamp.Location())
			if _, ok := verCounts[log.OrgID]; !ok {
				verCounts[log.OrgID] = make(map[time.Time]uint32)
			}
			verCounts[log.OrgID][ts]++
		}
	}

	// For each (orgID, month) use max of request and verify counts
	counts := make(map[int32]map[time.Time]uint32)
	for orgID, orgCounts := range reqCounts {
		counts[orgID] = make(map[time.Time]uint32)
		for ts, count := range orgCounts {
			counts[orgID][ts] = count
		}
	}
	for orgID, orgCounts := range verCounts {
		if _, ok := counts[orgID]; !ok {
			counts[orgID] = make(map[time.Time]uint32)
		}
		for ts, count := range orgCounts {
			if count > counts[orgID][ts] {
				counts[orgID][ts] = count
			}
		}
	}

	results := make([]*common.OrgTimeCount, 0)
	for orgID, orgCounts := range counts {
		for ts, count := range orgCounts {
			results = append(results, &common.OrgTimeCount{OrgID: orgID, Timestamp: ts, Count: count})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].OrgID == results[j].OrgID {
			return results[i].Timestamp.Before(results[j].Timestamp)
		}
		return results[i].OrgID < results[j].OrgID
	})

	return results, nil
}

func (m *MemoryTimeSeries) memoryReportStats(userID int32, from, mid, to time.Time) *common.UserReportStats {
	type propKey struct {
		PropertyID int32
		OrgID      int32
	}

	type propCounts struct {
		CurrentRequests uint64
		PrevRequests    uint64
		CurrentVerifies uint64
		PrevVerifies    uint64
	}

	counts := make(map[propKey]*propCounts)

	for _, log := range m.accessLogs {
		if log.UserID == userID && !log.Timestamp.Before(from) && log.Timestamp.Before(to) {
			key := propKey{PropertyID: log.PropertyID, OrgID: log.OrgID}
			if counts[key] == nil {
				counts[key] = &propCounts{}
			}
			if !log.Timestamp.Before(mid) {
				counts[key].CurrentRequests++
			} else {
				counts[key].PrevRequests++
			}
		}
	}

	for _, log := range m.verifyLogs {
		if log.UserID == userID && !log.Timestamp.Before(from) && log.Timestamp.Before(to) {
			key := propKey{PropertyID: log.PropertyID, OrgID: log.OrgID}
			if counts[key] == nil {
				counts[key] = &propCounts{}
			}
			if !log.Timestamp.Before(mid) {
				counts[key].CurrentVerifies++
			} else {
				counts[key].PrevVerifies++
			}
		}
	}

	stats := &common.UserReportStats{}
	for key, c := range counts {
		stats.Properties = append(stats.Properties, &common.UserReportPropertyStat{
			PropertyID:      key.PropertyID,
			OrgID:           key.OrgID,
			CurrentRequests: c.CurrentRequests,
			PrevRequests:    c.PrevRequests,
			CurrentVerifies: c.CurrentVerifies,
			PrevVerifies:    c.PrevVerifies,
		})
		stats.TotalCurrentRequests += c.CurrentRequests
		stats.TotalPrevRequests += c.PrevRequests
		stats.TotalCurrentVerifies += c.CurrentVerifies
		stats.TotalPrevVerifies += c.PrevVerifies
	}

	sort.Slice(stats.Properties, func(i, j int) bool {
		return stats.Properties[i].CurrentRequests > stats.Properties[j].CurrentRequests
	})

	return stats
}

func (m *MemoryTimeSeries) RetrieveWeeklyPropertiesReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*common.UserReportStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memoryReportStats(userID, from, mid, to), nil
}

func (m *MemoryTimeSeries) RetrieveMonthlyPropertiesReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*common.UserReportStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memoryReportStats(userID, from, mid, to), nil
}

func (m *MemoryTimeSeries) memoryFormsReportStats(userID int32, from, mid, to time.Time) *common.UserFormsReportStats {
	type formKey struct {
		FormID int32
		OrgID  int32
	}

	type formCounts struct {
		CurrentSubmissions uint64
		PrevSubmissions    uint64
		CurrentErrors      uint64
		PrevErrors         uint64
	}

	counts := make(map[formKey]*formCounts)

	for _, log := range m.formSubmitLogs {
		if log.UserID != userID || log.Timestamp.Before(from) || !log.Timestamp.Before(to) {
			continue
		}

		key := formKey{FormID: log.FormID, OrgID: log.OrgID}
		if counts[key] == nil {
			counts[key] = &formCounts{}
		}

		isCurrent := !log.Timestamp.Before(mid)
		if isCurrent {
			counts[key].CurrentSubmissions++
			if log.Status != 0 {
				counts[key].CurrentErrors++
			}
		} else {
			counts[key].PrevSubmissions++
			if log.Status != 0 {
				counts[key].PrevErrors++
			}
		}
	}

	stats := &common.UserFormsReportStats{}
	for key, c := range counts {
		stats.Forms = append(stats.Forms, &common.UserReportFormStat{
			FormID:             key.FormID,
			OrgID:              key.OrgID,
			CurrentSubmissions: c.CurrentSubmissions,
			PrevSubmissions:    c.PrevSubmissions,
			CurrentErrors:      c.CurrentErrors,
			PrevErrors:         c.PrevErrors,
		})
		stats.TotalCurrentSubmissions += c.CurrentSubmissions
		stats.TotalPrevSubmissions += c.PrevSubmissions
		stats.TotalCurrentErrors += c.CurrentErrors
		stats.TotalPrevErrors += c.PrevErrors
	}

	sort.Slice(stats.Forms, func(i, j int) bool {
		return stats.Forms[i].CurrentSubmissions > stats.Forms[j].CurrentSubmissions
	})

	return stats
}

func (m *MemoryTimeSeries) RetrieveWeeklyFormsReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*common.UserFormsReportStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memoryFormsReportStats(userID, from, mid, to), nil
}

func (m *MemoryTimeSeries) RetrieveMonthlyFormsReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*common.UserFormsReportStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memoryFormsReportStats(userID, from, mid, to), nil
}

func (m *MemoryTimeSeries) RetrievePropertyStatsByPeriod(ctx context.Context, orgID, propertyID int32, period common.TimePeriod) ([]*common.TimePeriodStat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	from := getStartTime(period)
	statsMap := make(map[time.Time]*common.TimePeriodStat)

	// Define truncation function based on period
	var truncate func(time.Time) time.Time
	switch period {
	case common.TimePeriodToday:
		// 1h
		truncate = func(t time.Time) time.Time { return t.Truncate(time.Hour) }
	case common.TimePeriodWeek:
		// Real DB uses request_logs_1d, so effectively daily resolution
		truncate = func(t time.Time) time.Time {
			y, m, d := t.Date()
			return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
		}
	case common.TimePeriodMonth:
		// 1d
		truncate = func(t time.Time) time.Time {
			y, m, d := t.Date()
			return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
		}
	case common.TimePeriodYear:
		// 1mo
		truncate = func(t time.Time) time.Time {
			y, m, _ := t.Date()
			return time.Date(y, m, 1, 0, 0, 0, 0, t.Location())
		}
	default:
		truncate = func(t time.Time) time.Time { return t.Truncate(time.Hour) }
	}

	getStat := func(t time.Time) *common.TimePeriodStat {
		ts := truncate(t)
		if _, ok := statsMap[ts]; !ok {
			statsMap[ts] = &common.TimePeriodStat{Timestamp: ts}
		}
		return statsMap[ts]
	}

	for _, log := range m.accessLogs {
		if log.OrgID == orgID && log.PropertyID == propertyID && !log.Timestamp.Before(from) {
			getStat(log.Timestamp).RequestsCount++
		}
	}

	for _, log := range m.verifyLogs {
		if log.OrgID == orgID && log.PropertyID == propertyID && !log.Timestamp.Before(from) {
			getStat(log.Timestamp).VerifiesCount++
		}
	}

	// Convert map to sorted slice
	result := make([]*common.TimePeriodStat, 0, len(statsMap))
	for _, v := range statsMap {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Timestamp.Before(result[j].Timestamp) })

	return result, nil
}

func (m *MemoryTimeSeries) RetrieveFormStatsByPeriod(ctx context.Context, orgID, formID int32, period common.TimePeriod) ([]*common.FormSubmitStat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tnow := time.Now().UTC()
	var from time.Time
	statsMap := make(map[time.Time]*common.FormSubmitStat)

	var truncate func(time.Time) time.Time
	switch period {
	case common.TimePeriodToday:
		from = tnow.AddDate(0, 0, -1).Truncate(time.Hour)
		truncate = func(t time.Time) time.Time { return t.UTC().Truncate(time.Hour) }
	case common.TimePeriodWeek:
		from = tnow.AddDate(0, 0, -7).Truncate(24 * time.Hour)
		truncate = func(t time.Time) time.Time {
			y, m, d := t.UTC().Date()
			return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		}
	case common.TimePeriodMonth:
		from = tnow.AddDate(0, -1, 0).Truncate(24 * time.Hour)
		truncate = func(t time.Time) time.Time {
			y, m, d := t.UTC().Date()
			return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		}
	case common.TimePeriodYear:
		from = tnow.AddDate(-1, 0, 0).Truncate(24 * time.Hour)
		truncate = func(t time.Time) time.Time {
			y, m, _ := t.UTC().Date()
			return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
		}
	default:
		return nil, ErrUnsupportedPeriod
	}

	for _, log := range m.formSubmitLogs {
		if log.OrgID != orgID || log.FormID != formID || log.Timestamp.UTC().Before(from) {
			continue
		}

		ts := truncate(log.Timestamp)
		if _, ok := statsMap[ts]; !ok {
			statsMap[ts] = &common.FormSubmitStat{Timestamp: ts}
		}
		if log.Status == 0 {
			statsMap[ts].SuccessCount++
		} else {
			statsMap[ts].FailureCount++
		}
	}

	result := make([]*common.FormSubmitStat, 0, len(statsMap))
	for _, stat := range statsMap {
		result = append(result, stat)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Timestamp.Before(result[j].Timestamp) })

	return result, nil
}

func (m *MemoryTimeSeries) RetrieveFailingForms(ctx context.Context, threshold, limit int) ([]*common.FailingFormCandidate, error) {
	if (threshold <= 0) || (limit <= 0) {
		return nil, ErrInvalidInput
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	type formHourKey struct {
		formID    int32
		timestamp time.Time
	}

	from := time.Now().UTC().Add(-24 * time.Hour)
	stats := make(map[formHourKey]*common.FormSubmitStat)
	for _, log := range m.formSubmitLogs {
		if log.Timestamp.UTC().Before(from) {
			continue
		}

		key := formHourKey{formID: log.FormID, timestamp: log.Timestamp.UTC().Truncate(time.Hour)}
		if stats[key] == nil {
			stats[key] = &common.FormSubmitStat{Timestamp: key.timestamp}
		}
		if log.Status == 0 {
			stats[key].SuccessCount++
		} else {
			stats[key].FailureCount++
		}
	}

	byForm := make(map[int32][]*common.FormSubmitStat)
	for key, stat := range stats {
		if (stat.SuccessCount + stat.FailureCount) == 0 {
			continue
		}
		byForm[key.formID] = append(byForm[key.formID], stat)
	}

	results := make([]*common.FailingFormCandidate, 0)
	for formID, formStats := range byForm {
		sort.Slice(formStats, func(i, j int) bool { return formStats[i].Timestamp.After(formStats[j].Timestamp) })
		if len(formStats) < threshold {
			continue
		}

		var failureCount uint32
		qualified := true
		for _, stat := range formStats[:threshold] {
			if stat.SuccessCount != 0 || stat.FailureCount == 0 {
				qualified = false
				break
			}
			failureCount += uint32(stat.FailureCount)
		}

		if qualified {
			results = append(results, &common.FailingFormCandidate{FormID: formID, FailureCount: failureCount})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].FailureCount == results[j].FailureCount {
			return results[i].FormID < results[j].FormID
		}
		return results[i].FailureCount > results[j].FailureCount
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (m *MemoryTimeSeries) RetrievePropertyRuleStatsByPeriod(ctx context.Context, userID, orgID, propertyID int32, period common.TimePeriod) ([]*common.TimeCount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Only support week and month periods for rule stats
	if period != common.TimePeriodWeek && period != common.TimePeriodMonth {
		return nil, ErrUnsupportedPeriod
	}

	from := getStartTime(period).UTC()
	statsMap := make(map[time.Time]uint32)

	// For rule stats, always use daily resolution
	truncate := func(t time.Time) time.Time {
		y, m, d := t.UTC().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}

	// Count only logs with rule_id > 0
	for _, log := range m.accessLogs {
		if log.UserID == userID && log.OrgID == orgID && log.PropertyID == propertyID && log.RuleID > 0 && !log.Timestamp.UTC().Before(from) {
			ts := truncate(log.Timestamp)
			statsMap[ts]++
		}
	}

	// Convert map to sorted slice
	result := make([]*common.TimeCount, 0, len(statsMap))
	for ts, count := range statsMap {
		result = append(result, &common.TimeCount{Timestamp: ts, Count: count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Timestamp.Before(result[j].Timestamp) })

	return result, nil
}

func (m *MemoryTimeSeries) RetrieveRecentTopProperties(ctx context.Context, limit int) (map[int32]uint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	since := time.Now().Add(-24 * time.Hour)
	counts := make(map[int32]uint)

	// Real DB uses verify_logs_1d (Verifications), not access logs
	for _, log := range m.verifyLogs {
		if !log.Timestamp.Before(since) {
			counts[log.PropertyID]++
		}
	}

	// For a stub, we just return the map
	if len(counts) <= limit {
		return counts, nil
	}

	// Minimal truncation logic for the limit (optional for a simple stub)
	limitedCounts := make(map[int32]uint)
	count := 0
	for k, v := range counts {
		if count >= limit {
			break
		}
		limitedCounts[k] = v
		count++
	}

	return limitedCounts, nil
}

func (m *MemoryTimeSeries) DeletePropertiesData(ctx context.Context, propertyIDs []int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make(map[int32]struct{})
	for _, id := range propertyIDs {
		ids[id] = struct{}{}
	}

	newAccess := m.accessLogs[:0]
	for _, log := range m.accessLogs {
		if _, ok := ids[log.PropertyID]; !ok {
			newAccess = append(newAccess, log)
		}
	}
	m.accessLogs = newAccess

	newVerify := m.verifyLogs[:0]
	for _, log := range m.verifyLogs {
		if _, ok := ids[log.PropertyID]; !ok {
			newVerify = append(newVerify, log)
		}
	}
	m.verifyLogs = newVerify

	return nil
}

func (m *MemoryTimeSeries) DeleteFormsData(ctx context.Context, formIDs []int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make(map[int32]struct{})
	for _, id := range formIDs {
		ids[id] = struct{}{}
	}

	newFormSubmitLogs := m.formSubmitLogs[:0]
	for _, log := range m.formSubmitLogs {
		if _, ok := ids[log.FormID]; !ok {
			newFormSubmitLogs = append(newFormSubmitLogs, log)
		}
	}
	m.formSubmitLogs = newFormSubmitLogs

	return nil
}

func (m *MemoryTimeSeries) DeleteOrganizationsData(ctx context.Context, orgIDs []int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make(map[int32]struct{})
	for _, id := range orgIDs {
		ids[id] = struct{}{}
	}

	newAccess := m.accessLogs[:0]
	for _, log := range m.accessLogs {
		if _, ok := ids[log.OrgID]; !ok {
			newAccess = append(newAccess, log)
		}
	}
	m.accessLogs = newAccess

	newVerify := m.verifyLogs[:0]
	for _, log := range m.verifyLogs {
		if _, ok := ids[log.OrgID]; !ok {
			newVerify = append(newVerify, log)
		}
	}
	m.verifyLogs = newVerify

	newFormSubmitLogs := m.formSubmitLogs[:0]
	for _, log := range m.formSubmitLogs {
		if _, ok := ids[log.OrgID]; !ok {
			newFormSubmitLogs = append(newFormSubmitLogs, log)
		}
	}
	m.formSubmitLogs = newFormSubmitLogs

	return nil
}

func (m *MemoryTimeSeries) DeleteUsersData(ctx context.Context, userIDs []int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make(map[int32]struct{})
	for _, id := range userIDs {
		ids[id] = struct{}{}
	}

	newAccess := m.accessLogs[:0]
	for _, log := range m.accessLogs {
		if _, ok := ids[log.UserID]; !ok {
			newAccess = append(newAccess, log)
		}
	}
	m.accessLogs = newAccess

	newVerify := m.verifyLogs[:0]
	for _, log := range m.verifyLogs {
		if _, ok := ids[log.UserID]; !ok {
			newVerify = append(newVerify, log)
		}
	}
	m.verifyLogs = newVerify

	newFormSubmitLogs := m.formSubmitLogs[:0]
	for _, log := range m.formSubmitLogs {
		if _, ok := ids[log.UserID]; !ok {
			newFormSubmitLogs = append(newFormSubmitLogs, log)
		}
	}
	m.formSubmitLogs = newFormSubmitLogs

	return nil
}

func mapToTimeCount(m map[time.Time]uint32) []*common.TimeCount {
	res := make([]*common.TimeCount, 0, len(m))
	for ts, count := range m {
		res = append(res, &common.TimeCount{Timestamp: ts, Count: count})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp.Before(res[j].Timestamp) })
	return res
}

func getStartTime(p common.TimePeriod) time.Time {
	now := time.Now()
	switch p {
	case common.TimePeriodToday:
		return now.AddDate(0, 0, -1)
	case common.TimePeriodWeek:
		return now.AddDate(0, 0, -7)
	case common.TimePeriodMonth:
		return now.AddDate(0, -1, 0)
	case common.TimePeriodYear:
		return now.AddDate(-1, 0, 0)
	default:
		return now
	}
}
