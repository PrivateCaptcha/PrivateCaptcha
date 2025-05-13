package billing

import (
	"context"
	"errors"
	"sync"
)

const (
	// do NOT use
	InternalStatusTrialing = "pc-trial"
)

type Prices map[string]int

type Plan struct {
	Name                 string
	PaddleProductID      string
	PaddlePriceIDMonthly string
	PaddlePriceIDYearly  string
	TrialDays            int
	PriceMonthly         int
	PriceYearly          int
	Version              int
	RequestsLimit        int64
	OrgsLimit            int
	PropertiesLimit      int
	OrgMembersLimit      int
	ThrottleLimit        int64
	APIRequestsPerSecond float64
}

func (p *Plan) IsValid() bool {
	return len(p.Name) > 0 &&
		len(p.PaddleProductID) > 0 &&
		len(p.PaddlePriceIDYearly) > 0 &&
		p.PriceMonthly > 0 &&
		p.PriceYearly > 0 &&
		p.RequestsLimit > 0
}

func (p *Plan) IsDowngradeFor(other *Plan) bool {
	if (p == nil) || (other == nil) {
		return false
	}

	if !p.IsValid() || !other.IsValid() {
		return false
	}

	return (p.RequestsLimit < other.RequestsLimit) &&
		(p.PriceMonthly < other.PriceMonthly) &&
		(p.PriceYearly < other.PriceYearly)
}

func (p *Plan) IsLegitUsage(requestsCount int64) bool {
	return requestsCount <= p.RequestsLimit
}

func (p *Plan) ShouldBeThrottled(requestsCount int64) bool {
	if (p.RequestsLimit <= 0) || (p.ThrottleLimit <= 0) {
		return false
	}

	return (requestsCount > p.RequestsLimit) && (requestsCount > p.ThrottleLimit)
}

func (plan *Plan) IsYearly(priceID string) bool {
	return plan.PaddlePriceIDYearly == priceID
}

const (
	defaultOrgLimit        = 10
	defaultPropertiesLimit = 50
	defaultTrialDays       = 14
	defaultOrgMembersLimit = 5
	defaultAPIKeyRequests  = 1.0
	version1               = 1
)

var (
	ErrUnknownProductID = errors.New("unknown product ID")
	ErrUnknownPriceID   = errors.New("unknown price ID")
	ErrInvalidArgument  = errors.New("invalid argument")
)

type PlanService interface {
	FindPlanEx(paddleProductID string, paddlePriceID string, stage string, internal bool) (*Plan, error)
	IsSubscriptionActive(status string) bool
	TrialStatus() string
	CancelSubscription(ctx context.Context, sid string) error
}

type CorePlanService struct {
	Lock          sync.RWMutex
	StagePlans    map[string][]*Plan
	InternalPlans []*Plan
}

var (
	internalTrialPlan = &Plan{
		Name:                 "Internal Trial",
		PaddleProductID:      "pctrial_CGK710ObXUu3hnErY87KMx4gnt3",
		PaddlePriceIDMonthly: "",
		PaddlePriceIDYearly:  "pctrial_qD6rwF1UomfdkgbOjaepoDn0RxX",
		TrialDays:            14,
		PriceMonthly:         0,
		PriceYearly:          0,
		Version:              version1,
		RequestsLimit:        1_000,
		OrgsLimit:            2,
		OrgMembersLimit:      2,
		PropertiesLimit:      10,
		ThrottleLimit:        2_000,
		APIRequestsPerSecond: 10,
	}

	internalAdminPlan = &Plan{
		Name:                 "Internal Admin",
		PaddleProductID:      "pcadmin_zgEsl1kNmYmk55XDkAsbgOflGQFU2NBN",
		PaddlePriceIDMonthly: "",
		PaddlePriceIDYearly:  "pcadmin_pQ9DX6GHn1iik3BqsLQJbnHLw1dU91J1",
		TrialDays:            100 * 365,
		PriceMonthly:         0,
		PriceYearly:          0,
		Version:              version1,
		RequestsLimit:        1_000_000,
		OrgsLimit:            1_00,
		PropertiesLimit:      1_000,
		OrgMembersLimit:      100,
		ThrottleLimit:        2_000_000,
		APIRequestsPerSecond: 100,
	}
)

func NewPlanService(stagePlans map[string][]*Plan) *CorePlanService {
	if stagePlans == nil {
		stagePlans = map[string][]*Plan{}
	}

	return &CorePlanService{
		StagePlans: stagePlans,
		InternalPlans: []*Plan{
			internalTrialPlan,
			internalAdminPlan,
		},
	}
}

func GetInternalAdminPlan() *Plan {
	return internalAdminPlan
}

func GetInternalTrialPlan() *Plan {
	return internalTrialPlan
}

func (s *CorePlanService) FindPlanEx(paddleProductID string, paddlePriceID string, stage string, internal bool) (*Plan, error) {
	if (stage == "") || (paddleProductID == "") || (paddlePriceID == "") {
		return nil, ErrInvalidArgument
	}

	s.Lock.RLock()
	defer s.Lock.RUnlock()

	var plans []*Plan
	if internal {
		plans = s.InternalPlans
	} else {
		plans = s.StagePlans[stage]
	}

	for _, p := range plans {
		if (p.PaddleProductID == paddleProductID) &&
			((p.PaddlePriceIDMonthly == paddlePriceID) || (p.PaddlePriceIDYearly == paddlePriceID)) {
			return p, nil
		}
	}

	return nil, ErrUnknownProductID
}

func (s *CorePlanService) TrialStatus() string {
	return InternalStatusTrialing
}

func (s *CorePlanService) CancelSubscription(ctx context.Context, sid string) error {
	// BUMP
	return nil
}

func (s *CorePlanService) IsSubscriptionActive(status string) bool {
	switch status {
	case InternalStatusTrialing:
		return true
	default:
		return false
	}
}
