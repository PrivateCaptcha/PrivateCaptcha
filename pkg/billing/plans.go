package billing

import (
	"context"
	"errors"
	"sync"
)

const (
	// do NOT use
	InternalStatusTrialing = "pc-trial-active"
	InternalStatusExpired  = "pc-trial-expired"
)

type Prices map[string]int

type basePlan struct {
	name                 string
	productID            string
	priceIDMonthly       string
	priceIDYearly        string
	trialDays            int
	priceMonthly         int
	priceYearly          int
	orgsLimit            int
	rulesLimit           int
	version              int
	requestsLimit        int64
	throttleLimit        int64
	apiRequestsPerSecond float64
}

func (p *basePlan) IsValid() bool {
	return len(p.name) > 0 &&
		len(p.productID) > 0 &&
		len(p.priceIDYearly) > 0 &&
		p.priceMonthly > 0 &&
		p.priceYearly > 0 &&
		p.requestsLimit > 0
}

func (plan *basePlan) Equals(productID string, priceID string) bool {
	return (plan.productID == productID) &&
		((plan.priceIDMonthly == priceID) || (plan.priceIDYearly == priceID))
}

func (p *basePlan) Name() string                  { return p.name }
func (p *basePlan) ProductID() string             { return p.productID }
func (p *basePlan) PriceIDs() (string, string)    { return p.priceIDMonthly, p.priceIDYearly }
func (p *basePlan) TrialDays() int                { return p.trialDays }
func (p *basePlan) RequestsLimit() int64          { return p.requestsLimit }
func (p *basePlan) APIRequestsPerSecond() float64 { return p.apiRequestsPerSecond }
func (p *basePlan) PropertiesLimit() int          { return 50 }
func (p *basePlan) OrgsLimit() int                { return p.orgsLimit }
func (p *basePlan) OrgMembersLimit() int          { return 10 }
func (p *basePlan) OrgRulesLimit() int            { return p.rulesLimit }
func (p *basePlan) PropertyRulesLimit() int       { return p.rulesLimit }

const (
	version1 = 1
)

var (
	ErrUnknownProductID = errors.New("unknown product ID")
	ErrUnknownPriceID   = errors.New("unknown price ID")
	ErrInvalidArgument  = errors.New("invalid argument")
)

type Plan interface {
	Name() string
	ProductID() string
	PriceIDs() (string, string)
	IsValid() bool
	Equals(productID string, priceID string) bool
	TrialDays() int
	RequestsLimit() int64
	PropertiesLimit() int
	OrgsLimit() int
	OrgMembersLimit() int
	OrgRulesLimit() int
	PropertyRulesLimit() int
	APIRequestsPerSecond() float64
}

type PlanService interface {
	FindPlan(productID string, priceID string, stage string, internal bool) (Plan, error)
	IsSubscriptionActive(status string) bool
	ActiveTrialStatus() string
	ExpiredTrialStatus() string
	CancelSubscription(ctx context.Context, sid string) error
	GetInternalAdminPlan() Plan
	GetInternalTrialPlan() Plan
}

type CorePlanService struct {
	Lock          sync.RWMutex
	StagePlans    map[string][]Plan
	InternalPlans []Plan
}

var (
	internalTrialPlan = &basePlan{
		name:                 "Internal Trial",
		productID:            "pctrial_CGK710ObXUu3hnErY87KMx4gnt3",
		priceIDMonthly:       "",
		priceIDYearly:        "pctrial_qD6rwF1UomfdkgbOjaepoDn0RxX",
		trialDays:            14,
		priceMonthly:         0,
		priceYearly:          0,
		orgsLimit:            1,
		rulesLimit:           10,
		version:              version1,
		requestsLimit:        1_000,
		throttleLimit:        2_000,
		apiRequestsPerSecond: 10,
	}

	internalAdminPlan = &basePlan{
		name:                 "Internal Admin",
		productID:            "pcadmin_zgEsl1kNmYmk55XDkAsbgOflGQFU2NBN",
		priceIDMonthly:       "",
		priceIDYearly:        "pcadmin_pQ9DX6GHn1iik3BqsLQJbnHLw1dU91J1",
		trialDays:            100 * 365,
		priceMonthly:         0,
		priceYearly:          0,
		orgsLimit:            adminOrgsLimit,
		rulesLimit:           0,
		version:              version1,
		requestsLimit:        1_000_000,
		throttleLimit:        2_000_000,
		apiRequestsPerSecond: 100,
	}
)

func NewPlanService(stagePlans map[string][]Plan) *CorePlanService {
	if stagePlans == nil {
		stagePlans = map[string][]Plan{}
	}

	return &CorePlanService{
		StagePlans: stagePlans,
		InternalPlans: []Plan{
			internalTrialPlan,
			internalAdminPlan,
		},
	}
}

func (s *CorePlanService) GetInternalAdminPlan() Plan {
	return internalAdminPlan
}

func (s *CorePlanService) GetInternalTrialPlan() Plan {
	return internalTrialPlan
}

func (s *CorePlanService) FindPlan(productID string, priceID string, stage string, internal bool) (Plan, error) {
	if (stage == "") || (productID == "") || (priceID == "") {
		return nil, ErrInvalidArgument
	}

	s.Lock.RLock()
	defer s.Lock.RUnlock()

	var plans []Plan
	if internal {
		plans = s.InternalPlans
	} else {
		plans = s.StagePlans[stage]
	}

	for _, p := range plans {
		if p.Equals(productID, priceID) {
			return p, nil
		}
	}

	return nil, ErrUnknownProductID
}

func (s *CorePlanService) ActiveTrialStatus() string {
	return InternalStatusTrialing
}

func (s *CorePlanService) ExpiredTrialStatus() string {
	return InternalStatusExpired
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
