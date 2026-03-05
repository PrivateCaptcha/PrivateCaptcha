package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"net/http"
	"os"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	common_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func loadProperties(count int, cfg common.ConfigStore) ([]*dbgen.Property, error) {
	ctx := context.TODO()

	pool, clickhouse, dberr := db.Connect(ctx, cfg, 5*time.Second, false /*admin*/, nil)
	if dberr != nil {
		return nil, dberr
	}

	defer pool.Close()
	/*defer*/ clickhouse.Close()

	businessDB := db.NewBusiness(pool)

	properties, err := businessDB.Impl().RetrieveProperties(ctx, count)
	if err != nil {
		return nil, err
	}

	slog.Info("Fetched properties", "count", len(properties))

	return properties, nil
}

func loadPropertiesEx(count int, cfg common.ConfigStore) (map[[16]byte]*dbgen.Property, map[int32]*dbgen.APIKey, error) {
	ctx := context.TODO()

	pool, clickhouse, dberr := db.Connect(ctx, cfg, 5*time.Second, false /*admin*/, nil)
	if dberr != nil {
		return nil, nil, dberr
	}

	defer pool.Close()
	/*defer*/ clickhouse.Close()

	businessDB := db.NewBusiness(pool)

	properties, err := businessDB.Impl().RetrieveProperties(ctx, count)
	if err != nil {
		return nil, nil, err
	}

	slog.Info("Fetched properties", "count", len(properties))

	loginExternalID := db.UUIDFromSiteKey(db.PortalLoginSitekey)
	registerExternalID := db.UUIDFromSiteKey(db.PortalRegisterSitekey)

	user2apiKeyMap := make(map[int32]*dbgen.APIKey)
	external2propertyMap := make(map[[16]byte]*dbgen.Property)
	for _, property := range properties {
		if bytes.Equal(property.ExternalID.Bytes[:], loginExternalID.Bytes[:]) ||
			bytes.Equal(property.ExternalID.Bytes[:], registerExternalID.Bytes[:]) {
			continue
		}

		external2propertyMap[property.ExternalID.Bytes] = property
		userID := property.CreatorID.Int32

		if _, ok := user2apiKeyMap[userID]; ok {
			continue
		}

		if keys, err := businessDB.Impl().RetrieveUserAPIKeys(ctx, userID); err == nil {
			if len(keys) > 1 {
				slog.Error("More than 1 API key found", "userID", userID)
			}
			// each user HAS to have at least 1 API key per seed()
			user2apiKeyMap[userID] = keys[0]
		} else {
			slog.Error("Failed to fetch user API keys", "userID", userID, common.ErrAttr(err))
		}
	}

	return external2propertyMap, user2apiKeyMap, nil
}

func loadProperty(cfg common.ConfigStore) (*dbgen.Property, *dbgen.APIKey, error) {
	ctx := context.TODO()

	pool, clickhouse, dberr := db.Connect(ctx, cfg, 5*time.Second, false /*admin*/, nil)
	if dberr != nil {
		return nil, nil, dberr
	}

	defer pool.Close()
	/*defer*/ clickhouse.Close()

	businessDB := db.NewBusiness(pool)

	properties, err := businessDB.Impl().RetrieveProperties(ctx, 10 /*limit*/)
	if err != nil {
		return nil, nil, err
	}

	slog.Info("Fetched properties", "count", len(properties))

	loginExternalID := db.UUIDFromSiteKey(db.PortalLoginSitekey)
	registerExternalID := db.UUIDFromSiteKey(db.PortalRegisterSitekey)

	for _, property := range properties {
		if bytes.Equal(property.ExternalID.Bytes[:], loginExternalID.Bytes[:]) ||
			bytes.Equal(property.ExternalID.Bytes[:], registerExternalID.Bytes[:]) {
			continue
		}

		userID := property.CreatorID.Int32

		if keys, err := businessDB.Impl().RetrieveUserAPIKeys(ctx, userID); err == nil {
			if len(keys) > 1 {
				slog.Error("More than 1 API key found", "userID", userID)
			}
			// each user HAS to have at least 1 API key per seed()
			return property, keys[0], nil
		}
	}

	return nil, nil, errors.New("valid data was not found")
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
		header.Add(rateLimitHeader, common_test.GenerateRandomIPv4())
		tgt.Header = header

		return nil
	}
}

