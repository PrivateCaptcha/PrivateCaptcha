package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func TestCancelURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.TODO()
	subscrParams := db_tests.CreateNewSubscriptionParams()
	subscrParams.Source = dbgen.SubscriptionSourcePaddle
	user, _, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name(), subscrParams)
	if err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(srv, cfg.PortalDomain(), common.NoopMiddleware)

	cancelURL := "http://localhost/my/test"

	server.PaddleAPI.(*billing.StubPaddleClient).URLs = &billing.ManagementURLs{CancelURL: cancelURL}

	cookie, err := authenticateSuite(ctx, user.Email, srv)
	if err != nil {
		t.Fatal(err)
	}

	endpoint := server.partsURL(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint, common.CancelEndpoint)
	req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	l, err := resp.Location()
	if err != nil {
		t.Fatal(err)
	}

	if l.String() != cancelURL {
		t.Errorf("Unexpected response. Received %v, expected %v", l, cancelURL)
	}
}
