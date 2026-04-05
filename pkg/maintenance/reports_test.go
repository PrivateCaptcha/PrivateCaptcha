package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

func seedTimeSeries(t *testing.T, ts *db.MemoryTimeSeries, userID int32, propID, orgID int32, timestamp time.Time, count int) {
	t.Helper()
	ctx := context.Background()
	records := make([]*common.AccessRecord, count)
	for i := range records {
		records[i] = &common.AccessRecord{
			UserID:     userID,
			PropertyID: propID,
			OrgID:      orgID,
			Timestamp:  timestamp.Add(time.Duration(i) * time.Minute),
		}
	}
	if err := ts.WriteAccessLogBatch(ctx, records); err != nil {
		t.Fatal(err)
	}
}

func seedVerifyLogs(t *testing.T, ts *db.MemoryTimeSeries, userID int32, propID, orgID int32, timestamp time.Time, count int) {
	t.Helper()
	ctx := context.Background()
	records := make([]*common.VerifyRecord, count)
	for i := range records {
		records[i] = &common.VerifyRecord{
			UserID:     userID,
			PropertyID: propID,
			OrgID:      orgID,
			Timestamp:  timestamp.Add(time.Duration(i) * time.Minute),
			Status:     1,
		}
	}
	if err := ts.WriteVerifyLogBatch(ctx, records); err != nil {
		t.Fatal(err)
	}
}

func createTestStore(t *testing.T) db.Implementor {
	t.Helper()
	return db.NewBusiness(nil)
}

func TestBuildWeeklyReportWithData(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(1)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC) // Monday truncated
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 50)
	seedTimeSeries(t, ts, userID, 10, 1, from, 80)
	seedVerifyLogs(t, ts, userID, 10, 1, from, 40)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.Period != "weekly" {
		t.Errorf("expected period 'weekly', got %q", result.Period)
	}
	if result.TotalRequests != 100 {
		t.Errorf("expected TotalRequests=100, got %d", result.TotalRequests)
	}
	if result.PrevRequests != 80 {
		t.Errorf("expected PrevRequests=80, got %d", result.PrevRequests)
	}
	if result.TotalVerifies != 50 {
		t.Errorf("expected TotalVerifies=50, got %d", result.TotalVerifies)
	}
	if result.PrevVerifies != 40 {
		t.Errorf("expected PrevVerifies=40, got %d", result.PrevVerifies)
	}
	if result.RequestsChange <= 0 {
		t.Errorf("expected positive RequestsChange, got %f", result.RequestsChange)
	}
	if result.VerifiesChange <= 0 {
		t.Errorf("expected positive VerifiesChange, got %f", result.VerifiesChange)
	}
	if result.VerificationRate == 0 {
		t.Error("expected non-zero VerificationRate")
	}
}

func TestBuildMonthlyReportWithData(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(2)

	now := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, -1, 0)
	from := now.AddDate(0, -2, 0)

	seedTimeSeries(t, ts, userID, 20, 2, mid, 200)
	seedVerifyLogs(t, ts, userID, 20, 2, mid, 100)
	seedTimeSeries(t, ts, userID, 20, 2, from, 150)
	seedVerifyLogs(t, ts, userID, 20, 2, from, 80)

	result, err := BuildMonthlyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.Period != "monthly" {
		t.Errorf("expected period 'monthly', got %q", result.Period)
	}
	if result.TotalRequests != 200 {
		t.Errorf("expected TotalRequests=200, got %d", result.TotalRequests)
	}
	if result.PrevRequests != 150 {
		t.Errorf("expected PrevRequests=150, got %d", result.PrevRequests)
	}
}

func TestBuildWeeklyReportNoData(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(3)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalRequests != 0 {
		t.Errorf("expected TotalRequests=0, got %d", result.TotalRequests)
	}
	if result.TotalVerifies != 0 {
		t.Errorf("expected TotalVerifies=0, got %d", result.TotalVerifies)
	}
	if result.PrevRequests != 0 {
		t.Errorf("expected PrevRequests=0, got %d", result.PrevRequests)
	}
	if result.PrevVerifies != 0 {
		t.Errorf("expected PrevVerifies=0, got %d", result.PrevVerifies)
	}
	if result.VerificationRate != 0 {
		t.Errorf("expected VerificationRate=0, got %f", result.VerificationRate)
	}
	if len(result.TopProperties) != 0 {
		t.Errorf("expected no TopProperties, got %d", len(result.TopProperties))
	}
	if result.RequestsChange != 0 {
		t.Errorf("expected RequestsChange=0, got %f", result.RequestsChange)
	}
}

