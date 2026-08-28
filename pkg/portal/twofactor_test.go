package portal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func signInCodeForTest(t *testing.T, email string) int {
	t.Helper()
	code, ok := testMailer.TwoFactorCode(email)
	if !ok {
		t.Fatalf("2FA code was not sent to %s", email)
	}
	return code
}

func signInChallengeForTest(t *testing.T, srv *http.ServeMux, email string) (*http.Cookie, int) {
	t.Helper()
	resp := loginSuite(srv, email, server.XSRF.Token(""))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return responseCookieForTest(t, resp, server.Sessions.CookieName), signInCodeForTest(t, email)
}

func newChallengeReplica(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	replicaCache, err := db.NewMemoryCache[db.CacheKey, any](t.Name(), 1000, &struct{}{}, time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	replicaStore := db.NewBusinessEx(store.Pool, replicaCache)
	sessionStore := db.NewSessionStore(replicaStore, session.KeyPersistent, monitoring.NewStub())
	replica := &Server{
		Store:    replicaStore,
		APIURL:   server.APIURL,
		CDNURL:   server.CDNURL,
		IDHasher: server.IDHasher,
		template: server.template,
		XSRF:     server.XSRF,
		Sessions: &session.Manager{
			CookieName:  server.Sessions.CookieName,
			Store:       sessionStore,
			MaxLifetime: sessionStore.TTL(),
		},
		Mailer:          server.Mailer,
		PlanService:     server.PlanService,
		RateLimiter:     server.RateLimiter,
		RenderConstants: server.RenderConstants,
		PlatformCtx:     server.PlatformCtx,
		DataCtx:         server.DataCtx,
	}
	replica.SettingsTabs = replica.createSettingsTabs()
	replica.Jobs = replica
	mux := http.NewServeMux()
	mux.Handle("POST /"+common.TwoFactorEndpoint, replica.csrf(replica.csrfSessionIDKeyFunc)(http.HandlerFunc(replica.postTwoFactor)))
	return replica, mux
}

func concurrentTwoFactorResponses(t *testing.T, count int, email, token string, code int, cookie *http.Cookie) []*http.Response {
	t.Helper()
	muxes := make([]*http.ServeMux, count)
	for i := range muxes {
		replica, mux := newChallengeReplica(t)
		muxes[i] = mux
		if _, err := replica.Sessions.Store.Read(t.Context(), cookie.Value, false /*skip cache*/); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	responses := make([]*http.Response, count)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			responses[index] = twoFactorSuite(muxes[index], email, token, code, cookie)
		}(i)
	}
	close(start)
	wg.Wait()
	return responses
}

type failingConsumeStore struct {
	session.Store
	err error
}

func (s *failingConsumeStore) ConsumeSignInChallenge(context.Context, *session.Session, *session.Session, string, int32) (session.SignInChallengeResult, error) {
	return session.SignInChallengeResult{}, s.err
}

func TestPostTwoFactor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// request portal (any protected endpoint really)
	privReq := httptest.NewRequest("GET", "/", nil)
	privReq.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, privReq)

	if w.Code != http.StatusOK {
		t.Errorf("Unexpected portal response code: %v", w.Code)
	}
}

func loginSuite(srv *http.ServeMux, email, token string) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamPortalSolution, "captchaSolution")

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	return w.Result()
}

func twoFactorSuite(srv *http.ServeMux, email, token string, code int, cookie *http.Cookie) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamVerificationCode, strconv.Itoa(code))

	// now send the 2fa request
	req := httptest.NewRequest("POST", "/"+common.TwoFactorEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Result()
}

func registrationChallengeForTest(t *testing.T, srv *http.ServeMux, email string) (*http.Cookie, string, int) {
	t.Helper()
	resp := registerSuite(srv, "Registration User", email, server.XSRF.Token(""))
	cookie := responseCookieForTest(t, resp, server.Sessions.CookieName)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	token, err := parseCsrfToken(string(body))
	if err != nil {
		t.Fatal(err)
	}
	return cookie, token, signInCodeForTest(t, email)
}

// technically it's close to TestPersistentSession, but it's more "end-to-end"
func TestPostTwoFactorOtherServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	cookie, code := signInChallengeForTest(t, srv, user.Email)

	if deleted := cache.Delete(ctx, db.SessionCacheKey(cookie.Value)); !deleted {
		t.Fatal("Didn't delete cached session")
	}

	resp := twoFactorSuite(srv, user.Email, server.XSRF.Token(cookie.Value), code, cookie)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	if location, _ := resp.Location(); location.String() != "/" {
		t.Errorf("unexpected redirect: %v", location)
	}

}

func TestPostTwoFactorRotatesSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	oldCookie, code := signInChallengeForTest(t, srv, user.Email)

	resp := twoFactorSuite(srv, user.Email, server.XSRF.Token(oldCookie.Value), code, oldCookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	newCookie := responseCookieForTest(t, resp, server.Sessions.CookieName)
	if newCookie.Value == oldCookie.Value {
		t.Fatal("session ID was not rotated after successful 2FA")
	}

	oldReq := httptest.NewRequest("GET", "/", nil)
	oldReq.AddCookie(oldCookie)
	oldW := httptest.NewRecorder()
	srv.ServeHTTP(oldW, oldReq)
	if oldW.Code == http.StatusOK {
		t.Fatal("pre-2FA session cookie still authenticated after rotation")
	}

	newReq := httptest.NewRequest("GET", "/", nil)
	newReq.AddCookie(newCookie)
	newW := httptest.NewRecorder()
	srv.ServeHTTP(newW, newReq)
	if newW.Code != http.StatusOK {
		t.Fatalf("rotated session cookie was not authenticated: %v", newW.Code)
	}
}

func TestPostTwoFactorAttemptLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	cookie, code := signInChallengeForTest(t, srv, user.Email)

	for attempt := 1; attempt <= maxFailedAttempts; attempt++ {
		resp := twoFactorSuite(srv, user.Email, server.XSRF.Token(cookie.Value), code+1, cookie)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 0 {
			t.Fatalf("failed attempt %d response = (status %d, cookies %d)", attempt, resp.StatusCode, len(resp.Cookies()))
		}
	}

	resp := twoFactorSuite(srv, user.Email, server.XSRF.Token(cookie.Value), code, cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 0 {
		t.Fatalf("attempt-limited response = (status %d, cookies %d)", resp.StatusCode, len(resp.Cookies()))
	}
	var authenticated int
	if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.sessions WHERE user_id = $1 AND state = 'authenticated'", user.ID).Scan(&authenticated); err != nil {
		t.Fatal(err)
	}
	if authenticated != 0 {
		t.Fatalf("authenticated successors after attempt limit = %d, want 0", authenticated)
	}
}

func concurrentAttemptLimitSuite(t *testing.T, kind dbgen.SessionChallengeKind) {
	t.Helper()
	ctx := t.Context()
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	email := strings.ToLower(t.Name()) + "@privatecaptcha.com"
	var userID int32
	var cookie *http.Cookie
	var token string
	var correctCode int

	switch kind {
	case dbgen.SessionChallengeKindSignIn:
		user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
		if err != nil {
			t.Fatal(err)
		}
		email = user.Email
		userID = user.ID
		cookie, correctCode = signInChallengeForTest(t, srv, email)
		token = server.XSRF.Token(cookie.Value)
	case dbgen.SessionChallengeKindRegistration:
		cookie, token, correctCode = registrationChallengeForTest(t, srv, email)
	default:
		t.Fatalf("unsupported challenge kind %q", kind)
	}

	const requests = maxFailedAttempts + 2
	responses := concurrentTwoFactorResponses(t, requests, email, token, invalidVerificationCode(correctCode), cookie)
	for _, response := range responses {
		response.Body.Close()
		if response.StatusCode != http.StatusOK || len(response.Cookies()) != 0 {
			t.Fatalf("invalid attempt response = (status %d, cookies %d), want (200, 0)", response.StatusCode, len(response.Cookies()))
		}
	}

	resp := twoFactorSuite(srv, email, token, correctCode, cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 0 {
		t.Fatalf("attempt-limited response = (status %d, cookies %d), want (200, 0)", resp.StatusCode, len(resp.Cookies()))
	}

	if kind == dbgen.SessionChallengeKindRegistration {
		if _, err := store.Impl().FindUserByEmail(ctx, email); !errors.Is(err, db.ErrRecordNotFound) {
			t.Fatalf("attempt-limited registration user lookup error = %v, want record not found", err)
		}
		return
	}
	var authenticated int
	if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.sessions WHERE user_id = $1 AND state = 'authenticated'", userID).Scan(&authenticated); err != nil {
		t.Fatal(err)
	}
	if authenticated != 0 {
		t.Fatal("attempt-limited sign-in authenticated the user")
	}
}

