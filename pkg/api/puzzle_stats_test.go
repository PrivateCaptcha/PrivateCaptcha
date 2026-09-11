package api

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"net/netip"
	"os"
	"reflect"
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

func TestAddVerifyRecord(t *testing.T) {
	t.Parallel()

	srv := &Server{
		VerifyLogChan: make(chan *common.VerifyRecord, 1),
		Levels:        difficulty.NewLevels(db.NewMemoryTimeSeries(), 1, time.Minute),
		Metrics:       monitoring.NewStub(),
	}
	expiresAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	ua := "private-captcha-go/1.2.3"
	srv.addVerifyRecord(t.Context(), &puzzle.VerifyResult{
		UserID:     1,
		OrgID:      2,
		PropertyID: 3,
		PuzzleID:   4,
		ExpiresAt:  expiresAt,
	}, ua)

	actual := <-srv.VerifyLogChan
	if !actual.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", actual.ExpiresAt, expiresAt)
	}
	if actual.UserAgent != ua {
		t.Errorf("UserAgent = %q, want go", actual.UserAgent)
	}

	srv.addVerifyRecord(t.Context(), &puzzle.VerifyResult{}, string(common.VerifyClientForm))
	if actual := <-srv.VerifyLogChan; actual.UserAgent != string(common.VerifyClientForm) {
		t.Errorf("UserAgent = %q, want form", actual.UserAgent)
	}
}

func TestVerifyUserAgent(t *testing.T) {
	t.Parallel()

	tests := map[string]common.VerifyClient{
		"private-captcha-java/1.0.0":   common.VerifyClientJava,
		"private-captcha-php/1.0.0":    common.VerifyClientPHP,
		"private-captcha-go/1.0.0":     common.VerifyClientGo,
		"private-captcha-dotnet/1.0.0": common.VerifyClientDotNet,
		"private-captcha-ruby/1.0.0":   common.VerifyClientRuby,
		"private-captcha-py/1.0.0":     common.VerifyClientPython,
		"private-captcha-js/1.0.0":     common.VerifyClientJS,
		"libcurl/8.16.0":               common.VerifyClientCurl,
		"private-captcha-go/":          common.VerifyClientGo,
		"private-captcha-java":         common.VerifyClientJava,
		"Private-Captcha-go/1.0.0":     common.VerifyClientUnknown,
		"curl/8.0":                     common.VerifyClientCurl,
		"":                             common.VerifyClientUnknown,
	}

	for userAgent, want := range tests {
		if actual := common.VerifyClientFromUserAgent(userAgent); actual != want {
			t.Errorf("VerifyClientFromUserAgent(%q) = %q, want %q", userAgent, actual, want)
		}
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
	uaParser := useragent.NewParser()
	verifier := NewVerifier(testsConfigStore(), nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), uaParser)
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	const ruleID = 42
	rulesPair := &rules.RulesPair{PropertyRules: rules.NewRulesCompiler(uaParser).Compile(t.Context(), []*dbgen.DifficultyRule{{
		ID:                ruleID,
		ConditionProperty: dbgen.RuleConditionPropertyAlways,
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       100,
		Enabled:           true,
	}})}

	ctx := context.WithValue(t.Context(), common.PropertyContextKey, property)
	ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, netip.MustParseAddr("192.0.2.129"))
	req := httptest.NewRequest("GET", "/puzzle", nil).WithContext(ctx)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	issued, _, err := verifier.PuzzleForRequest(req, levels, rulesPair, rules.NewRequestInfo(req, ""))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case record := <-capture.accessRecords:
		if record.RuleID != ruleID {
			t.Errorf("RuleID = %d, want %d", record.RuleID, ruleID)
		}
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
	for _, status := range []int8{1, 0, 1} {
		if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{{
			UserID: 9, OrgID: 8, PropertyID: propertyID, PuzzleID: puzzleID,
			Timestamp: now, ExpiresAt: expiresAt, Status: status,
		}}); err != nil {
			t.Fatal(err)
		}
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

	converged := false
	for attempt := 0; attempt < 10; attempt++ {
		var userID, orgID, issued, fingerprint, ipFamily, browserMajor uint64
		var ipPrefix uint64
		var browser, os, device string
		var statuses []uint8
		var counts []uint64
		err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT
	argMaxMerge(user_id),
	argMaxMerge(org_id),
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
			&userID, &orgID, &issued, &fingerprint, &ipFamily, &ipPrefix, &browser, &browserMajor, &os, &device, &statuses, &counts)
		if err != nil {
			t.Fatal(err)
		}

		statusCounts := make(map[uint8]uint64, len(statuses))
		for i, status := range statuses {
			statusCounts[status] = counts[i]
		}
		if userID == 1 && orgID == 2 && issued == 1 && fingerprint == 42 && ipFamily == 4 && ipPrefix == 0xc00002 &&
			browser == "Chrome" && browserMajor == 120 && os == "Linux" && device == "Desktop" &&
			statusCounts[0] == 1 && statusCounts[1] == 2 {
			converged = true
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !converged {
		t.Fatal("puzzle outcome did not converge")
	}

	assertVerifyStatusCounts(t, ctx, ts, "privatecaptcha.verify_logs_1h", propertyID, map[uint8]uint64{0: 1, 1: 2})
	assertVerifyStatusCounts(t, ctx, ts, "privatecaptcha.verify_logs_1d", propertyID, map[uint8]uint64{0: 1, 1: 2})
}

