package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"net/http"
	"os"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func loadProperties(count int, cfg common.ConfigStore) ([]*dbgen.Property, error) {
	ctx := context.TODO()

	pool, clickhouse, dberr := db.Connect(ctx, cfg)
	if dberr != nil {
		return nil, dberr
	}

	defer pool.Close()
	/*defer*/ clickhouse.Close()

	businessDB := db.NewBusiness(pool)

	properties, err := businessDB.RetrieveProperties(ctx, count)
	if err != nil {
		return nil, err
	}

	slog.Info("Fetched properties", "count", len(properties))

	return properties, nil
}

func generateRandomIPv4() string {
	// Generate a random 32-bit integer
	ipInt := randv2.Uint32()
	// Extract each byte and format as IP address
	return fmt.Sprintf("%d.%d.%d.%d",
		(ipInt>>24)&0xFF,
		(ipInt>>16)&0xFF,
		(ipInt>>8)&0xFF,
		ipInt&0xFF)
}

func randomSiteKey() string {
	array := make([]byte, 16)

	for i := range array {
		array[i] = byte(randv2.Int())
	}

	return hex.EncodeToString(array[:])
}

func puzzleTargeter(properties []*dbgen.Property, sitekeyPercent int, cfg common.ConfigStore) vegeta.Targeter {
	rateLimitHeader := cfg.Get(common.RateLimitHeaderKey).Value()

	return func(tgt *vegeta.Target) error {
		if tgt == nil {
			return vegeta.ErrNilTarget
		}

		tgt.Method = http.MethodGet

		var sitekey string
		property := properties[randv2.IntN(len(properties))]

		// in sitekeyPercent % of cases, we want to send valid sitekey
		// - if sitekeyPercent is 100, then 100 is always >= (rand() % 100)
		// - if sitekeyPercent is 0, then we always send invalid
		if sitekeyPercent >= randv2.IntN(100) {
			sitekey = db.UUIDToSiteKey(property.ExternalID)
		} else {
			sitekey = randomSiteKey()
		}

		apiURLConfig := config.AsURL(context.TODO(), cfg.Get(common.APIBaseURLKey))
		tgt.URL = fmt.Sprintf("http:%s/%s?%s=%s", apiURLConfig.URL(), common.PuzzleEndpoint, common.ParamSiteKey, sitekey)

		header := http.Header{}
		header.Add("Origin", property.Domain)
		header.Add(rateLimitHeader, generateRandomIPv4())
		tgt.Header = header

		return nil
	}
}

func load(usersCount int, cfg common.ConfigStore, freq int, durationSeconds int, sitekeyPercent int) error {
	properties, err := loadProperties(usersCount, cfg)
	if err != nil {
		return err
	}

	rate := vegeta.Rate{Freq: freq, Per: time.Second}
	duration := time.Duration(durationSeconds) * time.Second
	targeter := puzzleTargeter(properties, sitekeyPercent, cfg)
	attacker := vegeta.NewAttacker()

	slog.Info("Attacking", "duration", duration.String(), "rate", rate.String())

	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, rate, duration, "Big Bang!") {
		metrics.Add(res)
	}
	metrics.Close()

	reporter := vegeta.NewTextReporter(&metrics)
	reporter(os.Stdout)

	return nil
}
