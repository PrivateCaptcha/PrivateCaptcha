package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/joho/godotenv"
)

var (
	GitCommit   string
	envFileFlag = flag.String("env", "", "Path to .env file")
)

func main() {
	flag.Parse()

	if len(*envFileFlag) > 0 {
		_ = godotenv.Load(*envFileFlag)
	}

	cfg, err := config.New(os.Getenv)
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
