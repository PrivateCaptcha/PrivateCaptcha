package db

import (
	"context"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func TestSubscriptionLimitsCheckFormsLimit(t *testing.T) {
	planService := billing.NewPlanService(nil)
	plan := planService.GetInternalTrialPlan()
	_, priceID := plan.PriceIDs()

	subscr := &dbgen.Subscription{
		ID:                1,
		ExternalProductID: plan.ProductID(),
		ExternalPriceID:   priceID,
		Status:            billing.InternalStatusTrialing,
		Source:            dbgen.SubscriptionSourceInternal,
	}

	t.Run("BelowLimit", func(t *testing.T) {
		querier := &retrieveUserFormsQuerierStub{QuerierStub: &QuerierStub{}, count: int64(plan.FormsLimit(true) - 1)}
		store := NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}))
		limits := NewSubscriptionLimits(common.StageTest, store, planService)

		ok, extra, err := limits.CheckFormsLimit(context.Background(), 1, subscr)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !ok {
			t.Fatalf("expected below-limit account to be allowed, extra=%d", extra)
		}
		if extra != -1 {
			t.Fatalf("expected extra -1 one below limit, got %d", extra)
		}
	})

	t.Run("AtLimit", func(t *testing.T) {
		querier := &retrieveUserFormsQuerierStub{QuerierStub: &QuerierStub{}, count: int64(plan.FormsLimit(true))}
		store := NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}))
		limits := NewSubscriptionLimits(common.StageTest, store, planService)

		ok, extra, err := limits.CheckFormsLimit(context.Background(), 1, subscr)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if ok {
			t.Fatalf("expected at-limit account to be rejected")
		}
		if extra != 0 {
			t.Fatalf("expected extra 0 at exact limit, got %d", extra)
		}
	})
}
