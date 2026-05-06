package tests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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

func CreateNewPuzzleAPIKeyParams(name string, tnow time.Time, period time.Duration, requestsPerSecond float64) *dbgen.CreateAPIKeyParams {
	return &dbgen.CreateAPIKeyParams{
		Name:              name,
		ExpiresAt:         db.Timestampz(tnow.Add(period)),
		RequestsPerSecond: requestsPerSecond,
		RequestsBurst:     int32(requestsPerSecond) * 5,
		Period:            period,
		Scope:             dbgen.ApiKeyScopePuzzle,
		Readonly:          false,
	}
}

func CreateNewSubscriptionParams(plan billing.Plan) *dbgen.CreateSubscriptionParams {
	tnow := time.Now().UTC()
	priceIDMonthly, priceIDYearly := plan.PriceIDs()
	priceID := priceIDMonthly
	if len(priceID) == 0 {
		priceID = priceIDYearly
	}

	return &dbgen.CreateSubscriptionParams{
		ExternalProductID:      plan.ProductID(),
		ExternalPriceID:        priceID,
		ExternalSubscriptionID: db.Text(xid.New().String()),
		ExternalCustomerID:     db.Text(xid.New().String()),
		Status:                 string(billing.InternalStatusTrialing),
		Source:                 dbgen.SubscriptionSourceInternal,
		TrialEndsAt:            db.Timestampz(tnow.AddDate(0, 0, plan.TrialDays())),
		NextBilledAt:           db.Timestampz(tnow.AddDate(0, 1, 0)),
	}
}

func CreateNewPropertyParams(userID int32, domain string) *dbgen.CreatePropertyParams {
	return &dbgen.CreatePropertyParams{
		Name:             "Property " + xid.New().String(),
		CreatorID:        db.Int(userID),
		Domain:           domain,
		Level:            db.Int2(int16(common.DifficultyLevelMedium)),
		Growth:           dbgen.DifficultyGrowthMedium,
		ValidityInterval: 6 * time.Hour,
		AllowSubdomains:  false,
		AllowLocalhost:   false,
		MaxReplayCount:   1,
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

func DisableProperty(ctx context.Context, store *db.BusinessStore, propertyID int32) error {
	_, err := store.Pool.Exec(ctx, "UPDATE backend.properties SET enabled = FALSE WHERE id = $1", propertyID)
	return err
}

func DisableUserForTest(ctx context.Context, store *db.BusinessStore, userID int32) error {
	_, err := store.Pool.Exec(ctx, "UPDATE backend.users SET enabled = FALSE WHERE id = $1", userID)
	return err
}

// CorruptDifficultyRulePositionsForTest sets all rules to have very close positions
// to force rebalancing when moving. This is used to test the rebalancing logic.
func CorruptDifficultyRulePositionsForTest(ctx context.Context, store *db.BusinessStore, propertyID *int32, orgID *int32) error {
	var query string
	var args []interface{}

	if propertyID != nil {
		query = `WITH numbered AS (
		           SELECT id, (ROW_NUMBER() OVER (ORDER BY position ASC) - 1) * 0.0000001 AS new_pos
		           FROM backend.difficulty_rules
		           WHERE property_id = $1 AND org_id IS NULL
		         )
		         UPDATE backend.difficulty_rules dr
		         SET position = numbered.new_pos
		         FROM numbered
		         WHERE dr.id = numbered.id`
		args = []interface{}{*propertyID}
	} else if orgID != nil {
		query = `WITH numbered AS (
		           SELECT id, (ROW_NUMBER() OVER (ORDER BY position ASC) - 1) * 0.0000001 AS new_pos
		           FROM backend.difficulty_rules
		           WHERE org_id = $1 AND property_id IS NULL
		         )
		         UPDATE backend.difficulty_rules dr
		         SET position = numbered.new_pos
		         FROM numbered
		         WHERE dr.id = numbered.id`
		args = []interface{}{*orgID}
	} else {
		return errors.New("either propertyID or orgID must be provided")
	}

	_, err := store.Pool.Exec(ctx, query, args...)
	return err
}

type StubNoticeProvider struct {
	Value string
}

var _ db.PropertyNoticeProvider = (*StubNoticeProvider)(nil)

func (s *StubNoticeProvider) Notice(_ context.Context, _ *dbgen.Property) string {
	return s.Value
}

func TestCacheSerialization(store *db.BusinessStore) int {
	const maxEntries = 100_000
	var buf bytes.Buffer
	ctx := context.TODO()
	testKey := db.APIKeyCacheKey("test-key-roundtrip")
	if err := store.Cache.Set(ctx, testKey, "test-value-roundtrip"); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to seed cache: %v\n", err)
		return 1
	}
	if err := store.Cache.SaveTo(ctx, &buf, maxEntries); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save cache to buffer: %v\n", err)
		return 1
	}
	newCache, err := db.NewMemoryCache[db.CacheKey, any]("test", maxEntries, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create new cache: %v\n", err)
		return 1
	}
	if err := newCache.LoadFrom(ctx, &buf, 24*time.Hour); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load cache from buffer: %v\n", err)
		return 1
	}
	if val, err := newCache.Get(ctx, testKey); err != nil || val != "test-value-roundtrip" {
		fmt.Fprintf(os.Stderr, "Cache round-trip failed. val: %v, err: %v\n", val, err)
		return 1
	}
	return 0
}
