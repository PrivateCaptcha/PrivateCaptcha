package billing

import (
	"errors"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type Plan struct {
	Name                 string
	PaddleProductID      string
	PaddlePriceIDMonthly string
	PaddlePriceIDYearly  string
	DefaultMonthlyPrice  int
	DefaultYearlyPrice   int
	Version              int
	RequestsLimit        int64
	OrgsLimit            int
	PropertiesLimit      int
}

const (
	defaultOrgLimit        = 10
	defaultPropertiesLimit = 50
	version1               = 1
)

var (
	ErrUnknownProductID = errors.New("unknown product ID")

	devPlans = []*Plan{
		{
			Name:                 "Private Captcha 1K",
			PaddleProductID:      "pro_01j0379m2fed2nrf4hb2gmb6gh",
			PaddlePriceIDMonthly: "pri_01j037bk2bwtpryzqzafg1kyxk",
			PaddlePriceIDYearly:  "pri_01j037cm0ny17tk8t9d7qksntq",
			DefaultMonthlyPrice:  9,
			DefaultYearlyPrice:   9 * 11,
			RequestsLimit:        1_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
		},
		{
			Name:                 "Private Captcha 5K",
			PaddleProductID:      "pro_01j03v1tmp3gm1mmvhg357cg8v",
			PaddlePriceIDMonthly: "pri_01j03v3ne7q50zfndesjrp74k8",
			PaddlePriceIDYearly:  "pri_01j03v4vkw4jhe3a4ahpcp888c",
			DefaultMonthlyPrice:  19,
			DefaultYearlyPrice:   19 * 11,
			RequestsLimit:        5_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
		},
		{
			Name:                 "Private Captcha 10K",
			PaddleProductID:      "pro_01j03v5bpwqfk5gs09wm6cqgxp",
			PaddlePriceIDMonthly: "pri_01j03v5xqapmx8srxw3a7tz2ny",
			PaddlePriceIDYearly:  "pri_01j03v6jb4mzkdtxp2xrgb1vdt",
			DefaultMonthlyPrice:  29,
			DefaultYearlyPrice:   29 * 11,
			RequestsLimit:        10_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
		},
		{
			Name:                 "Private Captcha 25K",
			PaddleProductID:      "pro_01j03v84qgmc3qe61v3302k6wz",
			PaddlePriceIDMonthly: "pri_01j03v8m86qtfdhcy6hap60xe8",
			PaddlePriceIDYearly:  "pri_01j03v9b410k9pd09xkev1yw28",
			DefaultMonthlyPrice:  49,
			DefaultYearlyPrice:   49 * 11,
			RequestsLimit:        25_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
		},
		{
			Name:                 "Private Captcha 50K",
			PaddleProductID:      "pro_01j03v9zzanph7e0f0j89p1bqz",
			PaddlePriceIDMonthly: "pri_01j03vagbx7ew49yz8ybgjch78",
			PaddlePriceIDYearly:  "pri_01j03vb735t6j3z619ae2ar2n3",
			DefaultMonthlyPrice:  79,
			DefaultYearlyPrice:   79 * 11,
			RequestsLimit:        50_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
		},
		{
			Name:                 "Private Captcha 100K",
			PaddleProductID:      "pro_01j03vcaxkcf55s5n34dz41nrr",
			PaddlePriceIDMonthly: "pri_01j03vyfv2nx04s31dcs3bd54z",
			PaddlePriceIDYearly:  "pri_01j03vz7gkxb755c31sacg78gr",
			DefaultMonthlyPrice:  129,
			DefaultYearlyPrice:   129 * 11,
			RequestsLimit:        100_000,
			Version:              version1,
			OrgsLimit:            defaultOrgLimit,
			PropertiesLimit:      defaultPropertiesLimit,
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

func GetPlansForStage(stage string) ([]*Plan, bool) {
	plans, ok := stagePlans[stage]
	return plans, ok
}

func FindPlanByPaddlePrice(paddleProductID string, paddlePriceID string, stage string) (*Plan, error) {
	for _, p := range stagePlans[stage] {
		if (p.PaddleProductID == paddleProductID) &&
			((p.PaddlePriceIDMonthly == paddlePriceID) || (p.PaddlePriceIDYearly == paddlePriceID)) {
			return p, nil
		}
	}

	return nil, ErrUnknownProductID
}
