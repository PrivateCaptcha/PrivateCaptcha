package tests

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/xid"
)

func createUserAndOrgName(testName string) (string, string) {
	var parts []string
	start := 0

	for i, r := range testName {
		if i > 0 && (unicode.IsUpper(r) || r == '_') {
			parts = append(parts, testName[start:i])
			start = i
		}
	}
	parts = append(parts, testName[start:])

	name := strings.Join(parts, " ")
	orgName := strings.ToLower(strings.Join(parts, "-"))

	return name, orgName
}

func CreateNewAccountForTest(ctx context.Context, store *db.BusinessStore, testName string) (*dbgen.User, *dbgen.Organization, error) {
	email := testName + "@privatecaptcha.com"

	testPlan := billing.TestPlans[0]
	tnow := time.Now()

	name, orgName := createUserAndOrgName(testName)

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

func CancelUserSubscription(ctx context.Context, store *db.BusinessStore, userID int32) error {
	subscriptions, err := store.RetrieveSubscriptionsByUserIDs(ctx, []int32{userID})
	if err != nil {
		return err
	}

	subscr := subscriptions[0]
	_, err = store.UpdateSubscription(ctx, &dbgen.UpdateSubscriptionParams{
		PaddleSubscriptionID: subscr.Subscription.PaddleSubscriptionID,
		PaddleProductID:      subscr.Subscription.PaddleProductID,
		Status:               paddle.SubscriptionStatusCanceled,
		NextBilledAt:         pgtype.Timestamptz{},
		CancelFrom:           db.Timestampz(time.Now().UTC()),
	})

	return err
}

func CreateNewBareAccount(ctx context.Context, store *db.BusinessStore, testName string) (*dbgen.User, *dbgen.Organization, error) {
	email := testName + "@privatecaptcha.com"
	name, orgName := createUserAndOrgName(testName)

	return store.CreateNewAccount(ctx, nil /*create subscription params*/, email, name, orgName, -1 /*existingUserID*/)
}

func CreatePropertyForOrg(ctx context.Context, store *db.BusinessStore, org *dbgen.Organization) (*dbgen.Property, error) {
	return store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       fmt.Sprintf("user %v property", org.UserID.Int32),
		OrgID:      db.Int(org.ID),
		CreatorID:  db.Int(org.UserID.Int32),
		OrgOwnerID: db.Int(org.UserID.Int32),
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
}
