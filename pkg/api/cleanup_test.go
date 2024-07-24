package api

import (
	"context"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func TestCleanupPropertyData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	tnow := time.Now()
	const requests = 1000

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, err := db_test.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < requests; i++ {
		s.levels.Difficulty(common.RandomFingerprint(), property, tnow.Add(time.Duration(i)*10*time.Second))
	}

	// we need to wait for the timeout in the ProcessAccessLog()
	time.Sleep(1 * time.Second)

	request := &common.BackfillRequest{
		OrgID:      org.ID,
		UserID:     user.ID,
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

	err = store.SoftDeleteProperty(ctx, property.ID, org.ID)
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
