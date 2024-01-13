package api

import (
	"context"
	"fmt"
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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

func verifySuite(response, secret string) (*http.Response, error) {
	srv := http.NewServeMux()
	server.Setup(srv)

	//srv.HandleFunc("/", catchAll)

	data := url.Values{}
	data.Set(common.ParamSecret, secret)
	data.Set(common.ParamResponse, response)

	encoded := data.Encode()

	req, err := http.NewRequest(http.MethodPost, common.VerifyEndpoint, strings.NewReader(encoded))
	if err != nil {
		return nil, err
	}

	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Add(common.HeaderContentLength, strconv.Itoa(len(encoded)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	return resp, nil
}

func TestVerifyPuzzle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()

	user, err := queries.CreateUser(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, err := queries.CreateProperty(ctx, db.Int(user.ID))
	if err != nil {
		t.Fatal(err)
	}

	apikey, err := queries.CreateAPIKey(ctx, db.Int(user.ID))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := puzzleSuite(db.UUIDToSiteKey(property.ExternalID))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected puzzle status code %d", resp.StatusCode)
	}

	p, puzzleStr, err := fetchPuzzle(resp)
	if err != nil {
		t.Fatal(err)
	}

	solver := &puzzle.Solver{}
	solutions, err := solver.Solve(p)
	if err != nil {
		t.Fatal(err)
	}
	solutionsStr := solutions.String()
	resp, err = verifySuite(fmt.Sprintf("%s.%s", solutionsStr, puzzleStr), db.UUIDToSecret(apikey.ExternalID))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
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

	user, err := queries.CreateUser(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	apikey, err := queries.CreateAPIKey(ctx, db.Int(user.ID))
	if err != nil {
		t.Fatal(err)
	}

	err = queries.UpdateAPIKey(ctx, &dbgen.UpdateAPIKeyParams{
		ExpiresAt:  db.Timestampz(time.Now().AddDate(0, 0, -1)),
		ExternalID: apikey.ExternalID,
		Enabled:    db.Bool(true),
	})
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
