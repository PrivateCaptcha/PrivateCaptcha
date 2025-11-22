package portal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func TestPutPropertyInsufficientPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	_, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a new property
	property, _, err := server.Store.Impl().CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:      "propertyName",
		CreatorID: org1.UserID,
		Domain:    "example.com",
		Level:     db.Int2(int16(common.DifficultyLevelMedium)),
		Growth:    dbgen.DifficultyGrowthMedium,
	}, org1)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	// Create another user account
	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create intruder account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	// Send PUT request as the second user to update the property
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user2.ID))))
	form.Set(common.ParamName, "Updated Property Name")
	form.Set(common.ParamDifficulty, "0")
	form.Set(common.ParamGrowth, "2")

	req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/property/%s/edit", server.IDHasher.Encrypt(int(org1.ID)), server.IDHasher.Encrypt(int(property.ID))),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

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

func TestPostNewOrgProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	propertyName := t.Name() + "Property"

	// Send POST request to create a new property
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, propertyName)
	form.Set(common.ParamDomain, "google.com")

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/new", server.IDHasher.Encrypt(int(org.ID))),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect response but got error: %v", err)
	}

	expectedPrefix := fmt.Sprintf("/org/%s/property/", server.IDHasher.Encrypt(int(org.ID)))
	if path := location.String(); !strings.HasPrefix(path, expectedPrefix) {
		t.Errorf("Unexpected redirect path: %s, expected prefix: %s", path, expectedPrefix)
	}

	pp, err := store.Impl().RetrieveOrgProperties(ctx, org)
	if err != nil {
		t.Fatal(err)
	}

	if count := len(pp); count != 1 {
		t.Errorf("Unexpected number of properties in org: %v", count)
	} else {
		if pp[0].Name != propertyName {
			t.Errorf("Unexpected property in org: %v", pp[0].Name)
		}
	}
}

func TestMoveProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	user, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create a new property
	property, _, err := server.Store.Impl().CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:      "propertyName",
		CreatorID: org1.UserID,
		Domain:    "example.com",
		Level:     db.Int2(int16(common.DifficultyLevelMedium)),
		Growth:    dbgen.DifficultyGrowthMedium,
	}, org1)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	org2, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamOrg, server.IDHasher.Encrypt(int(org2.ID)))

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/%s/move", server.IDHasher.Encrypt(int(org1.ID)), server.IDHasher.Encrypt(int(property.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	properties, err := store.Impl().RetrieveOrgProperties(ctx, org2)
	if len(properties) != 1 || properties[0].ID != property.ID {
		t.Errorf("Property was not moved")
	}
}