func invalidTwoFactorChallengeSuite(t *testing.T, kind dbgen.SessionChallengeKind, update string) {
	t.Helper()
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	ctx := t.Context()
	email := strings.ToLower(t.Name()) + "@privatecaptcha.com"
	var userID int32
	var cookie *http.Cookie
	var token string
	var code int

	switch kind {
	case dbgen.SessionChallengeKindSignIn:
		user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
		if err != nil {
			t.Fatal(err)
		}
		email = user.Email
		userID = user.ID
		cookie, code = signInChallengeForTest(t, srv, email)
		token = server.XSRF.Token(cookie.Value)
	case dbgen.SessionChallengeKindRegistration:
		cookie, token, code = registrationChallengeForTest(t, srv, email)
	default:
		t.Fatalf("unsupported challenge kind %q", kind)
	}

	if _, err := store.Pool.Exec(ctx, update, cookie.Value); err != nil {
		t.Fatal(err)
	}
	resp := twoFactorSuite(srv, email, token, code, cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 0 {
		t.Fatalf("invalid challenge response = (status %d, cookies %d), want (200, 0)", resp.StatusCode, len(resp.Cookies()))
	}

	if kind == dbgen.SessionChallengeKindRegistration {
		if _, err := store.Impl().FindUserByEmail(ctx, email); !errors.Is(err, db.ErrRecordNotFound) {
			t.Fatalf("invalid registration user lookup error = %v, want record not found", err)
		}
		return
	}
	var authenticated int
	if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.sessions WHERE user_id = $1 AND state = 'authenticated'", userID).Scan(&authenticated); err != nil {
		t.Fatal(err)
	}
	if authenticated != 0 {
		t.Fatal("invalid sign-in challenge authenticated the user")
	}
}

func TestPostTwoFactorRejectsInvalidChallenge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	for _, test := range []struct {
		name   string
		kind   dbgen.SessionChallengeKind
		update string
	}{
		{name: "SignInExpired", kind: dbgen.SessionChallengeKindSignIn, update: "UPDATE backend.sessions SET challenge_expires_at = NOW() - INTERVAL '1 second' WHERE session_id = $1"},
		{name: "SignInWrongPurpose", kind: dbgen.SessionChallengeKindSignIn, update: "UPDATE backend.sessions SET challenge_kind = 'registration' WHERE session_id = $1"},
		{name: "RegistrationExpired", kind: dbgen.SessionChallengeKindRegistration, update: "UPDATE backend.sessions SET challenge_expires_at = NOW() - INTERVAL '1 second' WHERE session_id = $1"},
		{name: "RegistrationWrongPurpose", kind: dbgen.SessionChallengeKindRegistration, update: "UPDATE backend.sessions SET challenge_kind = 'email_change' WHERE session_id = $1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalidTwoFactorChallengeSuite(t, test.kind, test.update)
		})
	}

	t.Run("SignInMismatchedPayloadUser", func(t *testing.T) {
		ctx := t.Context()
		challengeUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"Challenge", testPlan)
		if err != nil {
			t.Fatal(err)
		}
		otherUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"Other", testPlan)
		if err != nil {
			t.Fatal(err)
		}
		srv := http.NewServeMux()
		server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
		cookie, code := signInChallengeForTest(t, srv, challengeUser.Email)
		pendingSession, err := server.Sessions.Store.Read(ctx, cookie.Value, false /*skip cache*/)
		if err != nil {
			t.Fatal(err)
		}
		pendingSession.Data().SetAuthoritativeUserID(otherUser.ID)

		resp := twoFactorSuite(srv, challengeUser.Email, server.XSRF.Token(cookie.Value), code, cookie)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 0 {
			t.Fatalf("mismatched-user response = (status %d, cookies %d), want (200, 0)", resp.StatusCode, len(resp.Cookies()))
		}
		var authenticated int
		if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.sessions WHERE user_id IN ($1, $2) AND state = 'authenticated'", challengeUser.ID, otherUser.ID).Scan(&authenticated); err != nil {
			t.Fatal(err)
		}
		if authenticated != 0 {
			t.Fatal("mismatched payload user authenticated an account")
		}
	})
}

func TestPostTwoFactorConcurrentAttemptsRespectLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	for name, kind := range map[string]dbgen.SessionChallengeKind{
		"SignIn":       dbgen.SessionChallengeKindSignIn,
		"Registration": dbgen.SessionChallengeKindRegistration,
	} {
		t.Run(name, func(t *testing.T) {
			concurrentAttemptLimitSuite(t, kind)
		})
	}
}

func TestPostTwoFactorConsumesChallengeOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	pendingCookie, code := signInChallengeForTest(t, srv, user.Email)
	token := server.XSRF.Token(pendingCookie.Value)

	responses := concurrentTwoFactorResponses(t, 2, user.Email, token, code, pendingCookie)

	successes := 0
	losers := 0
	for _, response := range responses {
		location, _ := response.Location()
		if response.StatusCode == http.StatusSeeOther && location.Path == "/" {
			successes++
			if len(response.Cookies()) != 1 || response.Cookies()[0].Value == pendingCookie.Value {
				t.Fatal("winning response did not contain only the rotated cookie")
			}
		} else {
			losers++
			if response.StatusCode != http.StatusOK {
				t.Fatalf("losing response status = %d, want 200", response.StatusCode)
			}
			if len(response.Cookies()) != 0 {
				t.Fatal("losing response replaced the pending cookie")
			}
		}
		response.Body.Close()
	}
	if successes != 1 || losers != 1 {
		t.Fatalf("concurrent submissions = (%d winners, %d losers), want (1, 1)", successes, losers)
	}
	var authenticated int
	replay := twoFactorSuite(srv, user.Email, token, code, pendingCookie)
	replay.Body.Close()
	replayLocation, _ := replay.Location()
	if replay.StatusCode == http.StatusSeeOther && replayLocation.Path == "/" {
		t.Fatal("consumed challenge replay succeeded")
	}
	if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.sessions WHERE user_id = $1 AND state = 'authenticated'", user.ID).Scan(&authenticated); err != nil {
		t.Fatal(err)
	}
	if authenticated != 1 {
		t.Fatalf("authenticated successors after replay = %d, want 1", authenticated)
	}

	registrationEmail := t.Name() + "-registration@privatecaptcha.com"
	registrationCookie, registrationToken, registrationCode := registrationChallengeForTest(t, srv, registrationEmail)

	registrationResponses := concurrentTwoFactorResponses(t, 2, registrationEmail, registrationToken, registrationCode, registrationCookie)

	registrationSuccesses := 0
	registrationLosers := 0
	var registrationSuccessorID string
	for _, response := range registrationResponses {
		if response.StatusCode == http.StatusSeeOther {
			registrationSuccesses++
			if len(response.Cookies()) != 1 || response.Cookies()[0].Value == registrationCookie.Value {
				t.Fatal("winning registration response did not rotate the cookie")
			}
			registrationSuccessorID = response.Cookies()[0].Value
		} else {
			registrationLosers++
			if response.StatusCode != http.StatusOK || len(response.Cookies()) != 0 {
				t.Fatalf("losing registration response = (status %d, cookies %d)", response.StatusCode, len(response.Cookies()))
			}
		}
		response.Body.Close()
	}
	if registrationSuccesses != 1 || registrationLosers != 1 {
		t.Fatalf("concurrent registrations = (%d winners, %d losers), want (1, 1)", registrationSuccesses, registrationLosers)
	}
	registrationUser, err := store.Impl().FindUserByEmail(ctx, registrationEmail)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		row, rowErr := dbgen.New(store.Pool).GetSessionByID(ctx, registrationSuccessorID)
		if rowErr == nil && row.State == dbgen.SessionStateAuthenticated && row.UserID.Valid && row.UserID.Int32 == registrationUser.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registration successor did not finalize: row=%+v err=%v", row, rowErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPostTwoFactorFailsClosedWhenSignInRenewalFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	pendingCookie, code := signInChallengeForTest(t, srv, user.Email)

	originalStore := server.Sessions.Store
	server.Sessions.Store = &failingConsumeStore{Store: originalStore, err: errors.New("consume unavailable")}
	defer func() { server.Sessions.Store = originalStore }()
	form := url.Values{}
	form.Add(common.ParamVerificationCode, strconv.Itoa(code))
	req := httptest.NewRequest(http.MethodPost, "/"+common.TwoFactorEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(pendingCookie.Value))
	req.Header.Set(common.HeaderHtmxRequest, "true")
	req.AddCookie(pendingCookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("consume failure status = %d, want 503", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("consume failure replaced the pending cookie")
	}
	var authenticated int
	if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.sessions WHERE user_id = $1 AND state = 'authenticated'", user.ID).Scan(&authenticated); err != nil {
		t.Fatal(err)
	}
	if authenticated != 0 {
		t.Fatalf("authenticated successors after consume failure = %d, want 0", authenticated)
	}
}

func resend2faSuite(srv *http.ServeMux, email, token string, cookie *http.Cookie) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)

	req := httptest.NewRequest("POST", "/"+common.ResendEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Result()
}

