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

func TestUsageReportBuilderWeeklyWithData(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(1)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC) // Monday
	mid := now.AddDate(0, 0, -7)                         // previous Monday
	from := now.AddDate(0, 0, -14)                       // two weeks ago

	// Current week data: 100 requests, 50 verifies for prop 1
	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 50)

	// Previous week data: 80 requests, 40 verifies for prop 1
	seedTimeSeries(t, ts, userID, 10, 1, from, 80)
	seedVerifyLogs(t, ts, userID, 10, 1, from, 40)

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()

	result := b.Build()

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
	if result.RequestsSign != "+" {
		t.Errorf("expected RequestsSign='+', got %q", result.RequestsSign)
	}
	if result.RequestsColor != colorGreen {
		t.Errorf("expected RequestsColor=%q, got %q", colorGreen, result.RequestsColor)
	}
	if result.VerifiesSign != "+" {
		t.Errorf("expected VerifiesSign='+', got %q", result.VerifiesSign)
	}
	if result.VerifiesColor != colorGreen {
		t.Errorf("expected VerifiesColor=%q, got %q", colorGreen, result.VerifiesColor)
	}
	if result.VerificationRate == 0 {
		t.Error("expected non-zero VerificationRate")
	}
}

func TestUsageReportBuilderMonthlyWithData(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(2)

	now := time.Date(2025, 4, 1, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, -1, 0)
	from := now.AddDate(0, -2, 0)

	seedTimeSeries(t, ts, userID, 20, 2, mid, 200)
	seedVerifyLogs(t, ts, userID, 20, 2, mid, 100)

	seedTimeSeries(t, ts, userID, 20, 2, from, 150)
	seedVerifyLogs(t, ts, userID, 20, 2, from, 80)

	b := NewUsageReportBuilder(ctx, store, ts, userID, true, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()

	result := b.Build()

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

func TestUsageReportBuilderNoData(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(3)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()
	b.BuildTopProperties()

	result := b.Build()

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

func TestUsageReportBuilderNoPreviousPeriod(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(4)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	// Only current period data
	seedTimeSeries(t, ts, userID, 10, 1, mid, 50)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 30)

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()

	result := b.Build()

	if result.TotalRequests != 50 {
		t.Errorf("expected TotalRequests=50, got %d", result.TotalRequests)
	}
	if result.PrevRequests != 0 {
		t.Errorf("expected PrevRequests=0, got %d", result.PrevRequests)
	}
	// With no previous data, change should be 100%
	if result.RequestsChange != 100 {
		t.Errorf("expected RequestsChange=100, got %f", result.RequestsChange)
	}
	if result.RequestsSign != "+" {
		t.Errorf("expected RequestsSign='+', got %q", result.RequestsSign)
	}
	if result.RequestsColor != colorGreen {
		t.Errorf("expected RequestsColor=%q, got %q", colorGreen, result.RequestsColor)
	}
}

func TestUsageReportBuilderDecreaseShowsRed(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(5)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	// Current period: fewer requests than previous
	seedTimeSeries(t, ts, userID, 10, 1, mid, 30)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 20)

	// Previous period: more requests
	seedTimeSeries(t, ts, userID, 10, 1, from, 60)
	seedVerifyLogs(t, ts, userID, 10, 1, from, 40)

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()

	result := b.Build()

	if result.RequestsColor != colorRed {
		t.Errorf("expected RequestsColor=%q for decrease, got %q", colorRed, result.RequestsColor)
	}
	if result.VerifiesColor != colorRed {
		t.Errorf("expected VerifiesColor=%q for decrease, got %q", colorRed, result.VerifiesColor)
	}
	// Sign should be empty (negative)
	if result.RequestsSign != "" {
		t.Errorf("expected empty RequestsSign for decrease, got %q", result.RequestsSign)
	}
}

func TestUsageReportBuilderNoChangeShowsNeutral(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(6)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	// Same data both periods
	seedTimeSeries(t, ts, userID, 10, 1, mid, 50)
	seedTimeSeries(t, ts, userID, 10, 1, from, 50)

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()

	result := b.Build()

	if result.RequestsChange != 0 {
		t.Errorf("expected RequestsChange=0, got %f", result.RequestsChange)
	}
	if result.RequestsColor != colorNeutral {
		t.Errorf("expected RequestsColor=%q for no change, got %q", colorNeutral, result.RequestsColor)
	}
}

func TestUsageReportBuilderTopPropertiesWithCache(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(7)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	// Multiple properties
	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedTimeSeries(t, ts, userID, 20, 1, mid, 50)

	// Pre-populate cache with property data
	prop1 := &dbgen.Property{ID: 10, Name: "Main Site", Domain: "example.com"}
	prop2 := &dbgen.Property{ID: 20, Name: "Blog", Domain: "blog.example.com"}
	_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(10), prop1)
	_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(20), prop2)

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()
	b.BuildTopProperties()

	result := b.Build()

	if len(result.TopProperties) != 2 {
		t.Fatalf("expected 2 TopProperties, got %d", len(result.TopProperties))
	}

	// Properties should be ordered by current requests desc
	if result.TopProperties[0].Name != "Main Site" {
		t.Errorf("expected first property 'Main Site', got %q", result.TopProperties[0].Name)
	}
	if result.TopProperties[0].Count != 100 {
		t.Errorf("expected first property count=100, got %d", result.TopProperties[0].Count)
	}
	if result.TopProperties[1].Name != "Blog" {
		t.Errorf("expected second property 'Blog', got %q", result.TopProperties[1].Name)
	}
}

