package portal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func TestGetFormStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, _, _, err := server.Store.Impl().CreateNewForm(ctx, db_tests.CreateNewPropertyParams(user.ID, "form-stats.example.com"), &dbgen.CreateFormParams{
		Name:              t.Name(),
		URL:               "https://example.com/submit",
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

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := timeSeries.WriteFormSubmitBatch(ctx, []*common.FormSubmitRecord{
		{UserID: user.ID, OrgID: org.ID, PropertyID: form.PropertyID, FormID: form.ID, Timestamp: now.Add(-1 * time.Hour), Status: 0},
		{UserID: user.ID, OrgID: org.ID, PropertyID: form.PropertyID, FormID: form.ID, Timestamp: now.Add(-2 * time.Hour), Status: 0},
		{UserID: user.ID, OrgID: org.ID, PropertyID: form.PropertyID, FormID: form.ID, Timestamp: now.Add(-3 * time.Hour), Status: 1},
	}); err != nil {
		t.Fatalf("Failed to write form submit batch: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	periods := []string{PeriodEndpointToday, PeriodEndpointWeek, PeriodEndpointMonth, PeriodEndpointYear}
	for _, period := range periods {
		t.Run(period, func(t *testing.T) {
			req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/form/%s/stats/%s",
				server.IDHasher.Encrypt(int(org.ID)),
				server.IDHasher.Encrypt(int(form.ID)),
				period), nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Unexpected status code %v for period %s", resp.StatusCode, period)
			}

			var stats FormStatsResponse
			if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
				t.Fatalf("Failed to decode response for period %s: %v", period, err)
			}

			successCount := 0
			for _, pt := range stats.Success {
				successCount += pt.Value
			}

			failureCount := 0
			for _, pt := range stats.Failure {
				failureCount += pt.Value
			}

			if successCount != 2 {
				t.Errorf("Expected 2 total successful form submissions for %s period, got %d", period, successCount)
			}
			if failureCount != 1 {
				t.Errorf("Expected 1 total failed form submission for %s period, got %d", period, failureCount)
			}
		})
	}
}
