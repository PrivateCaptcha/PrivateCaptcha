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

	if _, err := store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		var err error
		var auditEvents []*common.AuditLogEvent
		user, org, auditEvents, err = impl.CreateNewAccount(ctx, subscrParams, email, name, orgName, -1 /*existingUserID*/)
		return auditEvents, err
	}); err != nil {
		return nil, nil, err
	}
	return user, org, nil
}

func CreateNewBareAccount(ctx context.Context, store db.Implementor, testName string) (*dbgen.User, *dbgen.Organization, error) {
	email := testName + "@privatecaptcha.com"
	name, orgName := createUserAndOrgName(testName)

	var user *dbgen.User
	var org *dbgen.Organization

	if _, err := store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		var err error
		var auditEvents []*common.AuditLogEvent
		user, org, auditEvents, err = impl.CreateNewAccount(ctx, nil /*create subscription params*/, email, name, orgName, -1 /*existingUserID*/)
		return auditEvents, err
	}); err != nil {
		return nil, nil, err
	}
	return user, org, nil
}

func CreatePropertyForOrg(ctx context.Context, store db.Implementor, org *dbgen.Organization) (*dbgen.Property, error) {
	property, _, err := store.Impl().CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:      fmt.Sprintf("user %v property", org.UserID.Int32),
		Domain:    fmt.Sprintf("%s.org", strings.ReplaceAll(strings.ToLower(org.Name), " ", "-")),
		CreatorID: db.Int(org.UserID.Int32),
		Level:     db.Int2(int16(common.DifficultyLevelMedium)),
		Growth:    dbgen.DifficultyGrowthMedium,
	}, org)
	return property, err
}