func TestPuzzleOutcomeTenantDeletionBeforeMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	const (
		orgDeletedUserID  = 400001
		deletedOrgID      = 400002
		orgPropertyID     = 400003
		userDeletedUserID = 400004
		userOrgID         = 400005
		userPropertyID    = 400006
	)
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Hour)
	if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{UserID: orgDeletedUserID, OrgID: deletedOrgID, PropertyID: orgPropertyID, PuzzleID: 1, Timestamp: now, ExpiresAt: expiresAt},
		{UserID: userDeletedUserID, OrgID: userOrgID, PropertyID: userPropertyID, PuzzleID: 2, Timestamp: now, ExpiresAt: expiresAt},
	}); err != nil {
		t.Fatal(err)
	}
	waitForPuzzleOutcomes(t, ctx, ts, orgPropertyID, 1)
	waitForPuzzleOutcomes(t, ctx, ts, userPropertyID, 1)

	if err := timeSeries.DeleteOrganizationsData(ctx, []int32{deletedOrgID}); err != nil {
		t.Fatal(err)
	}
	assertPuzzleOutcomeRows(t, ctx, ts, "property_id = {property_id:UInt32}", []any{
		clickhouse.Named("property_id", uint32(orgPropertyID)),
	}, 0)

	if err := timeSeries.DeleteUsersData(ctx, []int32{userDeletedUserID}); err != nil {
		t.Fatal(err)
	}
	assertPuzzleOutcomeRows(t, ctx, ts, "property_id = {property_id:UInt32}", []any{
		clickhouse.Named("property_id", uint32(userPropertyID)),
	}, 0)
}

func TestPuzzleOutcomeRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	var createQuery string
	if err := ts.Clickhouse.QueryRowContext(t.Context(), `
SELECT create_table_query
FROM system.tables
WHERE database = 'privatecaptcha' AND name = 'puzzle_outcomes_recent'`).Scan(&createQuery); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(createQuery, "TTL expires_on + toIntervalDay(3)") &&
		!strings.Contains(createQuery, "TTL expires_on + INTERVAL 3 DAY") {
		t.Errorf("puzzle outcome table does not have a three-day TTL: %s", createQuery)
	}
}

