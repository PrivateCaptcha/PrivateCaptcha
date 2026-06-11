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
	CheckFormsLimit(ctx context.Context, userID int32, subscr *dbgen.Subscription) (bool, int, error)
	CheckOrgRulesLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (bool, int, error)
	CheckPropertyRulesLimit(ctx context.Context, propertyID int32, subscr *dbgen.Subscription) (bool, int, error)
	RequestsLimit(ctx context.Context, subscr *dbgen.Subscription) (int64, error)
	PropertiesLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error)
	OrgsLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error)
	FormsLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error)
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

	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	ok := (plan.OrgsLimit(isTrialing) == 0) || (count < plan.OrgsLimit(isTrialing))

	return ok, count - plan.OrgsLimit(isTrialing), nil
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

	members, err := sl.store.Impl().RetrieveOrganizationUsersWithEmailInvites(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return false, 0, err
	}

	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	ok := (plan.OrgMembersLimit(isTrialing) == 0) || (len(members) < plan.OrgMembersLimit(isTrialing))

	return ok, len(members) - plan.OrgMembersLimit(isTrialing), nil
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

	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	ok := (plan.PropertiesLimit(isTrialing) == 0) || (count < int64(plan.PropertiesLimit(isTrialing)))

	return ok, int(count) - plan.PropertiesLimit(isTrialing), nil
}

func (sl *SubscriptionLimitsImpl) CheckFormsLimit(ctx context.Context, userID int32, subscr *dbgen.Subscription) (bool, int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return false, 0, ErrNoActiveSubscription
	}

	isInternalSubscription := IsInternalSubscription(subscr.Source)
	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return false, 0, err
	}

	count, err := sl.store.Impl().RetrieveUserFormsCount(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve forms count", "userID", userID, common.ErrAttr(err))
		return false, 0, err
	}

	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	ok := (plan.FormsLimit(isTrialing) == 0) || (count < int64(plan.FormsLimit(isTrialing)))

	return ok, int(count) - plan.FormsLimit(isTrialing), nil
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
	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	return plan.RequestsLimit(isTrialing), nil
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
	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	return plan.PropertiesLimit(isTrialing), nil
}

func (sl *SubscriptionLimitsImpl) FormsLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return 0, ErrNoActiveSubscription
	}

	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage,
		IsInternalSubscription(subscr.Source))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan", "productID", subscr.ExternalProductID, "priceID", subscr.ExternalPriceID, common.ErrAttr(err))
		return 0, err

	}
	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	return plan.FormsLimit(isTrialing), nil
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
	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	return plan.OrgsLimit(isTrialing), nil
}

func (sl *SubscriptionLimitsImpl) CheckOrgRulesLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (bool, int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return false, 0, ErrNoActiveSubscription
	}

	isInternalSubscription := IsInternalSubscription(subscr.Source)
	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return false, 0, err
	}

	// Retrieve rules via cached method
	rules, err := sl.store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{orgID: 0})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org rules", "orgID", orgID, common.ErrAttr(err))
		return false, 0, err
	}

	count := 0
	if orgRules, ok := rules[orgID]; ok {
		count = len(orgRules)
	}

	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	// for rules, 0 means "zero" and not unlimited (like for orgs/properties)
	ok := /*(plan.OrgRulesLimit() == 0) || */ (count < plan.OrgRulesLimit(isTrialing))

	return ok, count - plan.OrgRulesLimit(isTrialing), nil
}

func (sl *SubscriptionLimitsImpl) CheckPropertyRulesLimit(ctx context.Context, propertyID int32, subscr *dbgen.Subscription) (bool, int, error) {
	if (subscr == nil) || !sl.planService.IsSubscriptionActive(subscr.Status) {
		return false, 0, ErrNoActiveSubscription
	}

	isInternalSubscription := IsInternalSubscription(subscr.Source)
	plan, err := sl.planService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, sl.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return false, 0, err
	}

	// Retrieve rules via cached method
	rules, err := sl.store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, map[int32]uint{propertyID: 0})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve property rules", "propertyID", propertyID, common.ErrAttr(err))
		return false, 0, err
	}

	count := 0
	if propRules, ok := rules[propertyID]; ok {
		count = len(propRules)
	}

	isTrialing := sl.planService.IsSubscriptionTrialing(subscr.Status)
	// for rules, 0 means "zero" and not unlimited (like for orgs/properties)
	ok := /*(plan.PropertyRulesLimit() == 0) ||*/ (count < plan.PropertyRulesLimit(isTrialing))

	return ok, count - plan.PropertyRulesLimit(isTrialing), nil
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
func (StubSubscriptionLimits) CheckFormsLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (_ bool, _ int, _ error) {
	return true, 0, nil
}
func (StubSubscriptionLimits) CheckOrgRulesLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (_ bool, _ int, _ error) {
	return true, 0, nil
}
func (StubSubscriptionLimits) CheckPropertyRulesLimit(ctx context.Context, propertyID int32, subscr *dbgen.Subscription) (_ bool, _ int, _ error) {
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
func (StubSubscriptionLimits) FormsLimit(ctx context.Context, subscr *dbgen.Subscription) (int, error) {
	return 0, nil
}

var _ SubscriptionLimits = (*StubSubscriptionLimits)(nil)
