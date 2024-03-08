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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
)

func puzzleSuite(sitekey string) (*http.Response, error) {
	srv := http.NewServeMux()
	server.Setup(srv)

	//srv.HandleFunc("/", catchAll)

	req, err := http.NewRequest(http.MethodGet, common.PuzzleEndpoint, nil)
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

func TestGetPuzzleUnauthorized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	resp, err := puzzleSuite(db.UUIDToSiteKey(*randomUUID()))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}
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

	user, err := queries.CreateUser(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	org, err := queries.CreateOrganization(ctx, &dbgen.CreateOrganizationParams{UserID: db.Int(user.ID), OrgName: t.Name()})
	if err != nil {
		t.Fatal(err)
	}

	property, err := queries.CreateProperty(ctx, db.Int(org.ID))
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

	user, err := queries.CreateUser(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	org, err := queries.CreateOrganization(ctx, &dbgen.CreateOrganizationParams{UserID: db.Int(user.ID), OrgName: t.Name()})
	if err != nil {
		t.Fatal(err)
	}

	property, err := queries.CreateProperty(ctx, db.Int(org.ID))
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	err = cache.SetMissing(ctx, sitekey, 1*time.Minute)
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
