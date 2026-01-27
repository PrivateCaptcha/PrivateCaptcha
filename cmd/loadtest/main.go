package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
)

const (
	modeSeed       = "seed"
	modeTestPuzzle = "test-puzzle"
	modeTestVerify = "test-verify"
)

var (
	envFileFlag         = flag.String("env", "", "Path to .env file, 'stdin' or empty")
	flagMode            = flag.String("mode", "", strings.Join([]string{modeSeed, modeTestPuzzle, modeTestVerify}, " | "))
	flagUsersCount      = flag.Int("user-count", 100, "number of users to seed")
	flagOrgsCount       = flag.Int("org-count", 10, "number of orgs to seed")
	flagPropertiesCount = flag.Int("property-count", 100, "number of properties to seed")
	flagRatePerSecond   = flag.Int("rps", 100, "Requests per second")
	flagDuration        = flag.Int("duration", 10, "Duration of the load test (seconds)")
	flagSitekeyPercent  = flag.Int("sitekey-percent", 100, "Percent of valid sitekey requests")
	flagSolutionsFile   = flag.String("solutions-file", "", "Filepath to solutions file (e.g. solutions.txt)")
	env                 *common.EnvMap
)

func main() {
	flag.Parse()

	var err error

	env, err = common.NewEnvMap(*envFileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	cfg := config.NewEnvConfig(env.Get)

	switch *flagMode {
	case modeSeed:
		svc := billing.NewPlanService(nil)
		solutionsCount := 0
		if *flagSolutionsFile != "" {
			// we need to send N {RPS} {/verify} requests for {duration} seconds **in total**
			solutionsCount = max(2, ((*flagDuration)*(*flagRatePerSecond))/((*flagUsersCount)*(*flagOrgsCount)*(*flagPropertiesCount)))
		}
		err = seed(*flagUsersCount, *flagOrgsCount, *flagPropertiesCount, solutionsCount, *flagSolutionsFile, svc, cfg)
	case modeTestPuzzle:
		err = loadPuzzle((*flagUsersCount)*(*flagOrgsCount)*(*flagPropertiesCount), cfg, *flagRatePerSecond, *flagDuration,
			*flagSitekeyPercent)
	case modeTestVerify:
		err = loadVerify((*flagUsersCount)*(*flagOrgsCount)*(*flagPropertiesCount), *flagSolutionsFile, cfg, *flagRatePerSecond, *flagDuration)
	default:
		err = fmt.Errorf("unknown mode: '%s'", *flagMode)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