func TestBuildMonthlyReportNoData(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(30)

	now := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, -1, 0)
	from := now.AddDate(0, -2, 0)

	result, err := BuildMonthlyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalRequests != 0 {
		t.Errorf("expected TotalRequests=0, got %d", result.TotalRequests)
	}
	if result.Period != "monthly" {
		t.Errorf("expected period 'monthly', got %q", result.Period)
	}
}

func TestBuildWeeklyReportNoPreviousPeriod(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(4)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 50)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 30)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalRequests != 50 {
		t.Errorf("expected TotalRequests=50, got %d", result.TotalRequests)
	}
	if result.PrevRequests != 0 {
		t.Errorf("expected PrevRequests=0, got %d", result.PrevRequests)
	}
	if result.RequestsChange != 100 {
		t.Errorf("expected RequestsChange=100, got %f", result.RequestsChange)
	}
}

func TestBuildMonthlyReportNoPreviousPeriod(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(40)

	now := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, -1, 0)
	from := now.AddDate(0, -2, 0)

	seedTimeSeries(t, ts, userID, 20, 2, mid, 70)

	result, err := BuildMonthlyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalRequests != 70 {
		t.Errorf("expected TotalRequests=70, got %d", result.TotalRequests)
	}
	if result.PrevRequests != 0 {
		t.Errorf("expected PrevRequests=0, got %d", result.PrevRequests)
	}
	if result.RequestsChange != 100 {
		t.Errorf("expected RequestsChange=100, got %f", result.RequestsChange)
	}
}

func TestBuildWeeklyReportDecreaseShowsNegativeChange(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(5)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 30)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 20)
	seedTimeSeries(t, ts, userID, 10, 1, from, 60)
	seedVerifyLogs(t, ts, userID, 10, 1, from, 40)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.RequestsChange >= 0 {
		t.Errorf("expected negative RequestsChange for decrease, got %f", result.RequestsChange)
	}
	if result.VerifiesChange >= 0 {
		t.Errorf("expected negative VerifiesChange for decrease, got %f", result.VerifiesChange)
	}
}

func TestBuildWeeklyReportNoChangeShowsZero(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(6)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 50)
	seedTimeSeries(t, ts, userID, 10, 1, from, 50)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.RequestsChange != 0 {
		t.Errorf("expected RequestsChange=0, got %f", result.RequestsChange)
	}
}

func TestBuildWeeklyReportTopPropertiesWithCache(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(7)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedTimeSeries(t, ts, userID, 20, 1, mid, 50)

	prop1 := &dbgen.Property{ID: 10, Name: "Main Site", Domain: "example.com"}
	prop2 := &dbgen.Property{ID: 20, Name: "Blog", Domain: "blog.example.com"}
	_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(10), prop1)
	_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(20), prop2)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.TopProperties) != 2 {
		t.Fatalf("expected 2 TopProperties, got %d", len(result.TopProperties))
	}

	if result.TopProperties[0].Name != "Main Site" {
		t.Errorf("expected first property 'Main Site', got %q", result.TopProperties[0].Name)
	}
	if result.TopProperties[0].Count != 100 {
		t.Errorf("expected first property count=100, got %d", result.TopProperties[0].Count)
	}
	if result.TopProperties[1].Name != "Blog" {
		t.Errorf("expected second property 'Blog', got %q", result.TopProperties[1].Name)
	}
	if result.TopProperties[0].Alternate {
		t.Error("expected first property row to be unstriped")
	}
	if !result.TopProperties[1].Alternate {
		t.Error("expected second property row to be striped")
	}
}

