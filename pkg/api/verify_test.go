package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

func TestSerializeResponse(t *testing.T) {
	v := verifyResponseRecaptchaV3{
		verifyResponseRecaptchaV2: verifyResponseRecaptchaV2{
			verifyResponse: verifyResponse{
				Success:    false,
				ErrorCodes: []puzzle.VerifyError{puzzle.VerifyErrorOther},
			},
			ChallengeTS: common.JSONTimeNow(),
			Hostname:    "hostname.com",
		},
		Score:  0.5,
		Action: "action",
	}

	_, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
}

func verifySuite(response, secret string) (*http.Response, error) {
	srv := http.NewServeMux()
	s.Setup(srv, "", true /*verbose*/)

	//srv.HandleFunc("/", catchAll)

	data := url.Values{}
	data.Set(common.ParamSecret, secret)
	data.Set(common.ParamResponse, response)

	encoded := data.Encode()

	req, err := http.NewRequest(http.MethodPost, "/"+common.VerifyEndpoint, strings.NewReader(encoded))
	if err != nil {
		return nil, err
	}

	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Add(common.HeaderContentLength, strconv.Itoa(len(encoded)))
	req.Header.Set(cfg.RateLimiterHeader(), generateRandomIPv4())

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	return resp, nil
}

func solutionsSuite(ctx context.Context, sitekey, domain string) (string, string, error) {
	resp, err := puzzleSuite(sitekey, domain)
	if err != nil {
		return "", "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Unexpected puzzle status code %d", resp.StatusCode)
	}

	p, puzzleStr, err := parsePuzzle(resp)
	if err != nil {
		return puzzleStr, "", err
	}

	solver := &puzzle.Solver{}
	solutions, err := solver.Solve(p)
	if err != nil {
		return puzzleStr, "", err
	}

	return puzzleStr, solutions.String(), nil
}

func setupVerifySuite(username string) (string, string, string, error) {
	ctx := context.TODO()

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, username)
	if err != nil {
		return "", "", "", err
	}

	property, err := store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       fmt.Sprintf("%v property", username),
		OrgID:      db.Int(org.ID),
		CreatorID:  db.Int(user.ID),
		OrgOwnerID: db.Int(user.ID),
		Domain:     testPropertyDomain,
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	if err != nil {
		return "", "", "", err
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey, property.Domain)
	if err != nil {
		return "", "", "", err
	}

	apikey, err := store.CreateAPIKey(ctx, user.ID, "", time.Now().Add(1*time.Hour), 10.0 /*rps*/)
	if err != nil {
		return "", "", "", err
	}

	return fmt.Sprintf("%s.%s", solutionsStr, puzzleStr), db.UUIDToSecret(apikey.ExternalID), sitekey, nil
}

func TestVerifyPuzzle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, _, err := setupVerifySuite(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite(payload, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func checkVerifyError(resp *http.Response, expected puzzle.VerifyError) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	response := &verifyResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return err
	}

	if expected == puzzle.VerifyNoError {
		if !response.Success {
			return fmt.Errorf("Expected successful verification")
		}

		if len(response.ErrorCodes) > 0 {
			return fmt.Errorf("Error codes present in response")
		}
	} else {
		if len(response.ErrorCodes) == 0 {
			return fmt.Errorf("No error codes in response")
		}

		if response.ErrorCodes[0] != expected {
			return fmt.Errorf("Unexpected error code: %v", response.ErrorCodes[0])
		}
	}

	return nil
}

func TestVerifyPuzzleReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, _, err := setupVerifySuite(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite(payload, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}

	// now second time the same
	resp, err = verifySuite(payload, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if err := checkVerifyError(resp, puzzle.VerifiedBeforeError); err != nil {
		t.Fatal(err)
	}
}

// same as successful test (TestVerifyPuzzle), but invalidates api key in cache
func TestVerifyCachePriority(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, err := store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       t.Name(),
		OrgID:      db.Int(org.ID),
		CreatorID:  db.Int(user.ID),
		OrgOwnerID: db.Int(user.ID),
		Domain:     testPropertyDomain,
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	if err != nil {
		t.Fatal(err)
	}

	puzzleStr, solutionsStr, err := solutionsSuite(ctx, db.UUIDToSiteKey(property.ExternalID), property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	apiKeyID := randomUUID()
	secret := db.UUIDToSecret(*apiKeyID)

	cache.SetMissing(ctx, db.APIKeyCacheKey(secret), db.DefaultCacheTTL)

	resp, err := verifySuite(fmt.Sprintf("%s.%s", solutionsStr, puzzleStr), secret)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestVerifyInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	resp, err := verifySuite("a.b.c", db.UUIDToSecret(*randomUUID()))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestVerifyExpiredKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := context.TODO()

	user, _, err := db_test.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	apikey, err := store.CreateAPIKey(ctx, user.ID, "", time.Now().Add(1*time.Hour), 10.0 /*rps*/)
	if err != nil {
		t.Fatal(err)
	}

	err = store.UpdateAPIKey(ctx, apikey.ExternalID, time.Now().AddDate(0, 0, -1), true)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite("a.b.c", db.UUIDToSecret(apikey.ExternalID))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestVerifyMaintenanceMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// NOTE: this test cannot be run in parallel as it modifies the global DB state (maintenance mode)
	// t.Parallel()

	payload, apiKey, sitekey, err := setupVerifySuite(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	cacheKey := db.PropertyBySitekeyCacheKey(sitekey)
	cache.Delete(context.TODO(), cacheKey)

	store.UpdateConfig(true /*maintenance mode*/)
	defer store.UpdateConfig(false /*maintenance mode*/)

	resp, err := verifySuite(payload, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}

	if err := checkVerifyError(resp, puzzle.MaintenanceModeError); err != nil {
		t.Fatal(err)
	}
}
