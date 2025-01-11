package api

import (
	"context"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func gcDataTestSuite(ctx context.Context, property *dbgen.Property, deleter func(p *dbgen.Property) error, t *testing.T) {
	const requests = 1000
	tnow := time.Now()

	for i := 0; i < requests; i++ {
		s.levels.Difficulty(common.RandomFingerprint(), property, 0 /*user level*/, tnow.Add(time.Duration(i)*10*time.Second))
	}

	// we need to wait for the timeout in the ProcessAccessLog()
	time.Sleep(1 * time.Second)

	request := &common.BackfillRequest{
		OrgID:      property.OrgID.Int32,
		UserID:     property.OrgOwnerID.Int32,
		PropertyID: property.ID,
	}
	from := tnow

	stats, err := timeSeries.ReadPropertyStats(ctx, request, from)
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

	err = job.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}

	stats, err = timeSeries.ReadPropertyStats(ctx, request, from)
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

	ctx := context.TODO()

	_, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, err := db_test.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}

	gcDataTestSuite(ctx, property, func(p *dbgen.Property) error {
		return store.SoftDeleteProperty(ctx, p.ID, p.OrgOwnerID.Int32)
	}, t)
}

func TestGCOrganizationData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()

	_, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, err := db_test.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}

	gcDataTestSuite(ctx, property, func(p *dbgen.Property) error {
		return store.SoftDeleteOrganization(ctx, p.OrgID.Int32, p.OrgOwnerID.Int32)
	}, t)
}

func TestGCUserData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()

	_, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, err := db_test.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}

	gcDataTestSuite(ctx, property, func(p *dbgen.Property) error {
		return store.SoftDeleteUser(ctx, p.OrgOwnerID.Int32)
	}, t)
}