// this test exists because we're using an aggregating column (`status_counts`) on SummingMergeTree engine (`verify_logs_1d`),
// which LUCKILY ClickHouse has an explicit special handling for (so we are safe). Test is just to be extra sure
func TestVerifyStatusCountsSurviveSummingMergeTreeMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	adminClickhouse := clickHouseAdmin(t, ctx)
	tables := []string{"privatecaptcha.verify_logs_1h", "privatecaptcha.verify_logs_1d"}
	mergesStopped := true
	t.Cleanup(func() {
		if mergesStopped {
			for _, table := range tables {
				if _, err := adminClickhouse.ExecContext(context.WithoutCancel(ctx), "SYSTEM START MERGES "+table); err != nil {
					t.Errorf("start merges for %s: %v", table, err)
				}
			}
		}
	})
	for _, table := range tables {
		if _, err := adminClickhouse.ExecContext(ctx, "SYSTEM STOP MERGES "+table); err != nil {
			t.Fatal(err)
		}
	}

	const (
		userID     = 10
		orgID      = 11
		propertyID = 781
	)
	timestamp := time.Now().UTC().Truncate(time.Second)
	for i, status := range []int8{1, 0, 1} {
		userAgent := []string{string(common.VerifyClientGo), string(common.VerifyClientGo), "arbitrary-client"}[i]
		if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{{
			UserID: userID, OrgID: orgID, PropertyID: propertyID,
			Timestamp: timestamp, UserAgent: userAgent, Status: status,
		}}); err != nil {
			t.Fatal(err)
		}
	}

	for _, table := range tables {
		assertVerifyAggregateRows(t, ctx, ts, table, propertyID, 3, 1, 2, map[uint8]uint64{0: 1, 1: 2})
		if _, err := adminClickhouse.ExecContext(ctx, "SYSTEM START MERGES "+table); err != nil {
			t.Fatal(err)
		}
	}
	mergesStopped = false

	for _, table := range tables {
		if _, err := adminClickhouse.ExecContext(ctx, "OPTIMIZE TABLE "+table+" FINAL"); err != nil {
			t.Fatal(err)
		}
	}
	assertVerifyAggregateRows(t, ctx, ts, "privatecaptcha.verify_logs_1h", propertyID, 2, 1, 2, map[uint8]uint64{0: 1, 1: 2})
	assertVerifyAggregateRows(t, ctx, ts, "privatecaptcha.verify_logs_1d", propertyID, 1, 1, 2, map[uint8]uint64{0: 1, 1: 2})
	assertVerifyUserAgentCounts(t, ctx, ts, propertyID, map[string][2]uint64{
		"go":      {1, 1},
		"unknown": {0, 1},
	})
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
	adminClickhouse := clickHouseAdmin(t, ctx)
	// This test uses old cohorts to exercise monthly recovery, so retain its source rows until cleanup.
	if _, err := adminClickhouse.ExecContext(ctx, `
ALTER TABLE privatecaptcha.puzzle_outcomes_recent
MODIFY TTL expires_on + INTERVAL 100 YEAR DELETE`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := adminClickhouse.ExecContext(context.WithoutCancel(ctx), `
ALTER TABLE privatecaptcha.puzzle_outcomes_recent
MODIFY TTL expires_on + INTERVAL 3 DAY DELETE`); err != nil {
			t.Errorf("restore puzzle outcome TTL: %v", err)
		}
	})
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -2, 0).Add(48 * time.Hour)
	issuedAt := expiresAt.Add(-2 * time.Hour)
	activeExpiresAt := now.Add(time.Hour)
	const (
		userID          = 12
		orgID           = 13
		otherUserID     = 14
		otherOrgID      = 15
		propertyID      = 778
		otherPropertyID = 780
	)
	expiresOn := expiresAt.Format(time.DateOnly)
	otherExpiresAt := expiresAt.Add(-24 * time.Hour)
	fingerprintExpiresAt := now.Truncate(24 * time.Hour).Add(-47 * time.Hour)
	expiresMonth := time.Date(expiresAt.Year(), expiresAt.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)

	if !t.Run("daily finalization", func(t *testing.T) {
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: expiresAt, userID: userID, orgID: orgID, propertyID: propertyID,
			puzzleID: 1, fingerprint: 41, ipFamily: 4, ipPrefix: 0xc00002,
			browser: "Chrome", browserMajor: 120, statusCodes: []uint8{0, 1}, statusCounts: []uint64{1, 1},
		})
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: expiresAt, userID: userID, orgID: orgID, propertyID: propertyID,
			puzzleID: 2, fingerprint: 42, ipFamily: 4, ipPrefix: 0xc00002,
			browser: "Chrome", browserMajor: 120, statusCodes: []uint8{1}, statusCounts: []uint64{1},
		})
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: expiresAt, userID: userID, orgID: orgID, propertyID: propertyID,
			puzzleID: 3, fingerprint: 42, browser: "Firefox", browserMajor: 121,
			statusCodes: []uint8{}, statusCounts: []uint64{},
		})
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: expiresAt, userID: otherUserID, orgID: otherOrgID, propertyID: otherPropertyID,
			puzzleID: 1, fingerprint: 41, ipFamily: 4, ipPrefix: 0xc00002,
			browser: "Chrome", browserMajor: 120, statusCodes: []uint8{0}, statusCounts: []uint64{1},
		})
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: expiresAt, userID: userID, orgID: orgID, propertyID: otherPropertyID,
			puzzleID: 2, fingerprint: 44, ipFamily: 4, ipPrefix: 0xc00002,
			browser: "Chrome", browserMajor: 121, statusCodes: []uint8{0}, statusCounts: []uint64{1},
		})
		if err := timeSeries.WriteAccessLogBatch(ctx, []*common.AccessRecord{{
			Fingerprint: 43, UserID: userID, OrgID: orgID, PropertyID: propertyID,
			Timestamp: issuedAt, PuzzleID: 4, ExpiresAt: activeExpiresAt, IPFamily: 4, IPPrefix: 0xc00002,
			Browser: "Chrome", BrowserMajor: 120, OS: "Linux", Device: "Desktop",
		}}); err != nil {
			t.Fatal(err)
		}

		waitForPuzzleOutcomes(t, ctx, ts, propertyID, 4)
		// Publish first, then finalize, to prove finalization is idempotent after a completed publication.
		if err := ts.PublishPuzzleStats(ctx, expiresAt); err != nil {
			t.Fatal(err)
		}
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

		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND property_id = {property_id:UInt32}
    AND ip_family = 4
    AND ip_prefix = 0xc00002`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID)),
			clickhouse.Named("property_id", uint32(propertyID)),
		}, 2, 2, 1)
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_user_agent_daily", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND property_id = {property_id:UInt32}
    AND browser = 'Chrome'
    AND browser_major = 120
    AND os = 'Linux'
    AND device = 'Desktop'`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID)),
			clickhouse.Named("property_id", uint32(propertyID)),
		}, 2, 2, 1)
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_user_agent_daily", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND property_id = {property_id:UInt32}
    AND browser = 'Firefox'
    AND browser_major = 121
    AND os = 'Linux'
    AND device = 'Desktop'`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID)),
			clickhouse.Named("property_id", uint32(propertyID)),
		}, 1, 0, 0)
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND property_id = {property_id:UInt32}
    AND ip_family = 4
    AND ip_prefix = 0xc00002`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("user_id", uint32(otherUserID)),
			clickhouse.Named("org_id", uint32(otherOrgID)),
			clickhouse.Named("property_id", uint32(otherPropertyID)),
		}, 1, 1, 1)
	}) {
		t.FailNow()
	}

	if !t.Run("second daily partition", func(t *testing.T) {
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: otherExpiresAt, userID: userID, orgID: orgID, propertyID: otherPropertyID,
			puzzleID: 3, fingerprint: 45, ipFamily: 4, ipPrefix: 0xc00002,
			browser: "Chrome", browserMajor: 122, statusCodes: []uint8{1}, statusCounts: []uint64{1},
		})
		processed, err := ts.FinalizeNextPuzzleStats(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if !processed {
			t.Fatal("expected second daily partition to be finalized")
		}
	}) {
		t.FailNow()
	}

	if !t.Run("fingerprint partition", func(t *testing.T) {
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: fingerprintExpiresAt, userID: userID, orgID: orgID, propertyID: propertyID,
			puzzleID: 10, fingerprint: 41, ipFamily: 4, ipPrefix: 0xc00002,
			browser: "Chrome", browserMajor: 120, statusCodes: []uint8{0}, statusCounts: []uint64{1},
		})
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: fingerprintExpiresAt, userID: userID, orgID: orgID, propertyID: otherPropertyID,
			puzzleID: 10, fingerprint: 41, ipFamily: 4, ipPrefix: 0xc00002,
			browser: "Chrome", browserMajor: 120, statusCodes: []uint8{0}, statusCounts: []uint64{1},
		})
		processed, err := ts.FinalizeNextPuzzleStats(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if !processed {
			t.Fatal("expected recent fingerprint partition to be finalized")
		}
		assertFingerprintPropertyUniq(t, ctx, ts, `
		expires_on = {expires_on:Date} AND fingerprint = 41`, []any{
			clickhouse.Named("expires_on", fingerprintExpiresAt.Format(time.DateOnly)),
		}, 2)
	}) {
		t.FailNow()
	}

	if !t.Run("monthly finalization", func(t *testing.T) {
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on >= {expires_on:Date}
    AND expires_on < addMonths({expires_on:Date}, 1)
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND ip_family = 4
    AND ip_prefix = 0xc00002`, []any{
			clickhouse.Named("expires_on", expiresMonth),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID)),
		}, 4, 4, 2)
		assertPuzzleStatsMonthly(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_monthly", `
expires_on = {expires_on:Date}`, []any{
			clickhouse.Named("expires_on", expiresMonth),
		}, 0, 0, 0)
		processed, err := ts.FinalizeNextPuzzleStatsMonth(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if !processed {
			t.Fatal("expected sealed month to be finalized")
		}
		assertPuzzleStatsMonthly(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_monthly", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND ip_family = 4
    AND ip_prefix = 0xc00002`, []any{
			clickhouse.Named("expires_on", expiresMonth),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID)),
		}, 1, 2, 2)
		assertPuzzleStatsMonthly(t, ctx, ts, "privatecaptcha.puzzle_stats_user_agent_monthly", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND browser = 'Chrome'
    AND os = 'Linux'
	    AND device = 'Desktop'`, []any{
			clickhouse.Named("expires_on", expiresMonth),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID)),
		}, 1, 2, 2)
	}) {
		t.FailNow()
	}

	if !t.Run("partial monthly replacement", func(t *testing.T) {
		if _, err := ts.Clickhouse.ExecContext(ctx, `
ALTER TABLE privatecaptcha.puzzle_stats_user_agent_monthly
DROP PARTITION {expires_on:Date}`, clickhouse.Named("expires_on", expiresMonth)); err != nil {
			t.Fatal(err)
		}
		if _, err := ts.Clickhouse.ExecContext(ctx, `
INSERT INTO privatecaptcha.puzzle_stats_user_agent_monthly
    (expires_on, user_id, org_id, browser, os, device, success_count, failure_count)
VALUES ({expires_on:Date}, {user_id:UInt32}, {org_id:UInt32}, 'Chrome', 'Linux', 'Desktop', 1, 0)`,
			clickhouse.Named("expires_on", expiresMonth),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID))); err != nil {
			t.Fatal(err)
		}
		if _, err := adminClickhouse.ExecContext(ctx, `
RENAME TABLE privatecaptcha.puzzle_stats_ip_monthly
TO privatecaptcha.puzzle_stats_ip_monthly_unavailable`); err != nil {
			t.Fatal(err)
		}
		ipTableRenamed := true
		t.Cleanup(func() {
			if ipTableRenamed {
				if _, err := adminClickhouse.ExecContext(context.WithoutCancel(ctx), `
RENAME TABLE privatecaptcha.puzzle_stats_ip_monthly_unavailable
TO privatecaptcha.puzzle_stats_ip_monthly`); err != nil {
					t.Errorf("restore IP monthly table: %v", err)
				}
			}
		})
		if processed, err := ts.FinalizeNextPuzzleStatsMonth(ctx, now); err == nil {
			t.Fatalf("between-drops finalization = (%v, nil), want error", processed)
		}
		if _, err := adminClickhouse.ExecContext(ctx, `
RENAME TABLE privatecaptcha.puzzle_stats_ip_monthly_unavailable
TO privatecaptcha.puzzle_stats_ip_monthly`); err != nil {
			t.Fatal(err)
		}
		ipTableRenamed = false

		processed, err := ts.FinalizeNextPuzzleStatsMonth(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if !processed {
			t.Fatal("expected partial monthly statistics to be replaced")
		}
		assertPuzzleStatsMonthly(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_monthly", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND ip_family = 4
    AND ip_prefix = 0xc00002`, []any{
			clickhouse.Named("expires_on", expiresMonth),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID)),
		}, 1, 2, 2)
		assertPuzzleStatsMonthly(t, ctx, ts, "privatecaptcha.puzzle_stats_user_agent_monthly", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND browser = 'Chrome'
    AND os = 'Linux'
	    AND device = 'Desktop'`, []any{
			clickhouse.Named("expires_on", expiresMonth),
			clickhouse.Named("user_id", uint32(userID)),
			clickhouse.Named("org_id", uint32(orgID)),
		}, 1, 2, 2)
	}) {
		t.FailNow()
	}

	if !t.Run("daily source retention", func(t *testing.T) {
		if _, err := ts.Clickhouse.ExecContext(ctx, `
ALTER TABLE privatecaptcha.puzzle_stats_ip_daily
DROP PARTITION {expires_on:Date}`, clickhouse.Named("expires_on", otherExpiresAt.Format(time.DateOnly))); err != nil {
			t.Fatal(err)
		}
		if _, err := ts.Clickhouse.ExecContext(ctx, `
ALTER TABLE privatecaptcha.puzzle_stats_user_agent_daily
DROP PARTITION {expires_on:Date}`, clickhouse.Named("expires_on", otherExpiresAt.Format(time.DateOnly))); err != nil {
			t.Fatal(err)
		}
		processed, err := ts.FinalizeNextPuzzleStatsMonth(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if processed {
			t.Fatal("monthly statistics were rebuilt after daily source retention")
		}
	}) {
		t.FailNow()
	}

	if !t.Run("watermark guard", func(t *testing.T) {
		processed, err := ts.FinalizeNextPuzzleStats(ctx, now.Add(48*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if processed {
			t.Fatal("ClickHouse watermark allowed an open cohort to be finalized")
		}
	}) {
		t.FailNow()
	}

	if !t.Run("property deletion", func(t *testing.T) {
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: now.Add(time.Hour), userID: userID, orgID: orgID, propertyID: otherPropertyID,
			puzzleID: 90, fingerprint: 45, ipFamily: 4, ipPrefix: 0xc00005,
			browser: "Chrome", browserMajor: 120,
		})
		if err := timeSeries.DeletePropertiesData(ctx, []int32{otherPropertyID}); err != nil {
			t.Fatal(err)
		}
		assertPuzzleOutcomeRows(t, ctx, ts, `property_id = {property_id:UInt32}`, []any{
			clickhouse.Named("property_id", uint32(otherPropertyID)),
		}, 0)
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on = {expires_on:Date} AND property_id = {property_id:UInt32}`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("property_id", uint32(otherPropertyID)),
		}, 0, 0, 0)
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_user_agent_daily", `
expires_on = {expires_on:Date} AND property_id = {property_id:UInt32}`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("property_id", uint32(otherPropertyID)),
		}, 0, 0, 0)
	}) {
		t.FailNow()
	}

	if !t.Run("organization deletion", func(t *testing.T) {
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: now.Add(time.Hour), userID: otherUserID, orgID: otherOrgID, propertyID: otherPropertyID + 1,
			puzzleID: 91, fingerprint: 46, ipFamily: 4, ipPrefix: 0xc00005,
			browser: "Chrome", browserMajor: 120,
		})
		if err := timeSeries.DeleteOrganizationsData(ctx, []int32{otherOrgID}); err != nil {
			t.Fatal(err)
		}
		assertPuzzleOutcomeRows(t, ctx, ts, `finalizeAggregation(org_id) = {org_id:UInt32}`, []any{
			clickhouse.Named("org_id", uint32(otherOrgID)),
		}, 0)
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on = {expires_on:Date} AND org_id = {org_id:UInt32}`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("org_id", uint32(otherOrgID)),
		}, 0, 0, 0)
		assertPuzzleStatsMonthly(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_monthly", `
expires_on = {expires_on:Date} AND org_id = {org_id:UInt32}`, []any{
			clickhouse.Named("expires_on", expiresMonth),
			clickhouse.Named("org_id", uint32(otherOrgID)),
		}, 0, 0, 0)
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on = {expires_on:Date} AND org_id = {org_id:UInt32}`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("org_id", uint32(orgID)),
		}, 2, 2, 1)
	}) {
		t.FailNow()
	}

	if !t.Run("user deletion", func(t *testing.T) {
		insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
			expiresAt: now.Add(time.Hour), userID: userID, orgID: orgID, propertyID: otherPropertyID + 2,
			puzzleID: 92, fingerprint: 47, ipFamily: 4, ipPrefix: 0xc00005,
			browser: "Chrome", browserMajor: 120,
		})
		if err := timeSeries.DeleteUsersData(ctx, []int32{userID}); err != nil {
			t.Fatal(err)
		}
		assertPuzzleOutcomeRows(t, ctx, ts, `finalizeAggregation(user_id) = {user_id:UInt32}`, []any{
			clickhouse.Named("user_id", uint32(userID)),
		}, 0)
		assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on = {expires_on:Date} AND user_id = {user_id:UInt32}`, []any{
			clickhouse.Named("expires_on", expiresOn),
			clickhouse.Named("user_id", uint32(userID)),
		}, 0, 0, 0)
		assertPuzzleStatsMonthly(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_monthly", `
