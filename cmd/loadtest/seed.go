package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/rs/xid"
	"golang.org/x/sync/errgroup"
)

const (
	maxParallel      = 4
	puzzleDifficulty = common.DifficultyLevelSmall - 2*common.DifficultyDelta
)

var (
	difficultyLevels = []common.DifficultyLevel{common.DifficultyLevelSmall, common.DifficultyLevelMedium, common.DifficultyLevelHigh}
	growthLevels     = []dbgen.DifficultyGrowth{dbgen.DifficultyGrowthSlow, dbgen.DifficultyGrowthMedium, dbgen.DifficultyGrowthFast}
	dotBytes         = []byte(".")
)

func seed(usersCount, orgsCount, propertiesCount, solutionsCount int, solutionsFile string, billingSvc billing.PlanService, cfg common.ConfigStore) error {
	ctx := context.TODO()

	pool, clickhouse, dberr := db.Connect(ctx, cfg, 5*time.Second, false /*admin*/)
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

	var wg sync.WaitGroup
	resultsChan := make(chan string, maxParallel*10)
	writerDone := make(chan struct{})
	go saveSolutionsToFile(solutionsFile, writerDone, resultsChan)

	solutionsSemaphore := make(chan struct{}, maxParallel*usersCount)

	for u := 0; u < usersCount; u++ {
		errs.Go(func() error {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			return seedUser(ctx, u, orgsCount, propertiesCount, solutionsCount, solutionsFile, plan, businessDB, cfg, &wg, resultsChan, solutionsSemaphore)
		})
	}

	if err := errs.Wait(); err != nil {
		return err
	}

	go func() {
		slog.Debug("Waiting for all solutions to be generated")
		wg.Wait()
		close(resultsChan)
	}()

	slog.Debug("Wait for the solutions to be written to file")
	<-writerDone

	return nil
}

func seedUser(ctx context.Context, u int, orgsCount, propertiesCount, solutionsCount int, solutionsFile string, plan billing.Plan, store db.Implementor, cfg common.ConfigStore, wg *sync.WaitGroup, resultsChan chan string, solutionsSemaphore chan struct{}) error {
	email := fmt.Sprintf("test.user.%v@privatecaptcha.com", u)
	name := fmt.Sprintf("John%v Doe%v", u, u)
	orgName := fmt.Sprintf("John%v-doe%v", u, u)
	tnow := time.Now().UTC()

	orgs := make([]*dbgen.Organization, 0, orgsCount)

	salt := api.NewPuzzleSalt(cfg.Get(common.APISaltKey))
	salt.Update()

	priceIDMonthly, _ := plan.PriceIDs()

	var user *dbgen.User
	var org *dbgen.Organization

	_, err := store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		var err error
		var auditEvents []*common.AuditLogEvent
		user, org, auditEvents, err = impl.CreateNewAccount(ctx, &dbgen.CreateSubscriptionParams{
			ExternalProductID:      plan.ProductID(),
			ExternalPriceID:        priceIDMonthly,
			ExternalSubscriptionID: db.Text(xid.New().String()),
			ExternalCustomerID:     db.Text(xid.New().String()),
			Source:                 dbgen.SubscriptionSourceInternal,
			Status:                 "trialing",
			TrialEndsAt:            db.Timestampz(tnow.AddDate(0, 1, 0)),
			NextBilledAt:           db.Timestampz(tnow.AddDate(0, 1, 0)),
		}, email, name, orgName, -1 /*existingUserID*/)
		return auditEvents, err
	})

	if err != nil {
		return err
	}

	orgs = append(orgs, org)

	for o := 0; o < orgsCount-1; o++ {
		extraOrgName := fmt.Sprintf("%s-extra%v", orgName, o)
		if eorg, _, err := store.Impl().CreateNewOrganization(ctx, extraOrgName, user.ID); err != nil {
			return err
		} else {
			orgs = append(orgs, eorg)
		}
	}

	solver := &puzzle.ComputeSolver{}

	for o, uorg := range orgs {
		for p := 0; p < propertiesCount; p++ {
			property, _, err := store.Impl().CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
				Name:             fmt.Sprintf("my great property %v", p), // constraint is unique_property_name_per_organization
				CreatorID:        db.Int(user.ID),
				Domain:           fmt.Sprintf("test%v.privatecaptcha.com", (u+1)*(o+1)*(p+1)),
				Level:            db.Int2(int16(difficultyLevels[randv2.IntN(len(difficultyLevels))])),
				Growth:           growthLevels[randv2.IntN(len(growthLevels))],
				ValidityInterval: 6 * time.Hour,
				MaxReplayCount:   1,
				AllowSubdomains:  false,
				AllowLocalhost:   true,
			}, uorg)

			if err != nil {
				return err
			}

			for s := 0; s < solutionsCount; s++ {
				cp := puzzle.NewComputePuzzle(puzzle.NextPuzzleID(), property.ExternalID.Bytes, uint8(puzzleDifficulty))
				if err := cp.Init(property.ValidityInterval); err != nil {
					return err
				}
				wg.Add(1)

				go func(pzzl puzzle.Puzzle, extraSalt []byte) {
					solutionsSemaphore <- struct{}{}
					defer func() { <-solutionsSemaphore }()
					defer wg.Done()

					if solutions, err := solver.Solve(pzzl); err == nil {
						if payload, err := pzzl.Serialize(ctx, salt.Value(), extraSalt); err == nil {
							var buf bytes.Buffer

							buf.WriteString(solutions.String())
							buf.Write(dotBytes)
							payload.Write(&buf)

							resultsChan <- buf.String()
						} else {
							slog.Error("Failed to serialize the solution and puzzle")
						}
					} else {
						slog.Error("Failed to find the solution for puzzle")
					}
				}(cp, property.Salt)
			}
		}
	}

	period := 30 * 24 * time.Hour
	_, _, err = store.Impl().CreateAPIKey(ctx, user, &dbgen.CreateAPIKeyParams{
		Name:              name,
		UserID:            db.Int(user.ID),
		ExpiresAt:         db.Timestampz(tnow.Add(period)),
		RequestsPerSecond: 1000,
		RequestsBurst:     5 * 1000,
		Period:            period,
		Scope:             dbgen.ApiKeyScopePuzzle,
	})
	if err != nil {
		return err
	}

	slog.Info("Finished seeding user", "index", u)
	return nil
}

func saveSolutionsToFile(filename string, writerDone chan struct{}, resultsChan chan string) error {
	defer close(writerDone)

	if len(filename) == 0 {
		return nil
	}

	file, err := os.Create(filename)
	if err != nil {
		slog.Error("Failed to create file", "filename", filename, common.ErrAttr(err))
		return err
	}
	defer file.Close()

	bufWriter := bufio.NewWriter(file)
	defer bufWriter.Flush()

	for result := range resultsChan {
		if _, err := bufWriter.WriteString(result); err != nil {
			slog.Error("Failed to write string", "filename", filename, common.ErrAttr(err))
		}
		if _, err := bufWriter.WriteString("\n"); err != nil {
			slog.Error("Failed to write string", "filename", filename, common.ErrAttr(err))
		}
	}

	slog.Info("Finished writing solutions to the file")

	return nil
}
