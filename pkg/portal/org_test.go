package portal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func TestGetAnotherUsersOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	_, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1")
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create another user account
	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2")
	if err != nil {
		t.Fatalf("Failed to create intruder account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(srv, portalDomain(), common.NoopMiddleware)

	cookie, err := authenticateSuite(ctx, user2.Email, srv)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%d/%s/%s", org1.ID, common.TabEndpoint, common.DashboardEndpoint), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	url, _ := resp.Location()
	if path := url.String(); !strings.HasPrefix(path, "/"+common.ErrorEndpoint) {
		t.Errorf("Unexpected redirect: %s", path)
	}
}
