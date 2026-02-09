package db

import (
	"context"
	"errors"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type SubscriptionLimits interface {
	CheckOrgsLimit(ctx context.Context, userID int32, subscr *dbgen.Subscription) (bool, int, error)
	CheckOrgMembersLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (bool, int, error)
	CheckPropertiesLimit(ctx context.Context, userID int32, subscr *dbgen.Subscription) (bool, int, error)
	RequestsLimit(ctx context.Context, subscr *dbgen.Subscription) (int64, error)
	PropertiesLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error)
	OrgsLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error)
}

var (
	ErrNoActiveSubscription = errors.New("subscription is not active or nil")
)

type SubscriptionLimitsImpl struct {
	Stage       string
	store       Implementor
	planService billing.PlanService
}

func NewSubscriptionLimits(stage string, store Implementor, planService billing.PlanService) *SubscriptionLimitsImpl {
	return &SubscriptionLimitsImpl{
		Stage:       stage,
		store:       store,
		planService: planService,
	}
}

var _ SubscriptionLimits = (*SubscriptionLimitsImpl)(nil)

func (sl *SubscriptionLimitsImpl) CheckOrgsLimit(ctx context.Context, userID int32, subscr *dbgen.Subscription) (bool, int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return false, 0, ErrNoActiveSubscription
	}

	isInternalSubscription := IsInternalSubscription(subscr.Source)
	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID,
			"priceID", subscr.ExternalPriceID, "productID", subscr.ExternalProductID, common.ErrAttr(err))
		return false, 0, err
	}

	count := 0
	// NOTE: this should be freshly cached as we should have just rendered the dashboard
	if orgs, err := sl.store.Impl().RetrieveUserOrganizations(ctx, userID); err == nil {
		for _, org := range orgs {
			if org.Level == dbgen.AccessLevelOwner {
				count++
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve user orgs", "userID", userID, common.ErrAttr(err))
		return false, 0, err
	}

	ok := (plan.OrgsLimit() == 0) || (count < plan.OrgsLimit())

	return ok, count - plan.OrgsLimit(), nil
}

func (sl *SubscriptionLimitsImpl) CheckOrgMembersLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (bool, int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return false, 0, ErrNoActiveSubscription
	}

	isInternalSubscription := IsInternalSubscription(subscr.Source)
	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return false, 0, err
	}

	members, err := sl.store.Impl().RetrieveOrganizationUsers(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return false, 0, err
	}

	ok := (plan.OrgMembersLimit() == 0) || (len(members) < plan.OrgMembersLimit())

	return ok, len(members) - plan.OrgMembersLimit(), nil
}

func (sl *SubscriptionLimitsImpl) CheckPropertiesLimit(ctx context.Context, userID int32, subscr *dbgen.Subscription) (bool, int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return false, 0, ErrNoActiveSubscription
	}

	isInternalSubscription := IsInternalSubscription(subscr.Source)
	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return false, 0, err
	}

	count, err := sl.store.Impl().RetrieveUserPropertiesCount(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties count", "userID", userID, common.ErrAttr(err))
		return false, 0, err
	}

	ok := (plan.PropertiesLimit() == 0) || (count < int64(plan.PropertiesLimit()))

	return ok, int(count) - plan.PropertiesLimit(), nil
}

func (sl *SubscriptionLimitsImpl) RequestsLimit(ctx context.Context, subscr *dbgen.Subscription) (int64, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return 0, ErrNoActiveSubscription
	}

	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage,
		IsInternalSubscription(subscr.Source))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan", "productID", subscr.ExternalProductID, "priceID", subscr.ExternalPriceID, common.ErrAttr(err))
		return 0, err

	}
	return plan.RequestsLimit(), nil
}

func (sl *SubscriptionLimitsImpl) PropertiesLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return 0, ErrNoActiveSubscription
	}

	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage,
		IsInternalSubscription(subscr.Source))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan", "productID", subscr.ExternalProductID, "priceID", subscr.ExternalPriceID, common.ErrAttr(err))
		return 0, err

	}
	return plan.PropertiesLimit(), nil
}

func (sl *SubscriptionLimitsImpl) OrgsLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return 0, ErrNoActiveSubscription
	}

	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage,
		IsInternalSubscription(subscr.Source))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan", "productID", subscr.ExternalProductID, "priceID", subscr.ExternalPriceID, common.ErrAttr(err))
		return 0, err

	}
	return plan.OrgsLimit(), nil
}

type StubSubscriptionLimits struct{}

func (StubSubscriptionLimits) CheckOrgsLimit(ctx context.Context, userID int32, subscr *dbgen.Subscription) (_ bool, _ int, _ error) {
	return true, 0, nil
}
func (StubSubscriptionLimits) CheckOrgMembersLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (_ bool, _ int, _ error) {
	return true, 0, nil
}
func (StubSubscriptionLimits) CheckPropertiesLimit(ctx context.Context, userID int32, subscr *dbgen.Subscription) (_ bool, _ int, _ error) {
	return true, 0, nil
}
func (StubSubscriptionLimits) RequestsLimit(ctx context.Context, subscr *dbgen.Subscription) (int64, error) {
	return 0, nil
}
func (StubSubscriptionLimits) PropertiesLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error) {
	return 0, nil
}
func (StubSubscriptionLimits) OrgsLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error) {
	return 0, nil
}

var _ SubscriptionLimits = (*StubSubscriptionLimits)(nil)
