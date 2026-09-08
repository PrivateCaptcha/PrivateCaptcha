package api

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/medama-io/go-useragent"
)

func TestStatsMigrationCompatibility(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	rows, err := ts.Clickhouse.QueryContext(ctx, `
SELECT table, name
FROM system.columns
WHERE database = 'privatecaptcha'
  AND table IN ('request_logs', 'verify_logs')
  AND name IN ('puzzle_id', 'expires_at', 'ip_family', 'ip_prefix', 'browser', 'browser_major', 'os', 'device')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	want := map[string]map[string]bool{
		"request_logs": {
			"puzzle_id":     true,
			"expires_at":    true,
			"ip_family":     true,
			"ip_prefix":     true,
			"browser":       true,
			"browser_major": true,
			"os":            true,
			"device":        true,
		},
		"verify_logs": {
			"expires_at": true,
		},
	}

	for rows.Next() {
		var table, name string
		if err := rows.Scan(&table, &name); err != nil {
			t.Fatal(err)
		}
		delete(want[table], name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for table, columns := range want {
		for column := range columns {
			t.Errorf("missing %s.%s", table, column)
		}
	}

	now := time.Now().UTC()
	if err := timeSeries.WriteAccessLogBatch(ctx, []*common.AccessRecord{
		{
			UserID:     1,
			OrgID:      1,
			PropertyID: 1,
			Timestamp:  now,
		},
		{
			UserID:       1,
			OrgID:        1,
			PropertyID:   1,
			Timestamp:    now,
			PuzzleID:     1,
			ExpiresAt:    now.Add(time.Hour),
			IPFamily:     4,
			IPPrefix:     0xc00002,
			Browser:      "Chrome",
			BrowserMajor: 120,
			OS:           "Linux",
			Device:       "Desktop",
		},
	}); err != nil {
		t.Fatalf("access records: %v", err)
	}
	if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{
			UserID:     1,
			OrgID:      1,
			PropertyID: 1,
			Timestamp:  now,
			Status:     0,
		},
		{
			UserID:     1,
			OrgID:      1,
			PropertyID: 1,
			PuzzleID:   1,
			Timestamp:  now,
			ExpiresAt:  now.Add(time.Hour),
			Status:     0,
		},
	}); err != nil {
		t.Fatalf("verification records: %v", err)
	}
}

func TestAddVerifyRecord(t *testing.T) {
	t.Parallel()

	srv := &Server{
		VerifyLogChan: make(chan *common.VerifyRecord, 1),
		Levels:        difficulty.NewLevels(db.NewMemoryTimeSeries(), 1, time.Minute),
		Metrics:       monitoring.NewStub(),
	}
	expiresAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	srv.addVerifyRecord(t.Context(), &puzzle.VerifyResult{
		UserID:     1,
		OrgID:      2,
		PropertyID: 3,
		PuzzleID:   4,
		ExpiresAt:  expiresAt,
	})

	actual := <-srv.VerifyLogChan
	if !actual.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", actual.ExpiresAt, expiresAt)
	}
}

func TestPuzzleRequestStats(t *testing.T) {
	t.Parallel()

	property := &dbgen.Property{
		ID:               1,
		ExternalID:       pgtype.UUID{Valid: true, Bytes: [16]byte{1}},
		OrgID:            pgtype.Int4{Valid: true, Int32: 2},
		OrgOwnerID:       pgtype.Int4{Valid: true, Int32: 3},
		Level:            pgtype.Int2{Valid: true, Int16: int16(common.DifficultyLevelMedium)},
		Growth:           dbgen.DifficultyGrowthMedium,
		ValidityInterval: puzzle.MaxValidityPeriod,
	}
	capture := &capturingTimeSeries{
		TimeSeriesStore: db.NewMemoryTimeSeries(),
		accessRecords:   make(chan *common.AccessRecord, 1),
	}
	levels := difficulty.NewLevels(capture, 1, time.Minute)
	levels.Init(time.Hour, time.Hour, time.Second)
	defer levels.Shutdown()
	defer levels.Stop()
	verifier := NewVerifier(testsConfigStore(), nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), useragent.NewParser())
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(t.Context(), common.PropertyContextKey, property)
	ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, netip.MustParseAddr("192.0.2.129"))
	req := httptest.NewRequest("GET", "/puzzle", nil).WithContext(ctx)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	issued, _, err := verifier.PuzzleForRequest(req, levels, nil, rules.NewRequestInfo(req, ""))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case record := <-capture.accessRecords:
		if record.PuzzleID != issued.PuzzleID() || record.PuzzleID == 0 {
			t.Errorf("PuzzleID = %d, want nonzero issued ID %d", record.PuzzleID, issued.PuzzleID())
		}
		if !record.ExpiresAt.Equal(issued.Expiration()) {
			t.Errorf("ExpiresAt = %v, want signed expiration %v", record.ExpiresAt, issued.Expiration())
		}
		if record.IPFamily != 4 || record.IPPrefix != 0xc00002 {
			t.Errorf("IP = (%d, %#x), want IPv4 192.0.2/24", record.IPFamily, record.IPPrefix)
		}
		if record.Browser != "Chrome" || record.BrowserMajor != 120 || record.OS != "Linux" || record.Device != "Desktop" {
			t.Errorf("parsed UA = %+v, want Chrome 120 Linux Desktop", record)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for access record")
	}
}

func TestPuzzleOutcomeCorrelation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	const (
		propertyID = 777
		puzzleID   = 987654321
	)
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Hour)
	if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{UserID: 1, OrgID: 2, PropertyID: propertyID, PuzzleID: puzzleID, Timestamp: now, ExpiresAt: expiresAt, Status: 1},
		{UserID: 1, OrgID: 2, PropertyID: propertyID, PuzzleID: puzzleID, Timestamp: now, ExpiresAt: expiresAt, Status: 0},
		{UserID: 1, OrgID: 2, PropertyID: propertyID, PuzzleID: puzzleID, Timestamp: now, ExpiresAt: expiresAt, Status: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := timeSeries.WriteAccessLogBatch(ctx, []*common.AccessRecord{{
		Fingerprint:  42,
		UserID:       1,
		OrgID:        2,
		PropertyID:   propertyID,
		Timestamp:    now,
		PuzzleID:     puzzleID,
		ExpiresAt:    expiresAt,
		IPFamily:     4,
		IPPrefix:     0xc00002,
		Browser:      "Chrome",
		BrowserMajor: 120,
		OS:           "Linux",
		Device:       "Desktop",
	}}); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 10; attempt++ {
		var issued, fingerprint, ipFamily, browserMajor uint64
		var ipPrefix uint64
		var browser, os, device string
		var statuses []uint8
		var counts []uint64
		err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT
    maxMerge(issued),
    maxMerge(fingerprint),
    maxMerge(ip_family),
    maxMerge(ip_prefix),
    maxMerge(browser),
    maxMerge(browser_major),
    maxMerge(os),
    maxMerge(device),
    tupleElement(sumMapMerge(status_counts), 1),
    tupleElement(sumMapMerge(status_counts), 2)
FROM privatecaptcha.puzzle_outcomes_recent
WHERE property_id = {property_id:UInt32} AND puzzle_id = {puzzle_id:UInt64}`,
			clickhouse.Named("property_id", uint32(propertyID)), clickhouse.Named("puzzle_id", uint64(puzzleID))).Scan(
			&issued, &fingerprint, &ipFamily, &ipPrefix, &browser, &browserMajor, &os, &device, &statuses, &counts)
		if err != nil {
			t.Fatal(err)
		}

		statusCounts := make(map[uint8]uint64, len(statuses))
		for i, status := range statuses {
			statusCounts[status] = counts[i]
		}
		if issued == 1 && fingerprint == 42 && ipFamily == 4 && ipPrefix == 0xc00002 &&
			browser == "Chrome" && browserMajor == 120 && os == "Linux" && device == "Desktop" &&
			statusCounts[0] == 1 && statusCounts[1] == 2 {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatal("puzzle outcome did not converge")
}

func TestPuzzleStatsSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	rows, err := ts.Clickhouse.QueryContext(ctx, `
SELECT name, engine, create_table_query
FROM system.tables
WHERE database = 'privatecaptcha'
  AND (name = 'puzzle_outcomes_recent' OR startsWith(name, 'puzzle_stats_'))`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type expectedTable struct {
		engine string
		ttl    string
	}
	want := map[string]expectedTable{
		"puzzle_outcomes_recent":            {engine: "AggregatingMergeTree"},
		"puzzle_stats_finalized":            {engine: "Null"},
		"puzzle_stats_ip_daily":             {engine: "AggregatingMergeTree", ttl: "TTL expires_on + toIntervalYear(3)"},
		"puzzle_stats_ip_daily_mv":          {engine: "MaterializedView"},
		"puzzle_stats_user_agent_daily":     {engine: "AggregatingMergeTree", ttl: "TTL expires_on + toIntervalYear(1)"},
		"puzzle_stats_user_agent_daily_mv":  {engine: "MaterializedView"},
		"puzzle_stats_fingerprint_daily":    {engine: "AggregatingMergeTree", ttl: "TTL expires_on + toIntervalDay(7)"},
		"puzzle_stats_fingerprint_daily_mv": {engine: "MaterializedView"},
	}
	for rows.Next() {
		var name, engine, createTableQuery string
		if err := rows.Scan(&name, &engine, &createTableQuery); err != nil {
			t.Fatal(err)
		}
		expected, exists := want[name]
		if !exists {
			t.Errorf("unexpected puzzle statistics table %s", name)
			continue
		}
		if engine != expected.engine {
			t.Errorf("%s engine = %s, want %s", name, engine, expected.engine)
		}
		if expected.ttl != "" && !strings.Contains(createTableQuery, expected.ttl) {
			t.Errorf("%s definition does not contain %q: %s", name, expected.ttl, createTableQuery)
		}
		delete(want, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name := range want {
		t.Errorf("missing puzzle statistics table %s", name)
	}
}

func TestPuzzleStatsFinalization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Truncate(24 * time.Hour).Add(-23 * time.Hour)
	issuedAt := expiresAt.Add(-2 * time.Hour)
	activeExpiresAt := now.Add(time.Hour)
	const propertyID = 778

	if err := timeSeries.WriteAccessLogBatch(ctx, []*common.AccessRecord{
		{
			Fingerprint:  41,
			UserID:       1,
			OrgID:        2,
			PropertyID:   propertyID,
			Timestamp:    issuedAt,
			PuzzleID:     1,
			ExpiresAt:    expiresAt,
			IPFamily:     4,
			IPPrefix:     0xc00002,
			Browser:      "Chrome",
			BrowserMajor: 120,
			OS:           "Linux",
			Device:       "Desktop",
		},
		{
			Fingerprint:  42,
			UserID:       1,
			OrgID:        2,
			PropertyID:   propertyID,
			Timestamp:    issuedAt,
			PuzzleID:     2,
			ExpiresAt:    expiresAt,
			IPFamily:     4,
			IPPrefix:     0xc00002,
			Browser:      "Chrome",
			BrowserMajor: 120,
			OS:           "Linux",
			Device:       "Desktop",
		},
		{Fingerprint: 42, UserID: 1, OrgID: 2, PropertyID: propertyID, Timestamp: issuedAt, PuzzleID: 3, ExpiresAt: expiresAt, Browser: "Firefox", BrowserMajor: 121, OS: "Linux", Device: "Desktop"},
		{
			Fingerprint:  43,
			UserID:       1,
			OrgID:        2,
			PropertyID:   propertyID,
			Timestamp:    issuedAt,
			PuzzleID:     4,
			ExpiresAt:    activeExpiresAt,
			IPFamily:     4,
			IPPrefix:     0xc00002,
			Browser:      "Chrome",
			BrowserMajor: 120,
			OS:           "Linux",
			Device:       "Desktop",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{UserID: 1, OrgID: 2, PropertyID: propertyID, PuzzleID: 1, Timestamp: issuedAt, ExpiresAt: expiresAt, Status: 0},
		{UserID: 1, OrgID: 2, PropertyID: propertyID, PuzzleID: 1, Timestamp: issuedAt, ExpiresAt: expiresAt, Status: 1},
		{UserID: 1, OrgID: 2, PropertyID: propertyID, PuzzleID: 2, Timestamp: issuedAt, ExpiresAt: expiresAt, Status: 1},
	}); err != nil {
		t.Fatal(err)
	}

	waitForPuzzleOutcomes(t, ctx, ts, propertyID, 4)
	processed, err := ts.FinalizeNextPuzzleStats(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if !processed {
		t.Fatal("expected expired puzzle outcomes to be finalized")
	}
	var remainingOutcomes uint64
	if err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT countDistinct(puzzle_id)
FROM privatecaptcha.puzzle_outcomes_recent
WHERE property_id = {property_id:UInt32}`,
		clickhouse.Named("property_id", uint32(propertyID))).Scan(&remainingOutcomes); err != nil {
		t.Fatal(err)
	}
	if remainingOutcomes != 1 {
		t.Fatalf("remaining puzzle outcomes = %d, want 1 active puzzle", remainingOutcomes)
	}

	expiresOn := expiresAt.Format(time.DateOnly)
	assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on = {expires_on:Date}
    AND ip_family = 4
    AND ip_prefix = 0xc00002`, []any{
		clickhouse.Named("expires_on", expiresOn),
	}, 2, 2, 1, 1, map[uint8]uint64{0: 1, 1: 2})
	assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_user_agent_daily", `
expires_on = {expires_on:Date}
    AND browser = 'Chrome'
    AND browser_major = 120
    AND os = 'Linux'
    AND device = 'Desktop'`, []any{
		clickhouse.Named("expires_on", expiresOn),
	}, 2, 2, 1, 1, map[uint8]uint64{0: 1, 1: 2})
	assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_user_agent_daily", `
expires_on = {expires_on:Date}
    AND browser = 'Firefox'
    AND browser_major = 121
    AND os = 'Linux'
    AND device = 'Desktop'`, []any{
		clickhouse.Named("expires_on", expiresOn),
	}, 1, 0, 0, 0, map[uint8]uint64{})
	assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_fingerprint_daily", `
expires_on = {expires_on:Date}
    AND fingerprint = 42`, []any{
		clickhouse.Named("expires_on", expiresOn),
	}, 2, 1, 0, 1, map[uint8]uint64{1: 1})
}

func waitForPuzzleOutcomes(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, propertyID int32, want uint64) {
	t.Helper()
	for attempt := 0; attempt < 10; attempt++ {
		var count uint64
		err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT countDistinct(puzzle_id)
FROM privatecaptcha.puzzle_outcomes_recent
WHERE property_id = {property_id:UInt32}`,
			clickhouse.Named("property_id", uint32(propertyID))).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("puzzle outcome count did not reach %d", want)
}

func assertPuzzleStats(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, table, where string, args []any, issued, attempted, successful, failedOnly uint64, wantStatuses map[uint8]uint64) {
	t.Helper()
	var actualIssued, actualAttempted, actualSuccessful, actualFailedOnly uint64
	var statuses []uint8
	var counts []uint64
	err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT
    sumMerge(issued_puzzles),
    sumMerge(attempted_puzzles),
    sumMerge(successful_puzzles),
    sumMerge(failed_only_puzzles),
    tupleElement(sumMapMerge(status_counts), 1),
    tupleElement(sumMapMerge(status_counts), 2)
FROM `+table+`
WHERE `+where, args...).Scan(&actualIssued, &actualAttempted, &actualSuccessful, &actualFailedOnly, &statuses, &counts)
	if err != nil {
		t.Fatal(err)
	}
	actualStatuses := make(map[uint8]uint64, len(statuses))
	for i, status := range statuses {
		actualStatuses[status] = counts[i]
	}
	if actualIssued != issued || actualAttempted != attempted || actualSuccessful != successful || actualFailedOnly != failedOnly {
		t.Fatalf("metrics = (%d, %d, %d, %d), want (%d, %d, %d, %d)", actualIssued, actualAttempted, actualSuccessful, actualFailedOnly, issued, attempted, successful, failedOnly)
	}
	if len(actualStatuses) != len(wantStatuses) {
		t.Fatalf("status counts = %v, want %v", actualStatuses, wantStatuses)
	}
	for status, want := range wantStatuses {
		if actualStatuses[status] != want {
			t.Fatalf("status %d count = %d, want %d", status, actualStatuses[status], want)
		}
	}
}

type capturingTimeSeries struct {
	common.TimeSeriesStore
	accessRecords chan *common.AccessRecord
}

func (ts *capturingTimeSeries) WriteAccessLogBatch(ctx context.Context, records []*common.AccessRecord) error {
	for _, record := range records {
		copy := *record
		ts.accessRecords <- &copy
	}
	return nil
}
