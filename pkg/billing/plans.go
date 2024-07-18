package billing

import (
	"errors"
	"sync"

	"github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type Plan struct {
	Name                 string
	PaddleProductID      string
	PaddlePriceIDMonthly string
	PaddlePriceIDYearly  string
	PriceMonthly         int
	PriceYearly          int
	Version              int
	RequestsLimit        int64
	OrgsLimit            int
	PropertiesLimit      int
	ThrottleLimit        int64
}

func (p *Plan) IsValid() bool {
	return len(p.Name) > 0 &&
		len(p.PaddleProductID) > 0 &&
		len(p.PaddlePriceIDYearly) > 0 &&
		len(p.PaddlePriceIDMonthly) > 0 &&
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

const (
	defaultOrgLimit        = 10
	defaultPropertiesLimit = 50
	version1               = 1
)

var (
	lock                sync.RWMutex
	ErrUnknownProductID = errors.New("unknown product ID")
	ErrUnknownPriceID   = errors.New("unknown price ID")

	devPlans = []*Plan{
		{
			Name:                 "Private Captcha 1K",
			PaddleProductID:      "pro_01j0379m2fed2nrf4hb2gmb6gh",
			PaddlePriceIDMonthly: "pri_01j037bk2bwtpryzqzafg1kyxk",
			PaddlePriceIDYearly:  "pri_01j037cm0ny17tk8t9d7qksntq",
			PriceMonthly:         9,
			PriceYearly:          9 * 11,
			RequestsLimit:        1_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        5_000, // next plan's limit
		},
		{
			Name:                 "Private Captcha 5K",
			PaddleProductID:      "pro_01j03v1tmp3gm1mmvhg357cg8v",
			PaddlePriceIDMonthly: "pri_01j03v3ne7q50zfndesjrp74k8",
			PaddlePriceIDYearly:  "pri_01j03v4vkw4jhe3a4ahpcp888c",
			PriceMonthly:         19,
			PriceYearly:          19 * 11,
			RequestsLimit:        5_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        10_000, // next plan's limit
		},
		{
			Name:                 "Private Captcha 10K",
			PaddleProductID:      "pro_01j03v5bpwqfk5gs09wm6cqgxp",
			PaddlePriceIDMonthly: "pri_01j03v5xqapmx8srxw3a7tz2ny",
			PaddlePriceIDYearly:  "pri_01j03v6jb4mzkdtxp2xrgb1vdt",
			PriceMonthly:         29,
			PriceYearly:          29 * 11,
			RequestsLimit:        10_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        25_000, // next plan's limit
		},
		{
			Name:                 "Private Captcha 25K",
			PaddleProductID:      "pro_01j03v84qgmc3qe61v3302k6wz",
			PaddlePriceIDMonthly: "pri_01j03v8m86qtfdhcy6hap60xe8",
			PaddlePriceIDYearly:  "pri_01j03v9b410k9pd09xkev1yw28",
			PriceMonthly:         49,
			PriceYearly:          49 * 11,
			RequestsLimit:        25_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        50_000, // next plan's limit
		},
		{
			Name:                 "Private Captcha 50K",
			PaddleProductID:      "pro_01j03v9zzanph7e0f0j89p1bqz",
			PaddlePriceIDMonthly: "pri_01j03vagbx7ew49yz8ybgjch78",
			PaddlePriceIDYearly:  "pri_01j03vb735t6j3z619ae2ar2n3",
			PriceMonthly:         79,
			PriceYearly:          79 * 11,
			RequestsLimit:        50_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        100_000, // next plan's limit
		},
		{
			Name:                 "Private Captcha 100K",
			PaddleProductID:      "pro_01j03vcaxkcf55s5n34dz41nrr",
			PaddlePriceIDMonthly: "pri_01j03vyfv2nx04s31dcs3bd54z",
			PaddlePriceIDYearly:  "pri_01j03vz7gkxb755c31sacg78gr",
			PriceMonthly:         129,
			PriceYearly:          129 * 11,
			RequestsLimit:        100_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        200_000, // x2
		},
	}

	prodPlans = []*Plan{}

	TestPlans = []*Plan{
		{
			Name:                 "Private Captcha Test",
			PaddleProductID:      "123456",
			PaddlePriceIDYearly:  "",
			PaddlePriceIDMonthly: "",
			RequestsLimit:        10_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        20_000,
		},
	}

	stagePlans = map[string][]*Plan{
		common.StageProd: prodPlans,
		common.StageDev:  devPlans,
		common.StageTest: TestPlans,
	}
)

func (plan *Plan) IsYearly(priceID string) bool {
	return plan.PaddlePriceIDYearly == priceID
}

func GetProductsForStage(stage string) []string {
	lock.RLock()
	defer lock.RUnlock()

	plans, ok := stagePlans[stage]
	if !ok {
		return []string{}
	}

	products := make([]string, 0, len(plans))

	for _, plan := range plans {
		products = append(products, plan.PaddleProductID)
	}

	return products
}

func GetPlansForStage(stage string) ([]*Plan, bool) {
	lock.RLock()
	defer lock.RUnlock()

	plans, ok := stagePlans[stage]
	return plans, ok
}

func FindPlanByProductID(paddleProductID string, stage string) (*Plan, error) {
	if (stage == "") || (paddleProductID == "") {
		return nil, errInvalidArgument
	}

	lock.RLock()
	defer lock.RUnlock()

	for _, p := range stagePlans[stage] {
		if p.PaddleProductID == paddleProductID {
			return p, nil
		}
	}

	return nil, ErrUnknownProductID
}

func FindPlanByPriceID(paddlePriceID string, stage string) (*Plan, error) {
	if (stage == "") || (paddlePriceID == "") {
		return nil, errInvalidArgument
	}

	lock.RLock()
	defer lock.RUnlock()

	for _, p := range stagePlans[stage] {
		if (p.PaddlePriceIDMonthly == paddlePriceID) || (p.PaddlePriceIDYearly == paddlePriceID) {
			return p, nil
		}
	}

	return nil, ErrUnknownPriceID
}

func FindPlanByPriceAndProduct(paddleProductID string, paddlePriceID string, stage string) (*Plan, error) {
	if (stage == "") || (paddleProductID == "") || (paddlePriceID == "") {
		return nil, errInvalidArgument
	}

	lock.RLock()
	defer lock.RUnlock()

	for _, p := range stagePlans[stage] {
		if (p.PaddleProductID == paddleProductID) &&
			((p.PaddlePriceIDMonthly == paddlePriceID) || (p.PaddlePriceIDYearly == paddlePriceID)) {
			return p, nil
		}
	}

	return nil, ErrUnknownProductID
}

func UpdatePlansPrices(prices Prices, stage string) {
	lock.Lock()
	defer lock.Unlock()

	for _, p := range stagePlans[stage] {
		if priceMonthly, ok := prices[p.PaddlePriceIDMonthly]; ok {
			p.PriceMonthly = priceMonthly
		}

		if priceYearly, ok := prices[p.PaddlePriceIDYearly]; ok {
			p.PriceYearly = priceYearly
		}
	}
}

func IsSubscriptionActive(status string) bool {
	switch status {
	case paddle.SubscriptionStatusActive, paddle.SubscriptionStatusTrialing:
		return true
	default:
		return false
	}
}
