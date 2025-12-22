package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	common_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

func TestSerializeResponse(t *testing.T) {
	v := VerifyResponseRecaptchaV3{
		VerifyResponseRecaptchaV2: VerifyResponseRecaptchaV2{
			Success:     false,
			ErrorCodes:  []string{puzzle.VerifyErrorOther.String()},
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

func verifySuite(response, secret, sitekey string) (*http.Response, error) {
	srv := http.NewServeMux()
	s.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	//srv.HandleFunc("/", catchAll)

	req, err := http.NewRequest(http.MethodPost, "/"+common.VerifyEndpoint, strings.NewReader(response))
	if err != nil {
		return nil, err
	}

	req.Header.Set(common.HeaderAPIKey, secret)
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())
	if len(sitekey) > 0 {
		req.Header.Set(common.HeaderSitekey, sitekey)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	return resp, nil
}

func siteVerifySuite(response, secret, sitekey string) (*http.Response, error) {
	srv := http.NewServeMux()
	s.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	//srv.HandleFunc("/", catchAll)

	data := url.Values{}
	data.Set(common.ParamSecret, secret)
	data.Set(common.ParamResponse, response)
	if len(sitekey) > 0 {
		data.Set(common.ParamSiteKey, sitekey)
	}

	encoded := data.Encode()

	req, err := http.NewRequest(http.MethodPost, "/"+common.SiteVerifyEndpoint, strings.NewReader(encoded))
	if err != nil {
		return nil, err
	}

	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	return resp, nil
}

func solutionsSuite(ctx context.Context, sitekey, domain string) (string, string, error) {
	resp, err := puzzleSuite(ctx, sitekey, domain)
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

	solver := &puzzle.ComputeSolver{}
	solutions, err := solver.Solve(p)
	if err != nil {
		return puzzleStr, "", err
	}

	return puzzleStr, solutions.String(), nil
}

func setupVerifySuite(ctx context.Context, username string, scope dbgen.ApiKeyScope) (string, string, string, error) {
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, username, testPlan)
	if err != nil {
		return "", "", "", err
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		return "", "", "", err
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey, property.Domain)
	if err != nil {
		return "", "", "", err
	}

	keyParams := tests.CreateNewPuzzleAPIKeyParams(username+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/)
	keyParams.Scope = scope
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		return "", "", "", err
	}

	return fmt.Sprintf("%s.%s", solutionsStr, puzzleStr), db.UUIDToSecret(apikey.ExternalID), sitekey, nil
}

func TestVerifyPuzzle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite(payload, apiKey, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestVerifyAnotherOrgScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := t.Context()

	user, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org1)
	if err != nil {
		t.Fatal(err)
	}

	puzzleStr, solutionsStr, err := solutionsSuite(ctx, db.UUIDToSiteKey(property.ExternalID), property.Domain)
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf("%s.%s", solutionsStr, puzzleStr)

	org2, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	params := tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/)
	params.OrgID = db.Int(org2.ID)
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, params)
	if err != nil {
		t.Fatal(err)
	}

	secret := db.UUIDToSecret(apikey.ExternalID)
	sitekey := db.UUIDToSiteKey(property.ExternalID)
	resp, err := verifySuite(payload, secret, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected verify status code %d", resp.StatusCode)
	}

	if err := checkVerifyError(resp, puzzle.OrgScopeError); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyInvalidAPIKeyScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePortal)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite(payload, apiKey, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestVerifyPuzzleWrongExpectedSitekey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, _, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite(payload, apiKey, db.UUIDToSiteKey(*randomUUID()))
	if err != nil {
		t.Fatal(err)
	}

	if err := checkVerifyError(resp, puzzle.InvalidPropertyError); err != nil {
		t.Fatal(err)
	}
}

func TestSiteVerifyPuzzle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := siteVerifySuite(payload, apiKey, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestSiteVerifyWrongExpectedSitekey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, _, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := siteVerifySuite(payload, apiKey, db.UUIDToSiteKey(*randomUUID()))
	if err != nil {
		t.Fatal(err)
	}

	if err := checkSiteVerifyError(resp, puzzle.InvalidPropertyError); err != nil {
		t.Fatal(err)
	}
}

func checkSiteVerifyError(resp *http.Response, expected puzzle.VerifyError) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	response := &VerifyResponseRecaptchaV2{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return err
	}

	if expected == puzzle.VerifyNoError {
		if !response.Success {
			return errors.New("expected successful verification")
		}

		if len(response.ErrorCodes) > 0 {
			return errors.New("error code present in response")
		}
	} else {
		if len(response.ErrorCodes) == 0 {
			return errors.New("no error code in response")
		}

		if response.ErrorCodes[0] != expected.String() {
			return fmt.Errorf("Unexpected error code: %v", response.ErrorCodes[0])
		}
	}

	return nil
}

func checkVerifyError(resp *http.Response, expected puzzle.VerifyError) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	response := &VerificationResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return err
	}

	if expected == puzzle.VerifyNoError {
		if !response.Success {
			return errors.New("expected successful verification")
		}

		if response.Code != 0 {
			return errors.New("error code present in response")
		}
	} else {
		if response.Code == 0 {
			return errors.New("no error code in response")
		}

		if response.Code != expected {
			return fmt.Errorf("Unexpected error code: %v", response.Code.String())
		}
	}

	return nil
}

func TestVerifyPuzzleReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite(payload, apiKey, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}

	// now second time the same
	resp, err = verifySuite(payload, apiKey, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if err := checkVerifyError(resp, puzzle.VerifiedBeforeError); err != nil {
		t.Fatal(err)
	}
}

