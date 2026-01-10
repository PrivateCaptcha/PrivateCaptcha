package portal

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func TestDismissNotification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()

	// Create a user and authenticate
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// Create a system notification for this user
	tnow := time.Now().UTC()
	notification, err := store.Impl().CreateSystemNotification(ctx, "Test notification message", tnow, nil /*duration*/, &user.ID)
	if err != nil {
		t.Fatalf("Failed to create notification: %v", err)
	}

	// Set the notification ID in the session
	sess, found := server.Sessions.SessionGet(httptest.NewRequest("GET", "/", nil).WithContext(ctx))
	if !found {
		// Get session from the cookie
		sessionReq := httptest.NewRequest("GET", "/", nil)
		sessionReq.AddCookie(cookie)
		sess, found = server.Sessions.SessionGet(sessionReq)
		if !found {
			t.Fatal("Could not get session")
		}
	}

	// Store notification ID in session
	sess.Set(session.KeyNotificationID, notification.ID)

	// Create encoded notification ID
	encodedID := server.IDHasher.Encrypt(int(notification.ID))

	// Make request to dismiss notification
	req := httptest.NewRequest("DELETE", "/"+common.NotificationEndpoint+"/"+encodedID, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should succeed
	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

func TestDismissNotificationInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()

	// Create a user and authenticate
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// Make request with invalid notification ID
	req := httptest.NewRequest("DELETE", "/"+common.NotificationEndpoint+"/invalid-id", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should fail with bad request
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status BadRequest, got %d", w.Code)
	}
}

func TestDismissNotificationWithoutSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Make request without session cookie
	req := httptest.NewRequest("DELETE", "/"+common.NotificationEndpoint+"/some-id", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should redirect to login due to auth middleware
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status %d", w.Code)
	}
}