func TestBuildWeeklyReportTopPropertiesLimitedTo5(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(8)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	for i := int32(1); i <= 7; i++ {
		seedTimeSeries(t, ts, userID, i*10, 1, mid, int(100-i*10))
		prop := &dbgen.Property{ID: i * 10, Name: "Prop", Domain: "example.com"}
		_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(i*10), prop)
	}

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.TopProperties) > topPropertiesLimit {
		t.Errorf("expected at most %d TopProperties, got %d", topPropertiesLimit, len(result.TopProperties))
	}
}

func TestBuildWeeklyReportVerificationRate(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(10)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 50)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	expectedRate := 50.0
	if result.VerificationRate != expectedRate {
		t.Errorf("expected VerificationRate=%f, got %f", expectedRate, result.VerificationRate)
	}
	if result.VerificationRateChange != 100 {
		t.Errorf("expected VerificationRateChange=100, got %f", result.VerificationRateChange)
	}
}

func TestBuildWeeklyReportVerificationRateChange(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(12)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 40)
	seedTimeSeries(t, ts, userID, 10, 1, from, 100)
	seedVerifyLogs(t, ts, userID, 10, 1, from, 50)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if result.VerificationRate != 40 {
		t.Errorf("expected VerificationRate=40, got %f", result.VerificationRate)
	}
	if result.VerificationRateChange >= 0 {
		t.Errorf("expected negative VerificationRateChange, got %f", result.VerificationRateChange)
	}
}

func TestPercentChange(t *testing.T) {
	tests := []struct {
		name     string
		current  uint64
		previous uint64
		expected float64
	}{
		{"zero to zero", 0, 0, 0},
		{"zero to positive", 0, 100, -100},
		{"positive to zero", 100, 0, 100},
		{"increase", 150, 100, 50},
		{"decrease", 50, 100, -50},
		{"equal", 100, 100, 0},
		{"double", 200, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentChange(tt.current, tt.previous)
			if got != tt.expected {
				t.Errorf("percentChange(%d, %d) = %f, want %f", tt.current, tt.previous, got, tt.expected)
			}
		})
	}
}

func TestBuildWeeklyReportPropertyChangeDirection(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(11)

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedTimeSeries(t, ts, userID, 10, 1, from, 50)
	seedTimeSeries(t, ts, userID, 20, 1, mid, 30)
	seedTimeSeries(t, ts, userID, 20, 1, from, 60)

	prop1 := &dbgen.Property{ID: 10, Name: "Up", Domain: "up.com"}
	prop2 := &dbgen.Property{ID: 20, Name: "Down", Domain: "down.com"}
	_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(10), prop1)
	_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(20), prop2)

	result, err := BuildWeeklyReport(ctx, store, ts, userID, from, mid, now)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.TopProperties) != 2 {
		t.Fatalf("expected 2 TopProperties, got %d", len(result.TopProperties))
	}

	var upProp, downProp *email.PropertyStat
	for i := range result.TopProperties {
		if result.TopProperties[i].Name == "Up" {
			upProp = &result.TopProperties[i]
		}
		if result.TopProperties[i].Name == "Down" {
			downProp = &result.TopProperties[i]
		}
	}

	if upProp == nil || downProp == nil {
		t.Fatal("expected both properties")
	}

	if upProp.Change <= 0 {
		t.Errorf("expected positive Change for increasing property, got %f", upProp.Change)
	}
	if downProp.Change >= 0 {
		t.Errorf("expected negative Change for decreasing property, got %f", downProp.Change)
	}
}

func TestReferenceSuffix(t *testing.T) {
	if got := weeklyReferenceSuffix(2025, 11); got != "/2025/11" {
		t.Errorf("weeklyReferenceSuffix(2025, 11) = %q, want %q", got, "/2025/11")
	}
	if got := monthlyReferenceSuffix(2025, time.March); got != "/2025/3" {
		t.Errorf("monthlyReferenceSuffix(2025, March) = %q, want %q", got, "/2025/3")
	}
}

func TestTruncateDay(t *testing.T) {
	input := time.Date(2025, 3, 17, 14, 30, 45, 123, time.UTC)
	expected := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	if got := truncateDay(input); !got.Equal(expected) {
		t.Errorf("truncateDay(%v) = %v, want %v", input, got, expected)
	}
}
