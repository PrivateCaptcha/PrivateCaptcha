package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PuerkitoBio/goquery"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestWebhookPrefixFromURL(t *testing.T) {
	testCases := []struct {
		name string
		url  string
		want string
	}{
		{name: "FirstPathSegment", url: "https://hooks.example.com/submit/form", want: "hooks.example.com/submit"},
		{name: "LongSegmentTrimmed", url: "https://hooks.example.com/abcdefghijklmnop/rest", want: "hooks.example.com/abcdefghijkl"},
		{name: "NoPath", url: "https://hooks.example.com", want: "hooks.example.com"},
		{name: "InvalidURLFallsBack", url: "not a valid url", want: "not a valid url"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webhookPrefixFromURL(tc.url); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFormToUserForm(t *testing.T) {
	hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "test-salt"))
	form := &dbgen.Form{
		ID:      34,
		OrgID:   pgtype.Int4{Int32: 56, Valid: true},
		URL:     "https://hooks.example.com/submit/form",
		Enabled: true,
	}

	userForm := formToUserForm(form, hasher)
	if userForm == nil {
		t.Fatal("expected user form")
	}
	if userForm.ID != hasher.Encrypt(int(form.ID)) {
		t.Fatalf("expected encrypted form ID %q, got %q", hasher.Encrypt(int(form.ID)), userForm.ID)
	}
	if userForm.OrgID != hasher.Encrypt(int(form.OrgID.Int32)) {
		t.Fatalf("expected encrypted org ID %q, got %q", hasher.Encrypt(int(form.OrgID.Int32)), userForm.OrgID)
	}
	if userForm.Name != form.Name {
		t.Fatalf("expected property name %q, got %q", form.Name, userForm.Name)
	}
	if userForm.WebhookPrefix != "hooks.example.com/submit" {
		t.Fatalf("expected webhook prefix %q, got %q", "hooks.example.com/submit", userForm.WebhookPrefix)
	}
	if !userForm.Enabled {
		t.Fatal("expected enabled form")
	}
}

func TestRenderFormsPaginationControls(t *testing.T) {
	platformCtx := &PlatformRenderContext{
		GitCommit:      "qwerty123",
		Enterprise:     true,
		licenseService: server.LicenseService,
	}

	buf, err := server.RenderResponse(t.Context(), "portal/forms.html", &orgFormsRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{CurrentOrg: stubOrg("123")},
		PaginationRenderContext: PaginationRenderContext{From: 1, To: 30, Count: 31, Page: 0, PerPage: 30},
		Forms:                   []*userForm{stubForm("Newsletter Signup", "123")},
	}, &RequestContext{Path: server.RelURL("/org/123/forms")}, platformCtx)
	if err != nil {
		t.Fatal(err)
	}

	document := portal_tests.ParseHTML(t, buf)
	buttons := document.Find("button[hx-target=\"#forms\"]")
	if buttons.Length() != 2 {
		t.Fatalf("expected 2 pagination buttons, got %d", buttons.Length())
	}
	buttons.Each(func(i int, s *goquery.Selection) {
		if s.AttrOr("hx-get", "") != "/org/123/forms" {
			t.Fatalf("expected pagination button to use forms endpoint, got %q", s.AttrOr("hx-get", ""))
		}
	})
}

func TestGetNewOrgForm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/org/%s/form/new", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Form Wizard") {
		t.Fatal("expected form wizard heading")
	}
	if !strings.Contains(body, "Create new form") {
		t.Fatal("expected create new form step")
	}
	if !strings.Contains(body, "Website integration") {
		t.Fatal("expected website integration step")
	}
	if !strings.Contains(body, `name="url"`) {
		t.Fatal("expected URL input field")
	}
	if !strings.Contains(body, fmt.Sprintf("/org/%s?tab=%s", server.IDHasher.Encrypt(int(org.ID)), common.FormsEndpoint)) {
		t.Fatal("expected cancel link back to forms area")
	}
	if strings.Contains(body, "Server integration") {
		t.Fatal("did not expect server integration step")
	}
}