// in this case "Site" part of the SiteVerify (reCAPTCHA compatibility) does not bring anything else
// except of checking _any_ error in the reCAPTCHA format
func TestSiteVerifyPuzzleReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := siteVerifySuite(payload, apiKey, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}

	// now second time the same
	resp, err = siteVerifySuite(payload, apiKey, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if err := checkSiteVerifyError(resp, puzzle.VerifiedBeforeError); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPuzzleAllowReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	payload, apiKey, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	property, err := store.Impl().GetCachedPropertyBySitekey(t.Context(), sitekey, nil)
	if err != nil {
		t.Fatal(err)
	}
	const maxReplayCount = 3
	// this should be still cached so we don't need to actually update DB
	property.MaxReplayCount = maxReplayCount

	for _ = range maxReplayCount {
		resp, err := verifySuite(payload, apiKey, sitekey)
		if err != nil {
			t.Fatal(err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Unexpected submit status code %d", resp.StatusCode)
		}

		if err := checkVerifyError(resp, puzzle.VerifyNoError); err != nil {
			t.Fatal(err)
		}
	}

	// now it should trigger an error
	resp, err := verifySuite(payload, apiKey, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
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

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	puzzleStr, solutionsStr, err := solutionsSuite(ctx, db.UUIDToSiteKey(property.ExternalID), property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	apiKeyID := randomUUID()
	secret := db.UUIDToSecret(*apiKeyID)
	sitekey := db.UUIDToSiteKey(property.ExternalID)

	cache.SetMissing(ctx, db.APIKeyCacheKey(secret))

	resp, err := verifySuite(fmt.Sprintf("%s.%s", solutionsStr, puzzleStr), secret, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestVerifyInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	payload, _, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite(payload, db.UUIDToSecret(*randomUUID()), sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestSiteVerifyInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	payload, _, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := siteVerifySuite(payload, db.UUIDToSecret(*randomUUID()), sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestVerifyExpiredKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/))
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Impl().UpdateAPIKey(ctx, user, apikey, time.Now().AddDate(0, 0, -1), true)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := verifySuite("a.b.c", db.UUIDToSecret(apikey.ExternalID), "")
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestVerifyMaintenanceMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// NOTE: this test cannot be run in parallel as it modifies the global DB state (maintenance mode)
	// t.Parallel()

	payload, apiKey, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
	if err != nil {
		t.Fatal(err)
	}

	cacheKey := db.PropertyBySitekeyCacheKey(sitekey)
	cache.Delete(t.Context(), cacheKey)

	store.UpdateConfig(true /*maintenance mode*/)
	defer store.UpdateConfig(false /*maintenance mode*/)

	resp, err := verifySuite(payload, apiKey, sitekey)
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

func verifyTestPropertySuite(t *testing.T, verifySitekey string, expectedCode puzzle.VerifyError) {
	ctx := t.Context()

	puzzleStr, solutionsStr, err := solutionsSuite(ctx, db.TestPropertySitekey, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf("%s.%s", solutionsStr, puzzleStr)

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/))
	if err != nil {
		t.Fatal(err)
	}

	secret := db.UUIDToSecret(apikey.ExternalID)

	resp, err := verifySuite(payload, secret, verifySitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected verify status code %d", resp.StatusCode)
	}

	if err := checkVerifyError(resp, expectedCode); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyTestProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	verifyTestPropertySuite(t, "" /*verify sitekey*/, puzzle.TestPropertyError)
}

func TestVerifyTestPropertyAgainstSitekey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	verifyTestPropertySuite(t, db.UUIDToSiteKey(*randomUUID()), puzzle.InvalidPropertyError)
}

func TestVerifyTestShortcut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	solver := &puzzle.ComputeSolver{}
	solutions, _ := solver.Solve(s.Verifier.TestPuzzle)

	var buf bytes.Buffer

	buf.WriteString(solutions.String())
	buf.Write([]byte("."))
	s.Verifier.WriteTestPuzzle(&buf)

	payload, err := s.Verifier.ParseSolutionPayload(ctx, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if payload.Puzzle() != s.Verifier.TestPuzzle {
		t.Fatal("verify result is not short circuited")
	}

	if result, _ := s.Verifier.Verify(ctx, payload, nil /*expectedOwner*/, time.Now().UTC()); result.Error != puzzle.TestPropertyError {
		t.Errorf("Unexpected verification result: %v", result.Error.String())
	}
}

func TestVerifyByOrgMember(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	puzzleStr, solutionsStr, err := solutionsSuite(ctx, db.UUIDToSiteKey(property.ExternalID), property.Domain)
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf("%s.%s", solutionsStr, puzzleStr)

	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)

	params := tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/)
	params.OrgID = db.Int(org.ID)
	apikey, _, err := store.Impl().CreateAPIKey(ctx, member, params)
	if err != nil {
		t.Fatal(err)
	}

	secret := db.UUIDToSecret(apikey.ExternalID)
	sitekey := db.UUIDToSiteKey(property.ExternalID)

	resp, err := verifySuite(payload, secret, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected verify status code %d", resp.StatusCode)
	}

	if err := checkVerifyError(resp, puzzle.WrongOwnerError); err != nil {
		t.Fatal(err)
	}

	// join the org
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatal(err)
	}

	// now, after we join the org, should be OK
	resp, err = verifySuite(payload, secret, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected verify status code %d", resp.StatusCode)
	}

	if err := checkVerifyError(resp, puzzle.VerifyNoError); err != nil {
		t.Fatal(err)
	}
}
