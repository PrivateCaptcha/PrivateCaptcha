package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
)

func parseGrowth(s string) (dbgen.DifficultyGrowth, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "constant", "const", "off":
		return dbgen.DifficultyGrowthConstant, nil
	case "slow":
		return dbgen.DifficultyGrowthSlow, nil
	case "normal", "medium":
		return dbgen.DifficultyGrowthMedium, nil
	case "fast":
		return dbgen.DifficultyGrowthFast, nil
	default:
		return dbgen.DifficultyGrowthMedium, fmt.Errorf("unknown growth %q", s)
	}
}

func parseDist(s string) (difficulty.SimDist, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "const", "constant":
		return difficulty.SimDistConst, nil
	case "poisson":
		return difficulty.SimDistPoisson, nil
	case "burst":
		return difficulty.SimDistBurst, nil
	default:
		return difficulty.SimDistPoisson, fmt.Errorf("unknown distribution %q", s)
	}
}

func parseFormula(s string) (difficulty.SimFormula, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "legacy", "v1":
		return difficulty.SimFormulaV1, nil
	case "v2", "candidate", "":
		return difficulty.SimFormulaV2, nil
	default:
		return difficulty.SimFormulaV2, fmt.Errorf("unknown formula %q", s)
	}
}

func applyScenario(name string, cfg *difficulty.SimConfig) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "steady":
		cfg.UserModel = difficulty.SimRequestModel{Dist: difficulty.SimDistPoisson, Rate: 0.15}
		cfg.PropertyModel = difficulty.SimRequestModel{Dist: difficulty.SimDistPoisson, Rate: 5}
	case "user-burst":
		cfg.UserModel = difficulty.SimRequestModel{Dist: difficulty.SimDistBurst, Rate: 0.15, BurstRate: 3, BurstStart: 120, BurstEnd: 240}
		cfg.PropertyModel = difficulty.SimRequestModel{Dist: difficulty.SimDistPoisson, Rate: 5}
	case "property-spike":
		cfg.UserModel = difficulty.SimRequestModel{Dist: difficulty.SimDistPoisson, Rate: 0.15}
		cfg.PropertyModel = difficulty.SimRequestModel{Dist: difficulty.SimDistBurst, Rate: 5, BurstRate: 50, BurstStart: 120, BurstEnd: 240}
	case "property-long-spike":
		cfg.UserModel = difficulty.SimRequestModel{Dist: difficulty.SimDistPoisson, Rate: 0.15}
		cfg.PropertyModel = difficulty.SimRequestModel{Dist: difficulty.SimDistBurst, Rate: 5, BurstRate: 25, BurstStart: 120, BurstEnd: 420}
	case "both-burst", "default", "":
		cfg.UserModel = difficulty.SimRequestModel{Dist: difficulty.SimDistBurst, Rate: 0.15, BurstRate: 3, BurstStart: 120, BurstEnd: 240}
		cfg.PropertyModel = difficulty.SimRequestModel{Dist: difficulty.SimDistBurst, Rate: 5, BurstRate: 50, BurstStart: 120, BurstEnd: 240}
	}
}