expires_on = {expires_on:Date} AND user_id = {user_id:UInt32}`, []any{
			clickhouse.Named("expires_on", expiresMonth),
			clickhouse.Named("user_id", uint32(userID)),
		}, 0, 0, 0)
	}) {
		t.FailNow()
	}
}

func TestPuzzleStatsRejectsClosedCohortEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Truncate(24 * time.Hour).Add(-47 * time.Hour)
	const (
		userID     = 6
		orgID      = 7
		propertyID = 779
	)

	insertPuzzleOutcome(t, ctx, ts, puzzleOutcomeRow{
		expiresAt: expiresAt, userID: userID, orgID: orgID, propertyID: propertyID,
		puzzleID: 1, fingerprint: 51, ipFamily: 4, ipPrefix: 0xc00003,
		browser: "Chrome", browserMajor: 120, statusCodes: []uint8{0}, statusCounts: []uint64{1},
	})
	processed, err := ts.FinalizeNextPuzzleStats(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if !processed {
		t.Fatal("expected closed puzzle outcome cohort to be finalized")
	}

	if err := timeSeries.WriteAccessLogBatch(ctx, []*common.AccessRecord{{
		Fingerprint: 52, UserID: userID, OrgID: orgID, PropertyID: propertyID,
		Timestamp: now, PuzzleID: 2, ExpiresAt: expiresAt, IPFamily: 4, IPPrefix: 0xc00003,
		Browser: "Chrome", BrowserMajor: 120, OS: "Linux", Device: "Desktop",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{{
		UserID: userID, OrgID: orgID, PropertyID: propertyID, PuzzleID: 1,
		Timestamp: now, ExpiresAt: expiresAt, Status: 1,
	}}); err != nil {
		t.Fatal(err)
	}

	var outcomes uint64
	if err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT countDistinct(puzzle_id)
FROM privatecaptcha.puzzle_outcomes_recent
WHERE property_id = {property_id:UInt32}`,
		clickhouse.Named("property_id", uint32(propertyID))).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if outcomes != 0 {
		t.Fatalf("closed cohort outcomes = %d, want 0", outcomes)
	}

	processed, err = ts.FinalizeNextPuzzleStats(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if processed {
		t.Fatal("late events recreated a finalized source cohort")
	}

	assertPuzzleStats(t, ctx, ts, "privatecaptcha.puzzle_stats_ip_daily", `
expires_on = {expires_on:Date}
	AND user_id = {user_id:UInt32}
	AND org_id = {org_id:UInt32}
    AND ip_family = 4
    AND ip_prefix = 0xc00003`, []any{
		clickhouse.Named("expires_on", expiresAt.Format(time.DateOnly)),
		clickhouse.Named("user_id", uint32(userID)),
		clickhouse.Named("org_id", uint32(orgID)),
	}, 1, 1, 1)
}

