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

	cfg, err := config.New(env.Get)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	paddleAPI, err := billing.NewPaddleAPI(cfg.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}

	products := billing.GetProductsForStage(cfg.Stage())
	prices, err := paddleAPI.GetPrices(context.TODO(), products)
	if err == nil {
		fmt.Printf("Fetched prices: %v", prices)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
}
