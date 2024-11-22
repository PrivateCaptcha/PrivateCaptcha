package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

const (
	modeSeed = "seed"
	modeTest = "test"
)

var (
	envFileFlag         = flag.String("env", "", "Path to .env file")
	flagMode            = flag.String("mode", "", strings.Join([]string{modeSeed, modeTest}, " | "))
	flagUsersCount      = flag.Int("user-count", 100, "number of users to seed")
	flagOrgsCount       = flag.Int("org-count", 10, "number of orgs to seed")
	flagPropertiesCount = flag.Int("property-count", 100, "number of properties to seed")
	flagRatePerSecond   = flag.Int("rps", 100, "Requests per second")
	flagDuration        = flag.Int("duration", 10, "Duration of the load test (seconds)")
	flagSitekeyPercent  = flag.Int("sitekey-percent", 100, "Percent of valid sitekey requests")
)

func main() {
	flag.Parse()

	var err error

	if len(*envFileFlag) > 0 {
		err = godotenv.Load(*envFileFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
	}

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	switch *flagMode {
	case modeSeed:
		err = seed(*flagUsersCount, *flagOrgsCount, *flagPropertiesCount, os.Getenv)
	case modeTest:
		err = load((*flagUsersCount)*(*flagOrgsCount)*(*flagPropertiesCount), os.Getenv, *flagRatePerSecond, *flagDuration,
			*flagSitekeyPercent)
	default:
		err = fmt.Errorf("unknown mode: '%s'", *flagMode)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
