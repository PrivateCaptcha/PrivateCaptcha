package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/utils"
)

func verifySuite(result string) (*http.Response, error) {
	srv := http.NewServeMux()
	server.Setup(srv)

	//srv.HandleFunc("/", catchAll)

	req, err := http.NewRequest(http.MethodPost, common.VerifyEndpoint, strings.NewReader(result))
	if err != nil {
		return nil, err
	}

	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

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

	property, err := setupProperty(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	resp, err := puzzleSuite(utils.UUIDToSiteKey(property.ExternalID))
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
	resp, err = verifySuite(fmt.Sprintf("%s.%s", solutionsStr, puzzleStr))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected submit status code %d", resp.StatusCode)
	}
}