func TestPostNewOrgForm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	formName := t.Name() + " Contact"
	formData := url.Values{}
	formData.Set(common.ParamCSRFToken, server.XSRF.Token(fmt.Sprintf("%d", user.ID)))
	formData.Set(common.ParamName, formName)
	formData.Set(common.ParamDomain, "example.com")
	formData.Set(common.ParamURL, "https://hooks.example.com/submit")
	formData.Set(common.ParamIgnoreError, "true")

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/new", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(formData.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Unexpected status code %d", w.Code)
	}

	forms, _, err := store.Impl().RetrieveOrgForms(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 {
		t.Fatalf("expected 1 form, got %d", len(forms))
	}

	createdForm := forms[0]
	if createdForm.Name != formName {
		t.Fatalf("expected form name %q, got %q", formName, createdForm.Name)
	}
	if createdForm.URL != "https://hooks.example.com/submit" {
		t.Fatalf("expected form URL to be preserved, got %q", createdForm.URL)
	}
	if !createdForm.Enabled {
		t.Fatal("expected created form to be enabled")
	}
	if createdForm.Method != dbgen.FormMethodPost {
		t.Fatalf("expected form method %q, got %q", dbgen.FormMethodPost, createdForm.Method)
	}
	if createdForm.RequestsPerSecond != 1 {
		t.Fatalf("expected requests per second 1, got %v", createdForm.RequestsPerSecond)
	}
	if createdForm.RequestsBurst != 5 {
		t.Fatalf("expected requests burst 5, got %d", createdForm.RequestsBurst)
	}
	if createdForm.RetryRequestCount != 0 {
		t.Fatalf("expected retry count 0, got %d", createdForm.RetryRequestCount)
	}

	createdProperty, err := store.Impl().GetCachedPropertyByID(ctx, createdForm.PropertyID)
	if err != nil {
		t.Fatalf("expected backing property, got %v", err)
	}
	if createdProperty == nil {
		t.Fatal("expected backing property to exist")
	}
	if createdProperty.Domain != "example.com" {
		t.Fatalf("expected backing property domain %q, got %q", "example.com", createdProperty.Domain)
	}

	formGUID := db.UUIDToString(createdForm.ExternalID)
	expectedFormURL := server.APIURL + "/form/" + formGUID
	if !strings.Contains(w.Body.String(), expectedFormURL) {
		t.Fatal("expected integration step to include public form endpoint")
	}
}

type rejectPortalFormURLVerifier struct {
	err error
}

type formsLimitSubscriptionStub struct {
	db.StubSubscriptionLimits
	err error
}

func (v rejectPortalFormURLVerifier) VerifyURL(ctx context.Context, rawURL string) error {
	return v.err
}

func (v rejectPortalFormURLVerifier) VerifyResolvedAddress(ctx context.Context, host string, ip netip.Addr) error {
	return v.err
}

func (s formsLimitSubscriptionStub) CheckFormsLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (bool, int, error) {
	return false, 1, s.err
}

func TestPostNewOrgFormInvalidInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	type testCase struct {
		name           string
		setup          func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func())
		mutate         func(values url.Values)
		expectedStatus int
		expectedBody   string
		expectedCount  int
		assertRedirect bool
	}

	newValues := func(userID int32) url.Values {
		values := url.Values{}
		values.Set(common.ParamCSRFToken, server.XSRF.Token(fmt.Sprintf("%d", userID)))
		values.Set(common.ParamName, "Negative Form")
		values.Set(common.ParamDomain, "example.com")
		values.Set(common.ParamURL, "https://hooks.example.com/submit")
		values.Set(common.ParamIgnoreError, "true")
		return values
	}

	testCases := []testCase{
		{
			name: "EmptyName",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			mutate: func(values url.Values) {
				values.Set(common.ParamName, "")
			},
			expectedStatus: http.StatusOK,
			expectedBody:   common.StatusPropertyNameEmptyError.String(),
			expectedCount:  0,
		},
		{
			name: "InvalidDomain",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			mutate: func(values url.Values) {
				values.Del(common.ParamIgnoreError)
				values.Set(common.ParamDomain, "localhost")
			},
			expectedStatus: http.StatusOK,
			expectedBody:   common.StatusPropertyDomainLocalhostError.String(),
			expectedCount:  0,
		},
		{
			name: "EmptyURL",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			mutate: func(values url.Values) {
				values.Set(common.ParamURL, "")
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "URL cannot be empty.",
			expectedCount:  0,
		},
		{
			name: "UnsafeURL",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}

				originalVerifier := server.FormURLVerifier
				server.FormURLVerifier = rejectPortalFormURLVerifier{err: errors.New("blocked")}

				return user, org, user.Email, func() {
					server.FormURLVerifier = originalVerifier
				}
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "URL is not valid.",
			expectedCount:  0,
		},
		{
			name: "MissingSubscription",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewBareAccount(ctx, store, t.Name())
				if err != nil {
					t.Fatalf("Failed to create bare account: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			expectedStatus: http.StatusOK,
			expectedBody:   activeSubscriptionForPropertyError,
			expectedCount:  0,
		},
		{
			name: "InvitedUser",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"owner", testPlan)
				if err != nil {
					t.Fatalf("Failed to create owner account: %v", err)
				}
				member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"member", testPlan)
				if err != nil {
					t.Fatalf("Failed to create invited account: %v", err)
				}
				if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
					t.Fatalf("Failed to invite member: %v", err)
				}
				return member, org, member.Email, func() {}
			},
			expectedStatus: http.StatusSeeOther,
			expectedCount:  0,
			assertRedirect: true,
		},
		{
			name: "FormsLimitExceeded",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}

				originalLimits := server.SubscriptionLimits
				server.SubscriptionLimits = formsLimitSubscriptionStub{}

				return user, org, user.Email, func() {
					server.SubscriptionLimits = originalLimits
				}
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "Forms limit reached for current subscription plan.",
			expectedCount:  0,
		},
		{
			name: "DuplicateName",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}
				_, _, _, err = store.Impl().CreateNewForm(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), &dbgen.CreateFormParams{
					Name:              "Negative Form",
					URL:               "https://hooks.example.com/existing",
					Fields:            []byte(`{}`),
					Enabled:           true,
					RequestsPerSecond: 1,
					RequestsBurst:     5,
					RetryRequestCount: 0,
					Method:            dbgen.FormMethodPost,
				}, org)
				if err != nil {
					t.Fatalf("Failed to create existing form: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			expectedStatus: http.StatusOK,
			expectedBody:   common.StatusPropertyNameDuplicateError.String(),
			expectedCount:  1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			user, org, email, cleanup := tc.setup(t)
			defer cleanup()

			srv := http.NewServeMux()
			server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

			cookie, err := portal_tests.AuthenticateSuite(t.Context(), email, srv, server.XSRF, server.Sessions)
			if err != nil {
				t.Fatal(err)
			}

			values := newValues(user.ID)
			if tc.mutate != nil {
				tc.mutate(values)
			}

			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/new", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(values.Encode()))
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d", tc.expectedStatus, w.Code)
			}

			if tc.assertRedirect {
				location, err := w.Result().Location()
				if err != nil {
					t.Fatalf("expected redirect location, got %v", err)
				}
				if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
					t.Fatalf("expected error redirect, got %q", location.String())
				}
			} else if tc.expectedBody != "" && !strings.Contains(w.Body.String(), tc.expectedBody) {
				t.Fatalf("expected body to contain %q", tc.expectedBody)
			}

			forms, _, err := store.Impl().RetrieveOrgForms(t.Context(), org, 0, db.MaxOrgPropertiesPageSize)
			if err != nil {
				t.Fatal(err)
			}
			if len(forms) != tc.expectedCount {
				t.Fatalf("expected %d forms, got %d", tc.expectedCount, len(forms))
			}
		})
	}
}