func main() {
	var propertyK float64
	var (
		steps              = flag.Int("steps", 600, "number of simulation steps")
		step               = flag.Duration("step", time.Second, "duration of one simulation step")
		propertyBucketSize = flag.Duration("property-bucket", 5*time.Minute, "property VarLeakyBucket interval")
		base               = flag.Float64("base", 100, "base difficulty")
		growthText         = flag.String("growth", "normal", "difficulty growth: constant, slow, normal, fast")
		formulaText        = flag.String("formula", "v2", "formula to plot as difficulty: v1/legacy or v2")
		scenario           = flag.String("scenario", "both-burst", "preset: steady, user-burst, property-spike, property-long-spike, both-burst")
		seed               = flag.Int64("seed", 1, "random seed")
		debug              = flag.Bool("debug", false, "append diagnostic formula/bucket columns to the TSV output")

		enableUser              = flag.Bool("user", true, "enable selected requester/user traffic")
		enableProperty          = flag.Bool("property", true, "enable background property traffic")
		userContributesProperty = flag.Bool("user-contributes-property", true, "also add selected requester traffic to the property bucket")

		userDistText       = flag.String("user-dist", "", "override user distribution: const, poisson, burst")
		userRate           = flag.Float64("user-rate", -1, "override selected requester expected requests per step")
		userBurstRate      = flag.Float64("user-burst-rate", -1, "override selected requester burst expected requests per step")
		userBurstStart     = flag.Int("user-burst-start", -1, "override selected requester burst start step")
		userBurstEnd       = flag.Int("user-burst-end", -1, "override selected requester burst end step")
		propertyDistText   = flag.String("property-dist", "", "override property distribution: const, poisson, burst")
		propertyRate       = flag.Float64("property-rate", -1, "override property expected requests per step")
		propertyBurstRate  = flag.Float64("property-burst-rate", -1, "override property burst expected requests per step")
		propertyBurstStart = flag.Int("property-burst-start", -1, "override property burst start step")
		propertyBurstEnd   = flag.Int("property-burst-end", -1, "override property burst end step")

		userRef        = flag.Float64("user-ref", 8, "V2: U8 user level that corresponds to about +8 difficulty before weights")
		minExpectedRPS = flag.Float64("min-expected-rps", 0.5, "V2: minimum expected property requests/sec used to derive the smooth P8 floor from property bucket size")
		userWeight     = flag.Float64("user-weight", 1.0, "V2: requester/user signal weight")
		propertyWeight = flag.Float64("property-weight", 0.25, "V2: property signal weight")
		crossWeight    = flag.Float64("cross-weight", 0.0, "V2: optional user*property interaction weight")
	)
	flag.Float64Var(&propertyK, "property-k", 4, "V2: K in P8=sqrt((minExpectedRPS*bucketSize)^2 + (K*leakRate)^2), measured in normal property buckets")
	flag.Float64Var(&propertyK, "k", 4, "alias for --property-k")
	flag.Parse()

	growth, err := parseGrowth(*growthText)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	formula, err := parseFormula(*formulaText)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	cfg := difficulty.SimConfig{
		Steps:                   *steps,
		Step:                    *step,
		PropertyBucketSize:      *propertyBucketSize,
		BaseDifficulty:          *base,
		Growth:                  growth,
		Formula:                 formula,
		EnableUser:              *enableUser,
		EnableProperty:          *enableProperty,
		UserContributesProperty: *userContributesProperty,
		Seed:                    *seed,
		Debug:                   *debug,
		FormulaConfig: difficulty.SimFormulaConfig{
			UserRef:            *userRef,
			MinExpectedRPS:     *minExpectedRPS,
			PropertyRefBuckets: propertyK,
			UserWeight:         *userWeight,
			PropertyWeight:     *propertyWeight,
			CrossWeight:        *crossWeight,
		},
	}
	applyScenario(*scenario, &cfg)

	if *userDistText != "" {
		cfg.UserModel.Dist, err = parseDist(*userDistText)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if *propertyDistText != "" {
		cfg.PropertyModel.Dist, err = parseDist(*propertyDistText)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if *userRate >= 0 {
		cfg.UserModel.Rate = *userRate
	}
	if *userBurstRate >= 0 {
		cfg.UserModel.BurstRate = *userBurstRate
	}
	if *userBurstStart >= 0 {
		cfg.UserModel.BurstStart = *userBurstStart
	}
	if *userBurstEnd >= 0 {
		cfg.UserModel.BurstEnd = *userBurstEnd
	}
	if *propertyRate >= 0 {
		cfg.PropertyModel.Rate = *propertyRate
	}
	if *propertyBurstRate >= 0 {
		cfg.PropertyModel.BurstRate = *propertyBurstRate
	}
	if *propertyBurstStart >= 0 {
		cfg.PropertyModel.BurstStart = *propertyBurstStart
	}
	if *propertyBurstEnd >= 0 {
		cfg.PropertyModel.BurstEnd = *propertyBurstEnd
	}

	if err := difficulty.RunSimulation(os.Stdout, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