func loadPuzzle(usersCount int, cfg common.ConfigStore, freq int, durationSeconds int, sitekeyPercent int) error {
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

type solutionPayloadPair struct {
	solution []byte
	payload  *puzzle.VerifyPayload
}

func verifyTargeter(pairs []*solutionPayloadPair, propertyMap map[[16]byte]*dbgen.Property, apiKeyMap map[int32]*dbgen.APIKey, cfg common.ConfigStore) vegeta.Targeter {
	rateLimitHeader := cfg.Get(common.RateLimitHeaderKey).Value()

	return func(tgt *vegeta.Target) error {
		if tgt == nil {
			return vegeta.ErrNilTarget
		}

		found := false
		var pair *solutionPayloadPair
		var apiKey *dbgen.APIKey

		for !found {
			pair = pairs[randv2.IntN(len(pairs))]

			p := pair.payload.Puzzle()
			externalID := p.PropertyID()
			property, ok := propertyMap[externalID]
			if !ok {
				slog.Error("Property for solution not found", "propertyID", hex.EncodeToString(externalID[:]))
				continue
			}

			apiKey, ok = apiKeyMap[property.CreatorID.Int32]
			if !ok {
				slog.Error("API Key not found for user", "userID", property.CreatorID.Int32)
				continue
			}

			found = true
		}

		tgt.Method = http.MethodPost

		apiURLConfig := config.AsURL(context.TODO(), cfg.Get(common.APIBaseURLKey))
		tgt.URL = fmt.Sprintf("http:%s/%s", apiURLConfig.URL(), common.VerifyEndpoint)
		tgt.Body = pair.solution

		header := http.Header{}
		header.Add(rateLimitHeader, common_test.GenerateRandomIPv4())
		header.Add(common.HeaderAPIKey, db.UUIDToSecret(apiKey.ExternalID))
		tgt.Header = header

		return nil
	}
}

func readSolutionPairs(ctx context.Context, solutionsFile string) ([]*solutionPayloadPair, error) {
	file, err := os.Open(solutionsFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	pairs := make([]*solutionPayloadPair, 0, 1000)
	count := 0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
		pair := &solutionPayloadPair{
			solution: []byte(scanner.Text()),
		}

		pair.payload, err = puzzle.ParseVerifyPayload[puzzle.ComputePuzzle](ctx, pair.solution)
		if err == nil {
			pairs = append(pairs, pair)
		} else {
			slog.Error("Failed to parse solution from file", "line", count, common.ErrAttr(err))
		}
	}

	slog.Debug("Read solutions from file", "lines", count, "parsed", len(pairs))

	return pairs, scanner.Err()
}

func loadVerify(usersCount int, solutionsFile string, cfg common.ConfigStore, freq int, durationSeconds int) error {
	pairs, err := readSolutionPairs(context.TODO(), solutionsFile)
	if err != nil {
		return err
	}

	propertiesMap, apiKeyMap, err := loadPropertiesEx(usersCount, cfg)
	if err != nil {
		return err
	}

	rate := vegeta.Rate{Freq: freq, Per: time.Second}
	duration := time.Duration(durationSeconds) * time.Second
	targeter := verifyTargeter(pairs, propertiesMap, apiKeyMap, cfg)
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

func stubVerifyTargeter(host string, apiKey string, cfg common.ConfigStore) vegeta.Targeter {
	rateLimitHeader := cfg.Get(common.RateLimitHeaderKey).Value()

	return func(tgt *vegeta.Target) error {
		if tgt == nil {
			return vegeta.ErrNilTarget
		}

		tgt.Method = http.MethodPost

		apiURLConfig := config.AsURL(context.TODO(), cfg.Get(common.APIBaseURLKey))
		tgt.URL = fmt.Sprintf("http:%s/%s", apiURLConfig.URL(), common.VerifyEndpoint)
		tgt.Body = []byte("AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.Aaqqqqq7u8zM3d3u7u7u7u4AAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAAAAAAAA=.AQCiRYnFLBXoqfEYUz7Up+ktTXhxXgw=")

		header := http.Header{}
		if len(rateLimitHeader) > 0 {
			header.Add(rateLimitHeader, common_test.GenerateRandomIPv4())
		}
		if len(apiKey) > 0 {
			header.Add(common.HeaderAPIKey, apiKey)
		}
		if len(host) > 0 {
			header.Add("Host", host)
		}
		tgt.Header = header

		return nil
	}
}

func loadVerifyStub(host, apiKey string, cfg common.ConfigStore, freq int, durationSeconds int, insecure bool) error {
	rate := vegeta.Rate{Freq: freq, Per: time.Second}
	duration := time.Duration(durationSeconds) * time.Second

	targeter := stubVerifyTargeter(host, apiKey, cfg)
	opts := make([]func(*vegeta.Attacker), 0)
	if insecure {
		opts = append(opts, vegeta.TLSConfig(&tls.Config{
			InsecureSkipVerify: true,
		}))
	}
	attacker := vegeta.NewAttacker(opts...)

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