func TestResend2FA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	cookie, originalCode := signInChallengeForTest(t, srv, user.Email)
	wrongCode := invalidVerificationCode(originalCode)
	resp := twoFactorSuite(srv, user.Email, server.XSRF.Token(cookie.Value), wrongCode, cookie)
	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("wrong code succeeded")
	}
	resp.Body.Close()
	resp = resend2faSuite(srv, user.Email, server.XSRF.Token(cookie.Value), cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected resend2fa status code: %v", resp.StatusCode)
	}
	resp.Body.Close()

	newCode := signInCodeForTest(t, user.Email)
	if newCode == originalCode {
		t.Fatalf("new 2FA code equals old code: %d", newCode)
	}

	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(cookie.Value), originalCode, cookie)
	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("old code succeeded after resend")
	}
	resp.Body.Close()
	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(cookie.Value), newCode, cookie)
	location, _ := resp.Location()
	if resp.StatusCode != http.StatusSeeOther || location.Path != "/" {
		t.Fatalf("unexpected post twofactor response with reissued code: status %v, location %v", resp.StatusCode, location)
	}
	resp.Body.Close()
}

func TestResend2FAUsesAuthoritativeEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	pendingCookie, _ := signInChallengeForTest(t, srv, user.Email)
	pendingSession, err := server.Sessions.Store.Read(ctx, pendingCookie.Value, false /*skip cache*/)
	if err != nil {
		t.Fatal(err)
	}
	tamperedEmail := t.Name() + "@attacker.example"
	if err := pendingSession.Set(ctx, session.KeyUserEmail, tamperedEmail); err != nil {
		t.Fatal(err)
	}

	resp := resend2faSuite(srv, user.Email, server.XSRF.Token(pendingCookie.Value), pendingCookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resend status = %d, want 200", resp.StatusCode)
	}
	mailedCode, ok := testMailer.TwoFactorCode(user.Email)
	if !ok {
		t.Fatal("resend did not deliver to the authoritative email")
	}
	if _, ok := testMailer.TwoFactorCode(tamperedEmail); ok {
		t.Fatal("resend delivered to tampered payload email")
	}
	authenticated := twoFactorSuite(srv, user.Email, server.XSRF.Token(pendingCookie.Value), mailedCode, pendingCookie)
	location, _ := authenticated.Location()
	authenticated.Body.Close()
	if authenticated.StatusCode != http.StatusSeeOther || location.Path != "/" {
		t.Fatalf("authoritative resend code did not authenticate: status %d, location %v", authenticated.StatusCode, location)
	}
}

func TestResend2FAWithoutSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to resend 2FA without a valid session - should fail due to CSRF
	// (the CSRF middleware requires a session with email to verify the token)
	form := url.Values{}
	form.Add(common.ParamCSRFToken, "invalid-token")

	req := httptest.NewRequest("POST", "/"+common.ResendEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp := w.Result()

	// Should redirect to expired page due to CSRF failure
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status code: %v", resp.StatusCode)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatal(err)
	}

	// Without valid session, CSRF will fail and redirect to expired endpoint
	if location.Path != "/"+common.ExpiredEndpoint {
		t.Errorf("Expected redirect to expired, got: %v", location.Path)
	}
}

func TestResend2FAWithCompletedSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	// Complete the full authentication
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to resend 2FA with an already completed session
	// After authentication, the session no longer has the email key, so CSRF will fail
	resp := resend2faSuite(srv, user.Email, server.XSRF.Token(cookie.Value), cookie)

	// Should redirect to expired since session is already completed (no email in session)
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status code: %v", resp.StatusCode)
	}
}

func TestParseOrgInviteIDFromURLValid(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Use the server's IDHasher
	encodedID := server.IDHasher.Encrypt(123)
	rawURL := "https://portal.example.com/" + common.OrgInviteEndpoint + "/" + encodedID + "/" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != 123 {
		t.Errorf("Expected 123, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLNoPrefixMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// URL without orginvite prefix
	rawURL := "https://portal.example.com/other/path/" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != -1 {
		t.Errorf("Expected -1, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLNoSuffix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	encodedID := server.IDHasher.Encrypt(123)
	// URL without /signup suffix
	rawURL := "https://portal.example.com/" + common.OrgInviteEndpoint + "/" + encodedID

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != -1 {
		t.Errorf("Expected -1, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Use an invalid encoded ID
	rawURL := "https://portal.example.com/" + common.OrgInviteEndpoint + "/invalid-id/" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != -1 {
		t.Errorf("Expected -1 for invalid ID, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLEmptyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Path with empty ID segment
	rawURL := "https://portal.example.com/" + common.OrgInviteEndpoint + "//" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != -1 {
		t.Errorf("Expected -1 for empty ID, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLRelativePath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	encodedID := server.IDHasher.Encrypt(456)
	// Relative URL
	rawURL := "/" + common.OrgInviteEndpoint + "/" + encodedID + "/" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != 456 {
		t.Errorf("Expected 456, got %d", result)
	}
}
