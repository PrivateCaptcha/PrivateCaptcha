package tests

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
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

func CreateNewSubscriptionParams(plan billing.Plan) *dbgen.CreateSubscriptionParams {
	tnow := time.Now()
	priceIDMonthly, _ := plan.PriceIDs()

	return &dbgen.CreateSubscriptionParams{
		ExternalProductID:      plan.ProductID(),
		ExternalPriceID:        priceIDMonthly,
		ExternalSubscriptionID: db.Text(xid.New().String()),
		ExternalCustomerID:     db.Text(xid.New().String()),
		Status:                 string(billing.InternalStatusTrialing),
		Source:                 dbgen.SubscriptionSourceInternal,
		TrialEndsAt:            db.Timestampz(tnow.AddDate(0, 1, 0)),
		NextBilledAt:           db.Timestampz(tnow.AddDate(0, 1, 0)),
	}
}

func CreateNewAccountForTest(ctx context.Context, store db.Implementor, testName string, plan billing.Plan) (*dbgen.User, *dbgen.Organization, error) {
	return CreateNewAccountForTestEx(ctx, store, testName, CreateNewSubscriptionParams(plan))
}

func CreateNewAccountForTestEx(ctx context.Context, store db.Implementor, testName string, subscrParams *dbgen.CreateSubscriptionParams) (*dbgen.User, *dbgen.Organization, error) {
	email := testName + "@privatecaptcha.com"
	name, orgName := createUserAndOrgName(testName)

	var user *dbgen.User
	var org *dbgen.Organization

	if err := store.WithTx(ctx, func(impl *db.BusinessStoreImpl) error {
		var err error
		user, org, err = impl.CreateNewAccount(ctx, subscrParams, email, name, orgName, -1 /*existingUserID*/)
		return err
	}); err != nil {
		return nil, nil, err
	}
	return user, org, nil
}

func CancelUserSubscription(ctx context.Context, store db.Implementor, userID int32) error {
	subscriptions, err := store.Impl().RetrieveSubscriptionsByUserIDs(ctx, []int32{userID})
	if err != nil {
		return err
	}

	subscr := subscriptions[0]
	_, err = store.Impl().UpdateSubscription(ctx, &dbgen.UpdateSubscriptionParams{
		ExternalSubscriptionID: subscr.Subscription.ExternalSubscriptionID,
		ExternalProductID:      subscr.Subscription.ExternalProductID,
		Status:                 "cancelled",
		NextBilledAt:           pgtype.Timestamptz{},
		CancelFrom:             db.Timestampz(time.Now().UTC()),
	})

	return err
}

func CreateNewBareAccount(ctx context.Context, store db.Implementor, testName string) (*dbgen.User, *dbgen.Organization, error) {
	email := testName + "@privatecaptcha.com"
	name, orgName := createUserAndOrgName(testName)

	var user *dbgen.User
	var org *dbgen.Organization

	if err := store.WithTx(ctx, func(impl *db.BusinessStoreImpl) error {
		var err error

		user, org, err = impl.CreateNewAccount(ctx, nil /*create subscription params*/, email, name, orgName, -1 /*existingUserID*/)
		return err
	}); err != nil {
		return nil, nil, err
	}
	return user, org, nil
}

func CreatePropertyForOrg(ctx context.Context, store db.Implementor, org *dbgen.Organization) (*dbgen.Property, error) {
	return store.Impl().CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       fmt.Sprintf("user %v property", org.UserID.Int32),
		OrgID:      db.Int(org.ID),
		CreatorID:  db.Int(org.UserID.Int32),
		OrgOwnerID: db.Int(org.UserID.Int32),
		Level:      db.Int2(int16(common.DifficultyLevelMedium)),
		Growth:     dbgen.DifficultyGrowthMedium,
	})
}
