package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
)

var (
	GitCommit   string
	envFileFlag = flag.String("env", "", "Path to .env file, 'stdin' or empty")
	env         *common.EnvMap
)

func main() {
	flag.Parse()

	var err error
	env, err = common.NewEnvMap(*envFileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	cfg := config.NewEnvConfig(config.DefaultMapper, env.Get)

	paddleAPI, err := billing.NewPaddleAPI(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}

	products := billing.NewPlanService().GetProductsForStage(cfg.Get(common.StageKey).Value())
	prices, err := paddleAPI.GetPrices(context.TODO(), products)
	if err == nil {
		fmt.Printf("Fetched prices: %v", prices)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
}
