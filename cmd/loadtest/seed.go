package main

import (
	"context"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/rs/xid"
	"golang.org/x/sync/errgroup"
)

const (
	maxParallel = 4
)

var (
	difficultyLevels = []common.DifficultyLevel{common.DifficultyLevelSmall, common.DifficultyLevelMedium, common.DifficultyLevelHigh}
	growthLevels     = []dbgen.DifficultyGrowth{dbgen.DifficultyGrowthSlow, dbgen.DifficultyGrowthMedium, dbgen.DifficultyGrowthFast}
)

func seed(usersCount, orgsCount, propertiesCount int, billingSvc billing.PlanService, cfg common.ConfigStore) error {
	ctx := context.TODO()

	pool, clickhouse, dberr := db.Connect(ctx, cfg, false /*admin*/)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	/*defer*/ clickhouse.Close()

	businessDB := db.NewBusiness(pool)

	planService := billing.NewPlanService(nil)
	plan := planService.GetInternalTrialPlan()

	semaphore := make(chan struct{}, maxParallel)
	errs, ctx := errgroup.WithContext(ctx)

	for u := 0; u < usersCount; u++ {
		errs.Go(func() error {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			return seedUser(ctx, u, orgsCount, propertiesCount, plan, businessDB)
		})
	}

	return errs.Wait()
}

func seedUser(ctx context.Context, u int, orgsCount, propertiesCount int, plan billing.Plan, store *db.BusinessStore) error {
	email := fmt.Sprintf("test.user.%v@privatecaptcha.com", u)
	name := fmt.Sprintf("John%v Doe%v", u, u)
	orgName := fmt.Sprintf("John%v-doe%v", u, u)
	tnow := time.Now().UTC()

	orgs := make([]*dbgen.Organization, 0)

	priceIDMonthly, _ := plan.PriceIDs()

	var user *dbgen.User
	var org *dbgen.Organization

	err := store.WithTx(ctx, func(impl *db.BusinessStoreImpl) error {
		var err error
		user, org, err = impl.CreateNewAccount(ctx, &dbgen.CreateSubscriptionParams{
			ExternalProductID:      plan.ProductID(),
			ExternalPriceID:        priceIDMonthly,
			ExternalSubscriptionID: db.Text(xid.New().String()),
			ExternalCustomerID:     db.Text(xid.New().String()),
			Source:                 dbgen.SubscriptionSourceInternal,
			Status:                 "trialing",
			TrialEndsAt:            db.Timestampz(tnow.AddDate(0, 1, 0)),
			NextBilledAt:           db.Timestampz(tnow.AddDate(0, 1, 0)),
		}, email, name, orgName, -1 /*existingUserID*/)
		return err
	})

	if err != nil {
		return err
	}

	orgs = append(orgs, org)

	for o := 0; o < orgsCount-1; o++ {
		extraOrgName := fmt.Sprintf("%s-extra%v", orgName, o)
		org, err := store.Impl().CreateNewOrganization(ctx, extraOrgName, user.ID)
		if err != nil {
			return err
		}

		orgs = append(orgs, org)
	}

	for o, org := range orgs {
		for p := 0; p < propertiesCount; p++ {
			_, err = store.Impl().CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
				Name:       fmt.Sprintf("my great property %v", p), // constraint is unique_property_name_per_organization
				OrgID:      db.Int(org.ID),
				CreatorID:  db.Int(user.ID),
				OrgOwnerID: org.UserID,
				Domain:     fmt.Sprintf("test%v.privatecaptcha.com", (u+1)*(o+1)*(p+1)),
				Level:      db.Int2(int16(difficultyLevels[randv2.IntN(len(difficultyLevels))])),
				Growth:     growthLevels[randv2.IntN(len(growthLevels))],
			})

			if err != nil {
				return err
			}
		}
	}

	_, err = store.Impl().CreateAPIKey(ctx, user.ID, "Test API Key", tnow.AddDate(0, 1, 0), 1000 /*rps*/)
	if err != nil {
		return err
	}

	slog.Info("Finished seeding user", "index", u)
	return nil
}
