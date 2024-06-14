package tests

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/rs/xid"
)

func CreateNewAccountForTest(ctx context.Context, store *db.BusinessStore, testName string) (*dbgen.User, *dbgen.Organization, error) {
	email := testName + "@privatecaptcha.com"

	var parts []string
	start := 0

	for i, r := range testName {
		if i > 0 && (unicode.IsUpper(r) || r == '_') {
			parts = append(parts, testName[start:i])
			start = i
		}
	}
	parts = append(parts, testName[start:])

	testPlan := billing.TestPlans[0]
	tnow := time.Now()

	name := strings.Join(parts, " ")
	orgName := strings.ToLower(strings.Join(parts, "-"))

	return store.CreateNewAccount(ctx, &dbgen.CreateSubscriptionParams{
		PaddleProductID:      testPlan.PaddleProductID,
		PaddlePriceID:        testPlan.PaddlePriceIDMonthly,
		PaddleSubscriptionID: xid.New().String(),
		PaddleCustomerID:     xid.New().String(),
		Status:               paddle.SubscriptionStatusTrialing,
		TrialEndsAt:          db.Timestampz(tnow.AddDate(0, 1, 0)),
		NextBilledAt:         db.Timestampz(tnow.AddDate(0, 1, 0)),
	}, email, name, orgName, -1 /*existingUserID*/)
}
