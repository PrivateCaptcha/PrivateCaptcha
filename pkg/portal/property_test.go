package portal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func TestPutPropertyInsufficientPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	_, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1")
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a new property
	property, err := server.Store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       "propertyName",
		OrgID:      db.Int(org1.ID),
		CreatorID:  org1.UserID,
		OrgOwnerID: org1.UserID,
		Domain:     "example.com",
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	// Create another user account
	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2")
	if err != nil {
		t.Fatalf("Failed to create intruder account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(srv, fakeRateLimiter)

	cookie, err := authenticateSuite(ctx, user2.Email, srv)
	if err != nil {
		t.Fatal(err)
	}

	// Send PUT request as the second user to update the property
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(user2.Email))
	form.Set(common.ParamName, "Updated Property Name")
	form.Set(common.ParamDifficulty, "0")
	form.Set(common.ParamGrowth, "2")

	req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%d/property/%d/edit", org1.ID, property.ID),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	expectedError := "Insufficient permissions to update settings."
	if !strings.Contains(string(body), expectedError) {
		t.Error("Expected response body to contain permissions error")
	}
}