func TestUsageReportBuilderTopPropertiesLimitedTo5(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(8)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	// 7 properties
	for i := int32(1); i <= 7; i++ {
		seedTimeSeries(t, ts, userID, i*10, 1, mid, int(100-i*10))
		prop := &dbgen.Property{ID: i * 10, Name: "Prop", Domain: "example.com"}
		_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(i*10), prop)
	}

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()
	b.BuildTopProperties()

	result := b.Build()

	if len(result.TopProperties) > topPropertiesLimit {
		t.Errorf("expected at most %d TopProperties, got %d", topPropertiesLimit, len(result.TopProperties))
	}
}

func TestUsageReportBuilderBuildConvenienceFunction(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(9)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)

	result := BuildUsageReport(ctx, store, ts, userID, false, from, mid, now)

	if result.Period != "weekly" {
		t.Errorf("expected period 'weekly', got %q", result.Period)
	}
	if result.TotalRequests != 100 {
		t.Errorf("expected TotalRequests=100, got %d", result.TotalRequests)
	}
}

func TestUsageReportBuilderVerificationRate(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(10)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedVerifyLogs(t, ts, userID, 10, 1, mid, 50)

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()

	result := b.Build()

	expectedRate := 50.0
	if result.VerificationRate != expectedRate {
		t.Errorf("expected VerificationRate=%f, got %f", expectedRate, result.VerificationRate)
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

func TestChangeSign(t *testing.T) {
	if changeSign(10) != "+" {
		t.Error("expected '+' for positive")
	}
	if changeSign(0) != "" {
		t.Error("expected '' for zero")
	}
	if changeSign(-5) != "" {
		t.Error("expected '' for negative")
	}
}

func TestChangeColor(t *testing.T) {
	if changeColor(10) != colorGreen {
		t.Error("expected green for positive")
	}
	if changeColor(-5) != colorRed {
		t.Error("expected red for negative")
	}
	if changeColor(0) != colorNeutral {
		t.Error("expected neutral for zero")
	}
}

func TestUsageReportBuilderPropertyChangeColors(t *testing.T) {
	ctx := t.Context()
	ts := db.NewMemoryTimeSeries()
	store := createTestStore(t)
	userID := int32(11)

	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	// Prop 10: increase (current > previous)
	seedTimeSeries(t, ts, userID, 10, 1, mid, 100)
	seedTimeSeries(t, ts, userID, 10, 1, from, 50)

	// Prop 20: decrease (current < previous)
	seedTimeSeries(t, ts, userID, 20, 1, mid, 30)
	seedTimeSeries(t, ts, userID, 20, 1, from, 60)

	prop1 := &dbgen.Property{ID: 10, Name: "Up", Domain: "up.com"}
	prop2 := &dbgen.Property{ID: 20, Name: "Down", Domain: "down.com"}
	_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(10), prop1)
	_ = store.(*db.BusinessStore).Cache.Set(ctx, db.PropertyByIDCacheKey(20), prop2)

	b := NewUsageReportBuilder(ctx, store, ts, userID, false, from, mid, now)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()
	b.BuildTopProperties()

	result := b.Build()

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

	if upProp.ChangeColor != colorGreen {
		t.Errorf("expected green for increasing property, got %q", upProp.ChangeColor)
	}
	if downProp.ChangeColor != colorRed {
		t.Errorf("expected red for decreasing property, got %q", downProp.ChangeColor)
	}
}

func TestScheduleReportsJobDeduplication(t *testing.T) {
	job := &ScheduleReportsJob{}
	now := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)

	if job.isProcessedToday(1, now) {
		t.Error("expected user 1 NOT processed")
	}

	job.markProcessed(1, now)

	if !job.isProcessedToday(1, now) {
		t.Error("expected user 1 processed")
	}

	// Different day should not be processed
	tomorrow := now.AddDate(0, 0, 1)
	if job.isProcessedToday(1, tomorrow) {
		t.Error("expected user 1 NOT processed tomorrow")
	}

	// Different user should not be processed
	if job.isProcessedToday(2, now) {
		t.Error("expected user 2 NOT processed")
	}
}
