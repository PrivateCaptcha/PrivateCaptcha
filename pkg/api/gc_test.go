package api

import (
	"context"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func gcPropertyDataTestSuite(ctx context.Context, property *dbgen.Property, deleter func(p *dbgen.Property) error, t *testing.T) {
	t.Helper()

	const requests = 1000
	tnow := time.Now()
	dp := difficulty.NewDBProperty(property)

	for i := 0; i < requests; i++ {
		server.Levels.Difficulty(ctx, common.RandomFingerprint(), dp, tnow.Add(time.Duration(i)*10*time.Second))
	}

	// we need to wait for the timeout in the ProcessAccessLog()
	time.Sleep(1 * time.Second)

	request := &common.BackfillRequest{
		OrgID:      property.OrgID.Int32,
		UserID:     property.OrgOwnerID.Int32,
		PropertyID: property.ID,
	}
	from := tnow

	stats, err := timeSeries.RetrievePropertyStatsSince(ctx, request, from)
	if err != nil {
		t.Fatal(err)
	}

	if len(stats) == 0 {
		t.Error("There are no stats found")
	}

	err = deleter(property)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	job := &maintenance.GarbageCollectDataJob{
		Age:        0,
		BusinessDB: store,
		TimeSeries: timeSeries,
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Fatal(err)
	}

	stats, err = timeSeries.RetrievePropertyStatsSince(ctx, request, from)
	if err != nil {
		t.Fatal(err)
	}

	nonZeroStatsCount := 0
	for _, s := range stats {
		if s.Count > 0 {
			nonZeroStatsCount++
		}
	}

	if nonZeroStatsCount > 0 {
		t.Errorf("There are %v stats found", nonZeroStatsCount)
	}
}

func gcFormDataTestSuite(ctx context.Context, form *dbgen.Form, deleter func(f *dbgen.Form) error, t *testing.T) {
	t.Helper()

	const requests = 400

	for i := 0; i < requests; i++ {
		server.addFormSubmitRecord(ctx, form, int8(i%2))
	}

	// we need to wait for the timeout in the ProcessAccessLog()
	time.Sleep(1 * time.Second)

	stats, err := timeSeries.RetrieveFormStatsByPeriod(ctx, form.OrgID.Int32, form.ID, common.TimePeriodToday)
	if err != nil {
		t.Fatal(err)
	}

	if len(stats) == 0 {
		t.Error("There are no stats found")
	}

	err = deleter(form)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	job := &maintenance.GarbageCollectDataJob{
		Age:        0,
		BusinessDB: store,
		TimeSeries: timeSeries,
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Fatal(err)
	}

	if err := timeSeries.DropCache(ctx, "form_stats_period"); err != nil {
		t.Error(err)
	}

	stats, err = timeSeries.RetrieveFormStatsByPeriod(ctx, form.OrgID.Int32, form.ID, common.TimePeriodToday)
	if err != nil {
		t.Fatal(err)
	}

	nonZeroStatsCount := 0
	nonZeroStatsSum := 0
	for _, s := range stats {
		if (s.SuccessCount > 0) || (s.FailureCount > 0) {
			nonZeroStatsCount++
			nonZeroStatsSum += s.SuccessCount + s.FailureCount
		}
	}

	if nonZeroStatsCount > 0 {
		t.Errorf("There are %v stats found (sum %v)", nonZeroStatsCount, nonZeroStatsSum)
	}
}

func TestGCPropertyData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, err := db_test.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}

	gcPropertyDataTestSuite(ctx, property, func(p *dbgen.Property) error {
		_, err := store.Impl().SoftDeleteProperty(ctx, p, org, user)
		return err
	}, t)
}

func TestGCPropertyOrgData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, err := db_test.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}

	gcPropertyDataTestSuite(ctx, property, func(p *dbgen.Property) error {
		_, err := store.Impl().SoftDeleteOrganization(ctx, org, user)
		return err
	}, t)
}

func TestGCUserData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, err := db_test.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}

	gcPropertyDataTestSuite(ctx, property, func(p *dbgen.Property) error {
		_, err := store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
			event, err := impl.SoftDeleteUser(ctx, user)
			return []*common.AuditLogEvent{event}, err
		})
		return err
	}, t)
}

func TestGCFormData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	form, _, _, err := store.Impl().CreateNewForm(ctx, db_test.CreateNewPropertyParams(user.ID, "gc-form.example.com"), &dbgen.CreateFormParams{
		Name:              t.Name(),
		URL:               "https://example.com/submit",
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		t.Fatal(err)
	}

	gcFormDataTestSuite(ctx, form, func(f *dbgen.Form) error {
		_, err := store.Pool.Exec(ctx, "UPDATE backend.forms SET deleted_at = NOW() - INTERVAL '1 hour' WHERE id = $1", form.ID)
		return err
	}, t)
}

func TestGCFormOrgData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	form, _, _, err := store.Impl().CreateNewForm(ctx, db_test.CreateNewPropertyParams(user.ID, "gc-form.example.com"), &dbgen.CreateFormParams{
		Name:              t.Name(),
		URL:               "https://example.com/submit",
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		t.Fatal(err)
	}

	gcFormDataTestSuite(ctx, form, func(f *dbgen.Form) error {
		tnow := time.Now().UTC()
		timeFrom := tnow.AddDate(0, 0, -1).Truncate(1 * time.Hour)
		strCacheKey := timeFrom.Format(time.DateTime)
		cacheKey := db.FormStatsCacheKey(f.ID, strCacheKey)
		_ = cache.Delete(ctx, cacheKey)

		_, err := store.Impl().SoftDeleteOrganization(ctx, org, user)
		return err
	}, t)
}

func TestGCFormUserData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	form, _, _, err := store.Impl().CreateNewForm(ctx, db_test.CreateNewPropertyParams(user.ID, "gc-form.example.com"), &dbgen.CreateFormParams{
		Name:              t.Name(),
		URL:               "https://example.com/submit",
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		t.Fatal(err)
	}

	gcFormDataTestSuite(ctx, form, func(f *dbgen.Form) error {
		tnow := time.Now().UTC()
		timeFrom := tnow.AddDate(0, 0, -1).Truncate(1 * time.Hour)
		strCacheKey := timeFrom.Format(time.DateTime)
		cacheKey := db.FormStatsCacheKey(f.ID, strCacheKey)
		_ = cache.Delete(ctx, cacheKey)

		_, err := store.Impl().SoftDeleteUser(ctx, user)
		return err
	}, t)
}
