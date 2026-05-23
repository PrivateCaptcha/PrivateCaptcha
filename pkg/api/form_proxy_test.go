package api

import (
	"context"
	"fmt"
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
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func createFormProxyForTest(ctx context.Context, t *testing.T, name, domain string) (*dbgen.Form, *dbgen.Property) {
	t.Helper()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, name, testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, property, _, err := store.Impl().CreateNewForm(ctx, db_tests.CreateNewPropertyParams(user.ID, domain), &dbgen.CreateFormParams{
		Url:               "https://example.com/submit",
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	return form, property
}

func formProxySuite(t *testing.T, form *dbgen.Form, body url.Values) *http.Response {
	t.Helper()

	srv := http.NewServeMux()
	server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	req := httptest.NewRequest(http.MethodPost, "/"+common.FormEndpoint+"/"+db.UUIDToString(form.ExternalID), strings.NewReader(body.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Result()
}

func TestFormProxyRejectsInvalidCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	form, _ := createFormProxyForTest(ctx, t, t.Name(), "invalid-captcha.example.com")

	body := url.Values{}
	body.Set("email", "test@example.com")
	body.Set(common.ParamPrivateCaptchaSolution, "invalid-captcha")

	resp := formProxySuite(t, form, body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestFormProxyRejectsWrongPropertyCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	form1, _ := createFormProxyForTest(ctx, t, t.Name()+"-one", "wrong-property-one.example.com")
	_, property2 := createFormProxyForTest(ctx, t, t.Name()+"-two", "wrong-property-two.example.com")

	sitekey2 := db.UUIDToSiteKey(property2.ExternalID)
	puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey2, property2.Domain)
	if err != nil {
		t.Fatal(err)
	}

	body := url.Values{}
	body.Set("email", "test@example.com")
	body.Set(common.ParamPrivateCaptchaSolution, fmt.Sprintf("%s.%s", solutionsStr, puzzleStr))

	resp := formProxySuite(t, form1, body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestFormProxyAcceptsValidCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	server.FormSubmissionChan = make(chan *FormSubmission, 1)
	form, property := createFormProxyForTest(ctx, t, t.Name(), "valid-form.example.com")

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey, property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	body := url.Values{}
	body.Set("email", "test@example.com")
	body.Set(common.ParamPrivateCaptchaSolution, fmt.Sprintf("%s.%s", solutionsStr, puzzleStr))

	resp := formProxySuite(t, form, body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Unexpected submit status code %d", resp.StatusCode)
	}

	select {
	case submission := <-server.FormSubmissionChan:
		if submission.Values.Get(common.ParamPrivateCaptchaSolution) != "" {
			t.Fatalf("expected captcha solution field to be stripped")
		}
		if submission.Values.Get("email") != "test@example.com" {
			t.Fatalf("expected form field to be preserved")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued form submission")
	}
}
