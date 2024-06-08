package billing

import (
	"errors"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type Plan struct {
	Name                   string
	PaddleProductIDMonthly string
	PaddleProductIDYearly  string
	Version                int
	RequestsLimit          int64
	OrgsLimit              int
	PropertiesLimit        int
}

const (
	defaultOrgLimit        = 10
	defaultPropertiesLimit = 50
	version1               = 1
)

var (
	ErrUnknownProductID = errors.New("unknown product ID")

	prodPlans = []*Plan{
		{
			Name:                   "Private Captcha 1K",
			PaddleProductIDMonthly: "",
			PaddleProductIDYearly:  "",
			RequestsLimit:          1_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
		{
			Name:                   "Private Captcha 5K",
			PaddleProductIDMonthly: "",
			PaddleProductIDYearly:  "",
			RequestsLimit:          5_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
		{
			Name:                   "Private Captcha 10K",
			PaddleProductIDMonthly: "",
			PaddleProductIDYearly:  "",
			RequestsLimit:          10_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
		{
			Name:                   "Private Captcha 25K",
			PaddleProductIDMonthly: "",
			PaddleProductIDYearly:  "",
			RequestsLimit:          25_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
		{
			Name:                   "Private Captcha 50K",
			PaddleProductIDMonthly: "",
			PaddleProductIDYearly:  "",
			RequestsLimit:          50_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
		{
			Name:                   "Private Captcha 75K",
			PaddleProductIDMonthly: "",
			PaddleProductIDYearly:  "",
			RequestsLimit:          75_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
		{
			Name:                   "Private Captcha 100K",
			PaddleProductIDMonthly: "",
			PaddleProductIDYearly:  "",
			RequestsLimit:          100_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
		{
			Name:                   "Private Captcha 200K",
			PaddleProductIDMonthly: "",
			PaddleProductIDYearly:  "",
			RequestsLimit:          200_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
	}

	devPlans = []*Plan{
		{
			Name:                   "Private Captcha Dev",
			PaddleProductIDMonthly: "123456",
			PaddleProductIDYearly:  "234567",
			RequestsLimit:          100_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
	}

	TestPlans = []*Plan{
		{
			Name:                   "Private Captcha Test",
			PaddleProductIDMonthly: "123456",
			PaddleProductIDYearly:  "234567",
			RequestsLimit:          10_000,
			Version:                version1,
			OrgsLimit:              defaultOrgLimit,
			PropertiesLimit:        defaultPropertiesLimit,
		},
	}

	stagePlans = map[string][]*Plan{
		common.StageProd: prodPlans,
		common.StageDev:  devPlans,
		common.StageTest: TestPlans,
	}
)

func FindPlanByProductID(paddleProductID string, stage string) (*Plan, error) {
	for _, p := range stagePlans[stage] {
		if (p.PaddleProductIDMonthly == paddleProductID) || (p.PaddleProductIDYearly == paddleProductID) {
			return p, nil
		}
	}

	return nil, ErrUnknownProductID
}
