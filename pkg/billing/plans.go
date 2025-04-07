package billing

import (
	"errors"
	"sync"

	"github.com/PaddleHQ/paddle-go-sdk/v3/pkg/paddlenotification"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

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

const (
	defaultOrgLimit        = 10
	defaultPropertiesLimit = 50
	defaultTrialDays       = 14
	defaultAPIKeyRequests  = 1.0
	version1               = 1
)

var (
	lock                sync.RWMutex
	ErrUnknownProductID = errors.New("unknown product ID")
	ErrUnknownPriceID   = errors.New("unknown price ID")

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
		ThrottleLimit:        2_000_000,
		APIRequestsPerSecond: 100,
	}

	devPlans = []*Plan{
		{
			Name:            "Private Captcha 1K",
			PaddleProductID: "pro_01j0379m2fed2nrf4hb2gmb6gh",
			//PaddlePriceIDMonthly: "pri_01j037bk2bwtpryzqzafg1kyxk",
			PaddlePriceIDYearly:  "pri_01j037cm0ny17tk8t9d7qksntq",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         9,
			PriceYearly:          9 * 11,
			RequestsLimit:        1_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        5_000, // next plan's limit
			APIRequestsPerSecond: 1.0,
		},
		{
			Name:                 "Private Captcha 5K",
			PaddleProductID:      "pro_01j03v1tmp3gm1mmvhg357cg8v",
			PaddlePriceIDMonthly: "pri_01j03v3ne7q50zfndesjrp74k8",
			PaddlePriceIDYearly:  "pri_01j03v4vkw4jhe3a4ahpcp888c",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         19,
			PriceYearly:          19 * 11,
			RequestsLimit:        5_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        10_000, // next plan's limit
			APIRequestsPerSecond: 1.0,
		},
		{
			Name:                 "Private Captcha 10K",
			PaddleProductID:      "pro_01j03v5bpwqfk5gs09wm6cqgxp",
			PaddlePriceIDMonthly: "pri_01j03v5xqapmx8srxw3a7tz2ny",
			PaddlePriceIDYearly:  "pri_01j03v6jb4mzkdtxp2xrgb1vdt",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         29,
			PriceYearly:          29 * 11,
			RequestsLimit:        10_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        25_000, // next plan's limit
			APIRequestsPerSecond: 2.0,
		},
		{
			Name:                 "Private Captcha 25K",
			PaddleProductID:      "pro_01j03v84qgmc3qe61v3302k6wz",
			PaddlePriceIDMonthly: "pri_01j03v8m86qtfdhcy6hap60xe8",
			PaddlePriceIDYearly:  "pri_01j03v9b410k9pd09xkev1yw28",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         49,
			PriceYearly:          49 * 11,
			RequestsLimit:        25_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        50_000, // next plan's limit
			APIRequestsPerSecond: 2.0,
		},
		{
			Name:                 "Private Captcha 50K",
			PaddleProductID:      "pro_01j03v9zzanph7e0f0j89p1bqz",
			PaddlePriceIDMonthly: "pri_01j03vagbx7ew49yz8ybgjch78",
			PaddlePriceIDYearly:  "pri_01j03vb735t6j3z619ae2ar2n3",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         79,
			PriceYearly:          79 * 11,
			RequestsLimit:        50_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        100_000, // next plan's limit
			APIRequestsPerSecond: 4.0,
		},
		{
			Name:                 "Private Captcha 100K",
			PaddleProductID:      "pro_01j03vcaxkcf55s5n34dz41nrr",
			PaddlePriceIDMonthly: "pri_01j03vyfv2nx04s31dcs3bd54z",
			PaddlePriceIDYearly:  "pri_01j03vz7gkxb755c31sacg78gr",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         129,
			PriceYearly:          129 * 11,
			RequestsLimit:        100_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        200_000, // x2
			APIRequestsPerSecond: 10.0,
		},
	}

	prodPlans = []*Plan{
		{
			Name:            "Private Captcha 1K",
			PaddleProductID: "pro_01jgby7f4zvtn644canpryjs5e",
			//PaddlePriceIDMonthly: "",
			PaddlePriceIDYearly:  "pri_01jgby921qq2q3t12qnjdxz3dj",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         9,
			PriceYearly:          59,
			RequestsLimit:        1_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        5_000, // next plan's limit
			APIRequestsPerSecond: 10,
		},
		{
			Name:                 "Private Captcha 5K",
			PaddleProductID:      "pro_01jgbyg4y3hamxqvpvjcfv40gz",
			PaddlePriceIDMonthly: "pri_01jgbyk6sq3gkwjkem891azdd7",
			PaddlePriceIDYearly:  "pri_01jgbyn4fs9rarwrjmrpx8k64s",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         15,
			PriceYearly:          150,
			RequestsLimit:        5_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        10_000, // next plan's limit
			APIRequestsPerSecond: 15,
		},
		{
			Name:                 "Private Captcha 10K",
			PaddleProductID:      "pro_01jgbyr1yvtcezgmm2svf3yrkh",
			PaddlePriceIDMonthly: "pri_01jgbysz696kqbvtwvz8vqh5x5",
			PaddlePriceIDYearly:  "pri_01jgbz15bp8nx7qwb5932bed64",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         29,
			PriceYearly:          290,
			RequestsLimit:        10_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        25_000, // next plan's limit
			APIRequestsPerSecond: 20,
		},
		{
			Name:                 "Private Captcha 25K",
			PaddleProductID:      "pro_01jgbz5gpastcsnyfyrwsftc4k",
			PaddlePriceIDMonthly: "pri_01jgbz658cnmkrcq9zdyzbzbmt",
			PaddlePriceIDYearly:  "pri_01jgbz6z4jbm1kadvck8yc2vgy",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         49,
			PriceYearly:          490,
			RequestsLimit:        25_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        50_000, // next plan's limit
			APIRequestsPerSecond: 25,
		},
		{
			Name:                 "Private Captcha 50K",
			PaddleProductID:      "pro_01jgbzaw24w528ckgtyew5ev5m",
			PaddlePriceIDMonthly: "pri_01jgbzbjd9ttp4kv02yjnab10k",
			PaddlePriceIDYearly:  "pri_01jgbzc69a3q409529q1wxcprk",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         99,
			PriceYearly:          990,
			RequestsLimit:        50_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        100_000, // next plan's limit
			APIRequestsPerSecond: 40,
		},
		{
			Name:                 "Private Captcha 100K",
			PaddleProductID:      "pro_01jgbzfyfwyx49ypp4tcth1d03",
			PaddlePriceIDMonthly: "pri_01jgbzhwemg90t8c1r5w972ekt",
			PaddlePriceIDYearly:  "pri_01jgbzjtrrw30t3m0t8pta1sap",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         199,
			PriceYearly:          1990,
			RequestsLimit:        100_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        200_000, // next plan's limit
			APIRequestsPerSecond: 75,
		},
		{
			Name:                 "Private Captcha 200K",
			PaddleProductID:      "pro_01jgbzremf60a5404yc0538h46",
			PaddlePriceIDMonthly: "pri_01jgbzs0rm49k9t8bq27qs5b8y",
			PaddlePriceIDYearly:  "pri_01jgbzt1z45s1cdy83hszhb885",
			TrialDays:            defaultTrialDays,
			PriceMonthly:         299,
			PriceYearly:          2990,
			RequestsLimit:        200_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
			ThrottleLimit:        400_000, // next plan's limit
			APIRequestsPerSecond: 100,
		},
	}

	stagePlans = map[string][]*Plan{
		common.StageProd:    prodPlans,
		common.StageStaging: devPlans,
		common.StageDev:     devPlans,
	}

	internalPlans = []*Plan{
		internalTrialPlan,
		internalAdminPlan,
	}
)

func (plan *Plan) IsYearly(priceID string) bool {
	return plan.PaddlePriceIDYearly == priceID
}

func GetInternalAdminPlan() *Plan {
	return internalAdminPlan
}

func GetInternalTrialPlan() *Plan {
	return internalTrialPlan
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

func FindPlanEx(paddleProductID string, paddlePriceID string, stage string, internal bool) (*Plan, error) {
	if (stage == "") || (paddleProductID == "") || (paddlePriceID == "") {
		return nil, errInvalidArgument
	}

	lock.RLock()
	defer lock.RUnlock()

	var plans []*Plan
	if internal {
		plans = internalPlans
	} else {
		plans = stagePlans[stage]
	}

	for _, p := range plans {
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
	switch paddlenotification.SubscriptionStatus(status) {
	case paddlenotification.SubscriptionStatusActive, paddlenotification.SubscriptionStatusTrialing:
		return true
	default:
		return false
	}
}

func IsSubscriptionTrialing(status string) bool {
	switch paddlenotification.SubscriptionStatus(status) {
	case paddlenotification.SubscriptionStatusTrialing:
		return true
	default:
		return false
	}
}
