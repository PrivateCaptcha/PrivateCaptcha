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

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	_ "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/utils"
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

func TestGetPuzzleUnauthorized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	eid := &pgtype.UUID{Valid: true}
	for i := range eid.Bytes {
		eid.Bytes[i] = byte(rand.Int())
	}

	resp, err := puzzleSuite(utils.UUIDToSiteKey(*eid))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}
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

	property, err := queries.CreateProperty(context.TODO(), pgtype.Int4{Int32: user.ID, Valid: true})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := puzzleSuite(utils.UUIDToSiteKey(property.ExternalID))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	puzzleStr, _, _ := strings.Cut(string(body), ".")
	decodedData, err := base64.StdEncoding.DecodeString(puzzleStr)
	if err != nil {
		t.Fatalf("Failed to parse the body: %v", err)
	}

	p := new(puzzle.Puzzle)
	err = p.UnmarshalBinary(decodedData)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Valid() {
		t.Errorf("Response puzzle is not valid")
	}
}
