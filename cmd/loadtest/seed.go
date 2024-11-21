package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"time"

	"github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/rs/xid"
	"golang.org/x/sync/errgroup"
)

const (
	maxCacheSize = 1_000_000
	maxParallel  = 4
)

var (
	difficultyLevels = []dbgen.DifficultyLevel{dbgen.DifficultyLevelSmall, dbgen.DifficultyLevelMedium, dbgen.DifficultyLevelHigh}
	growthLevels     = []dbgen.DifficultyGrowth{dbgen.DifficultyGrowthSlow, dbgen.DifficultyGrowthMedium, dbgen.DifficultyGrowthFast}
)

func seed(usersCount, orgsCount, propertiesCount int, getenv func(string) string) error {
	ctx := context.TODO()

	var cache common.Cache[string, any]
	var err error
	cache, err = db.NewMemoryCache[string, any](5*time.Minute, maxCacheSize, nil /*missing value*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create memory cache for server", common.ErrAttr(err))
		cache = db.NewStaticCache[string, any](maxCacheSize, nil /*missing value*/)
	}

	pool, clickhouse, dberr := db.Connect(ctx, getenv)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	/*defer*/ clickhouse.Close()

	businessDB := db.NewBusiness(pool, cache)

	stage := getenv("STAGE")
	plans, ok := billing.GetPlansForStage(stage)
	if !ok || (len(plans) == 0) {
		return errors.New("no billing plans available for current stage")
	}

	plan := plans[len(plans)-1]

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

func seedUser(ctx context.Context, u int, orgsCount, propertiesCount int, plan *billing.Plan, store *db.BusinessStore) error {
	email := fmt.Sprintf("test.user.%v@privatecaptcha.com", u)
	name := fmt.Sprintf("John%v Doe%v", u, u)
	orgName := fmt.Sprintf("John%v-doe%v", u, u)
	tnow := time.Now().UTC()

	orgs := make([]*dbgen.Organization, 0)

	user, org, err := store.CreateNewAccount(ctx, &dbgen.CreateSubscriptionParams{
		PaddleProductID:      plan.PaddleProductID,
		PaddlePriceID:        plan.PaddlePriceIDMonthly,
		PaddleSubscriptionID: xid.New().String(),
		PaddleCustomerID:     xid.New().String(),
		Status:               paddle.SubscriptionStatusTrialing,
		TrialEndsAt:          db.Timestampz(tnow.AddDate(0, 1, 0)),
		NextBilledAt:         db.Timestampz(tnow.AddDate(0, 1, 0)),
	}, email, name, orgName, -1 /*existingUserID*/)

	if err != nil {
		return err
	}

	orgs = append(orgs, org)

	for o := 0; o < orgsCount-1; o++ {
		extraOrgName := fmt.Sprintf("%s-extra%v", orgName, o)
		org, err := store.CreateNewOrganization(ctx, extraOrgName, user.ID)
		if err != nil {
			return err
		}

		orgs = append(orgs, org)
	}

	for o, org := range orgs {
		for p := 0; p < propertiesCount; p++ {
			_, err = store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
				Name:       fmt.Sprintf("my great property %v", p), // constraint is unique_property_name_per_organization
				OrgID:      db.Int(org.ID),
				CreatorID:  db.Int(user.ID),
				OrgOwnerID: org.UserID,
				Domain:     fmt.Sprintf("test%v.privatecaptcha.com", (u+1)*(o+1)*(p+1)),
				Level:      difficultyLevels[randv2.IntN(len(difficultyLevels))],
				Growth:     growthLevels[randv2.IntN(len(growthLevels))],
			})

			if err != nil {
				return err
			}
		}
	}

	_, err = store.CreateAPIKey(ctx, user.ID, "Test API Key", tnow.AddDate(0, 1, 0), 1000 /*rps*/)
	if err != nil {
		return err
	}

	slog.Info("Finished seeding user", "index", u)
	return nil
}