func TestPuzzleStatsRejectsFutureCohortEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ts, ok := timeSeries.(*db.TimeSeriesDB)
	if !ok {
		t.Fatal("expected ClickHouse time-series store")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(10 * 24 * time.Hour)
	const (
		userID     = 16
		orgID      = 17
		propertyID = 782
	)
	if err := timeSeries.WriteAccessLogBatch(ctx, []*common.AccessRecord{{
		UserID: userID, OrgID: orgID, PropertyID: propertyID, PuzzleID: 1,
		Timestamp: now, ExpiresAt: expiresAt,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{{
		UserID: userID, OrgID: orgID, PropertyID: propertyID, PuzzleID: 2,
		Timestamp: now, ExpiresAt: expiresAt,
	}}); err != nil {
		t.Fatal(err)
	}

	assertPuzzleOutcomeRows(t, ctx, ts, "property_id = {property_id:UInt32}", []any{
		clickhouse.Named("property_id", uint32(propertyID)),
	}, 0)
}

type puzzleOutcomeRow struct {
	expiresAt    time.Time
	userID       int32
	orgID        int32
	propertyID   int32
	puzzleID     uint64
	fingerprint  uint64
	ipFamily     uint8
	ipPrefix     uint64
	browser      string
	browserMajor uint16
	statusCodes  []uint8
	statusCounts []uint64
}

func clickHouseAdmin(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	adminConfig := config.NewBaseConfig(cfg)
	adminConfig.Add(config.NewStaticValue(common.ClickHouseAdminKey, "default"))
	adminConfig.Add(config.NewStaticValue(common.ClickHouseAdminPasswordKey, os.Getenv("CH_ADMIN_PASSWORD")))
	adminConfig.Add(config.NewStaticValue(common.ClickHousePasswordKey, ""))
	pool, clickhouseDB, err := db.Connect(ctx, adminConfig, 3*time.Second, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if clickhouseDB == nil {
		pool.Close()
		t.Fatal("expected ClickHouse admin connection")
	}
	t.Cleanup(func() {
		pool.Close()
		if err := clickhouseDB.Close(); err != nil {
			t.Errorf("close ClickHouse admin connection: %v", err)
		}
	})
	return clickhouseDB
}

func insertPuzzleOutcome(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, row puzzleOutcomeRow) {
	t.Helper()
	_, err := ts.Clickhouse.ExecContext(ctx, `
INSERT INTO privatecaptcha.puzzle_outcomes_recent
SELECT
    {expires_on:Date},
    {property_id:UInt32},
    {puzzle_id:UInt64},
    argMaxState({user_id:UInt32}, toUInt8(1)),
    argMaxState({org_id:UInt32}, toUInt8(1)),
    maxState({fingerprint:UInt64}),
    maxState({ip_family:UInt8}),
    maxState({ip_prefix:UInt64}),
    maxState({browser:String}),
    maxState({browser_major:UInt16}),
    maxState('Linux'),
    maxState('Desktop'),
    maxState(toUInt8(1)),
    sumMapState({status_codes:Array(UInt8)}, {status_counts:Array(UInt64)})`,
		clickhouse.Named("expires_on", row.expiresAt.Format(time.DateOnly)),
		clickhouse.Named("property_id", uint32(row.propertyID)),
		clickhouse.Named("puzzle_id", row.puzzleID),
		clickhouse.Named("user_id", uint32(row.userID)),
		clickhouse.Named("org_id", uint32(row.orgID)),
		clickhouse.Named("fingerprint", row.fingerprint),
		clickhouse.Named("ip_family", row.ipFamily),
		clickhouse.Named("ip_prefix", row.ipPrefix),
		clickhouse.Named("browser", row.browser),
		clickhouse.Named("browser_major", row.browserMajor),
		clickhouse.Named("status_codes", row.statusCodes),
		clickhouse.Named("status_counts", row.statusCounts),
	)
	if err != nil {
		t.Fatal(err)
	}
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

func assertPuzzleOutcomeRows(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, where string, args []any, want uint64) {
	t.Helper()
	var actual uint64
	if err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT count()
FROM privatecaptcha.puzzle_outcomes_recent
WHERE `+where, args...).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != want {
		t.Fatalf("puzzle outcome rows = %d, want %d", actual, want)
	}
}

func assertPuzzleStats(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, table, where string, args []any, issued, attempted, successful uint64) {
	t.Helper()
	var actualIssued, actualAttempted, actualSuccessful uint64
	err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT
    sum(issued_puzzles),
    sum(attempted_puzzles),
    sum(successful_puzzles)
FROM `+table+`
WHERE `+where, args...).Scan(&actualIssued, &actualAttempted, &actualSuccessful)
	if err != nil {
		t.Fatal(err)
	}
	if actualIssued != issued || actualAttempted != attempted || actualSuccessful != successful {
		t.Fatalf("metrics = (%d, %d, %d), want (%d, %d, %d)", actualIssued, actualAttempted, actualSuccessful, issued, attempted, successful)
	}
}

func assertVerifyStatusCounts(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, table string, propertyID int32, want map[uint8]uint64) {
	t.Helper()
	var statuses []uint8
	var counts []uint64
	err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT
    tupleElement(sumMapMerge(status_counts), 1),
    tupleElement(sumMapMerge(status_counts), 2)
FROM `+table+`
WHERE property_id = {property_id:UInt32}`,
		clickhouse.Named("property_id", uint32(propertyID))).Scan(&statuses, &counts)
	if err != nil {
		t.Fatal(err)
	}
	actual := make(map[uint8]uint64, len(statuses))
	for i, status := range statuses {
		actual[status] = counts[i]
	}
	if len(actual) != len(want) {
		t.Fatalf("status counts = %v, want %v", actual, want)
	}
	for status, count := range want {
		if actual[status] != count {
			t.Fatalf("status %d count = %d, want %d", status, actual[status], count)
		}
	}
}

func assertVerifyUserAgentCounts(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, propertyID int32, want map[string][2]uint64) {
	t.Helper()
	rows, err := ts.Clickhouse.QueryContext(ctx, `
SELECT user_agent, sum(success_count), sum(failure_count)
FROM privatecaptcha.verify_logs_1h
WHERE property_id = {property_id:UInt32}
GROUP BY user_agent`, clickhouse.Named("property_id", uint32(propertyID)))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	actual := make(map[string][2]uint64, len(want))
	for rows.Next() {
		var userAgent string
		var success, failure uint64
		if err := rows.Scan(&userAgent, &success, &failure); err != nil {
			t.Fatal(err)
		}
		actual[userAgent] = [2]uint64{success, failure}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("user agent counts = %v, want %v", actual, want)
	}
}

func assertVerifyAggregateRows(
	t *testing.T,
	ctx context.Context,
	ts *db.TimeSeriesDB,
	table string,
	propertyID int32,
	wantRows, wantSuccess, wantFailure uint64,
	wantStatuses map[uint8]uint64,
) {
	t.Helper()
	var rows, success, failure uint64
	var statuses []uint8
	var counts []uint64
	if err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT
    count(),
    sum(success_count),
    sum(failure_count),
    tupleElement(sumMapMerge(status_counts), 1),
    tupleElement(sumMapMerge(status_counts), 2)
FROM `+table+`
WHERE property_id = {property_id:UInt32}`,
		clickhouse.Named("property_id", uint32(propertyID))).Scan(&rows, &success, &failure, &statuses, &counts); err != nil {
		t.Fatal(err)
	}
	if rows != wantRows || success != wantSuccess || failure != wantFailure {
		t.Fatalf("verify rows and metrics = (%d, %d, %d), want (%d, %d, %d)", rows, success, failure, wantRows, wantSuccess, wantFailure)
	}
	actualStatuses := make(map[uint8]uint64, len(statuses))
	for i, status := range statuses {
		actualStatuses[status] = counts[i]
	}
	if len(actualStatuses) != len(wantStatuses) {
		t.Fatalf("status counts = %v, want %v", actualStatuses, wantStatuses)
	}
	for status, count := range wantStatuses {
		if actualStatuses[status] != count {
			t.Fatalf("status %d count = %d, want %d", status, actualStatuses[status], count)
		}
	}
}

func assertPuzzleStatsMonthly(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, table, where string, args []any, rows, successful, failed uint64) {
	t.Helper()
	var actualRows, actualSuccessful, actualFailed uint64
	err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT count(), sum(success_count), sum(failure_count)
FROM `+table+`
WHERE `+where, args...).Scan(&actualRows, &actualSuccessful, &actualFailed)
	if err != nil {
		t.Fatal(err)
	}
	if actualRows != rows || actualSuccessful != successful || actualFailed != failed {
		t.Fatalf("monthly rows and metrics = (%d, %d, %d), want (%d, %d, %d)", actualRows, actualSuccessful, actualFailed, rows, successful, failed)
	}
}

func assertFingerprintPropertyUniq(t *testing.T, ctx context.Context, ts *db.TimeSeriesDB, where string, args []any, want uint64) {
	t.Helper()
	var actual uint64
	err := ts.Clickhouse.QueryRowContext(ctx, `
SELECT uniqMerge(property_uniq)
FROM privatecaptcha.puzzle_stats_fingerprint_daily
WHERE `+where, args...).Scan(&actual)
	if err != nil {
		t.Fatal(err)
	}
	if actual != want {
		t.Fatalf("unique properties = %d, want %d", actual, want)
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
