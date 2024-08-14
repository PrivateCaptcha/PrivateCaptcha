package api

import (
	"context"
	"encoding/base64"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
)

func puzzleSuite(sitekey string) (*http.Response, error) {
	srv := http.NewServeMux()
	s.Setup(srv, "")

	//srv.HandleFunc("/", catchAll)

	req, err := http.NewRequest(http.MethodGet, "/"+common.PuzzleEndpoint, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add(common.ParamSiteKey, sitekey)
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	return resp, nil
}

func randomUUID() *pgtype.UUID {
	eid := &pgtype.UUID{Valid: true}

	for i := range eid.Bytes {
		eid.Bytes[i] = byte(rand.Int())
	}

	return eid
}

func puzzleSuiteWithBackfillWait(t *testing.T, sitekey string) {
	resp, err := puzzleSuite(sitekey)
	if err != nil {
		t.Fatal(err)
	}

	// first request is successful, until we backfill
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	time.Sleep(3 * authBackfillDelay)

	resp, err = puzzleSuite(sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}
}

func TestGetPuzzleWithoutAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	sitekey := db.UUIDToSiteKey(*randomUUID())

	puzzleSuiteWithBackfillWait(t, sitekey)
}

func TestGetPuzzleWithoutSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := context.TODO()

	user, org, err := db_test.CreateNewBareAccount(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, err := store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       t.Name(),
		OrgID:      db.Int(org.ID),
		CreatorID:  db.Int(user.ID),
		OrgOwnerID: db.Int(user.ID),
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	if err := cache.Delete(ctx, db.PropertyBySitekeyCacheKey(sitekey)); err != nil {
		t.Fatal(err)
	}

	puzzleSuiteWithBackfillWait(t, sitekey)
}

func TestGetPuzzleWithCancelledSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

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
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	if err := cache.Delete(ctx, db.PropertyBySitekeyCacheKey(sitekey)); err != nil {
		t.Fatal(err)
	}

	if err := db_test.CancelUserSubscription(ctx, store, user.ID); err != nil {
		t.Fatal(err)
	}

	puzzleSuiteWithBackfillWait(t, sitekey)
}

func fetchPuzzle(resp *http.Response) (*puzzle.Puzzle, string, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	responseStr := string(body)
	puzzleStr, _, _ := strings.Cut(responseStr, ".")
	decodedData, err := base64.StdEncoding.DecodeString(puzzleStr)
	if err != nil {
		return nil, "", err
	}

	p := new(puzzle.Puzzle)
	err = p.UnmarshalBinary(decodedData)
	if err != nil {
		return nil, "", err
	}

	return p, responseStr, nil
}

func TestGetPuzzle(t *testing.T) {
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
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := puzzleSuite(db.UUIDToSiteKey(property.ExternalID))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	p, _, err := fetchPuzzle(resp)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Valid() {
		t.Errorf("Response puzzle is not valid")
	}
}

// setup is the same as for successful test, but we tombstone key in cache
func TestPuzzleCachePriority(t *testing.T) {
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
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	err = cache.SetMissing(ctx, db.PropertyBySitekeyCacheKey(sitekey))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := puzzleSuite(sitekey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}
}
