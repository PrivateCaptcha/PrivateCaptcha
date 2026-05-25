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

func gcDataTestSuite(ctx context.Context, property *dbgen.Property, deleter func(p *dbgen.Property) error, t *testing.T) {
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

	if len(stats) > 0 {
		t.Errorf("There are %v stats found", len(stats))
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

	gcDataTestSuite(ctx, property, func(p *dbgen.Property) error {
		_, err := store.Impl().SoftDeleteProperty(ctx, p, org, user)
		return err
	}, t)
}

func TestGCOrganizationData(t *testing.T) {
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

	gcDataTestSuite(ctx, property, func(p *dbgen.Property) error {
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

	gcDataTestSuite(ctx, property, func(p *dbgen.Property) error {
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

	now := time.Now()
	if err := timeSeries.WriteFormSubmitBatch(ctx, []*common.FormSubmitRecord{
		{UserID: user.ID, OrgID: org.ID, PropertyID: form.PropertyID, FormID: form.ID, Timestamp: now, Status: 0},
		{UserID: user.ID, OrgID: org.ID, PropertyID: form.PropertyID, FormID: form.ID, Timestamp: now, Status: 1},
	}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	stats, err := timeSeries.RetrieveFormStatsByPeriod(ctx, org.ID, form.ID, common.TimePeriodToday)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Fatal("There are no form stats found")
	}

	if _, err := store.Pool.Exec(ctx, "UPDATE backend.forms SET deleted_at = NOW() - INTERVAL '1 hour' WHERE id = $1", form.ID); err != nil {
		t.Fatal(err)
	}

	job := &maintenance.GarbageCollectDataJob{
		Age:        0,
		BusinessDB: store,
		TimeSeries: timeSeries,
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	stats, err = timeSeries.RetrieveFormStatsByPeriod(ctx, org.ID, form.ID, common.TimePeriodToday)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) > 0 {
		t.Errorf("There are %v form stats found", len(stats))
	}

	var count int
	if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.forms WHERE id = $1", form.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("Expected hard-deleted form, got count %d", count)
	}
}
