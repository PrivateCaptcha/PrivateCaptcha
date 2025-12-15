package portal

import (
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func TestExpireInternalTrials(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	// this has to reflect what we actually use instead of db_helpers where we fill external IDs too
	subscrParams := createInternalTrial(testPlan, server.PlanService.ActiveTrialStatus())
	user, _, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name(), subscrParams)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	subscr, err := store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		t.Fatal(err)
	}

	if subscr.Status != server.PlanService.ActiveTrialStatus() {
		t.Errorf("Invalid subscription status: %v", subscr.Status)
	}

	job := &maintenance.ExpireInternalTrialsJob{
		PastInterval: 0,
		Age:          -(subscr.TrialEndsAt.Time.Sub(subscr.CreatedAt.Time) + 1*time.Minute),
		BusinessDB:   store,
		PlanService:  server.PlanService,
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	cacheKey := db.SubscriptionCacheKey(subscr.ID)
	cache.Delete(ctx, cacheKey)

	subscr, err = store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		t.Fatal(err)
	}

	if subscr.Status != server.PlanService.ExpiredTrialStatus() {
		t.Errorf("Invalid subscription status: %v", subscr.Status)
	}
}
