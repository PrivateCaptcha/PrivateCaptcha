package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type stubStore struct {
	resolved           *Session
	resolveErr         error
	issueSignIn        func(context.Context, SignInChallengeIssue) (*ChallengeResult, error)
	issueRegistration  func(context.Context, RegistrationChallengeIssue) (*ChallengeResult, error)
	consumeSignIn      func(context.Context, SignInChallengeConsume) (*ChallengeResult, error)
	createRegistration func(context.Context, RegistrationSuccessorCreate) (*ChallengeResult, error)
	revokeSession      func(context.Context, string) (*RevocationResult, error)
}

func (s *stubStore) Start(ctx context.Context, interval time.Duration)        {}
func (s *stubStore) EnqueueExpirationRenewal(ctx context.Context, sid string) {}
func (s *stubStore) StartAnonymousSession(sid string) *Session {
	return NewAnonymousSession(sid, payloadStoreStub{})
}
func (s *stubStore) Resolve(ctx context.Context, sid string) (*Session, error) {
	return s.resolved, s.resolveErr
}
func (s *stubStore) IssueSignInChallenge(ctx context.Context, issue SignInChallengeIssue) (*ChallengeResult, error) {
	if s.issueSignIn == nil {
		return nil, nil
	}
	return s.issueSignIn(ctx, issue)
}
func (s *stubStore) IssueRegistrationChallenge(ctx context.Context, issue RegistrationChallengeIssue) (*ChallengeResult, error) {
	if s.issueRegistration != nil {
		return s.issueRegistration(ctx, issue)
	}
	return nil, nil
}
func (s *stubStore) SetVerifyRegistration(context.Context, string) error { return nil }
func (s *stubStore) ResendPendingChallenge(context.Context, PendingChallengeResend) (*ChallengeResult, error) {
	return nil, nil
}
func (s *stubStore) ConsumeSignInChallenge(ctx context.Context, consume SignInChallengeConsume) (*ChallengeResult, error) {
	if s.consumeSignIn == nil {
		return nil, nil
	}
	return s.consumeSignIn(ctx, consume)
}
func (s *stubStore) ConsumeRegistrationChallenge(context.Context, RegistrationChallengeConsume) (*RegistrationConsumeResult, error) {
	return nil, nil
}
func (s *stubStore) CreateRegistrationSuccessor(ctx context.Context, create RegistrationSuccessorCreate) (*ChallengeResult, error) {
	if s.createRegistration != nil {
		return s.createRegistration(ctx, create)
	}
	return nil, nil
}
func (s *stubStore) IssueEmailChangeChallenge(context.Context, EmailChangeChallengeIssue) (*ChallengeResult, error) {
	return nil, nil
}
func (s *stubStore) ConsumeEmailChangeChallenge(context.Context, EmailChangeChallengeConsume) (*ChallengeResult, error) {
	return nil, nil
}
func (s *stubStore) RevokeSession(ctx context.Context, sid string) (*RevocationResult, error) {
	if s.revokeSession == nil {
		return nil, nil
	}
	return s.revokeSession(ctx, sid)
}
func (s *stubStore) RevokeUserSessions(context.Context, int32) error { return nil }

func TestManagerStartReplacesUnknownCookieWithAnonymousSession(t *testing.T) {
	store := &stubStore{resolveErr: ErrSessionMissing}
	manager := &Manager{CookieName: "pcsid", Store: store, MaxLifetime: 3 * time.Hour, Path: "/portal"}
	req := httptest.NewRequest(http.MethodGet, "/portal/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(&http.Cookie{Name: manager.CookieName, Value: url.QueryEscape("legacy-sid")})
	w := httptest.NewRecorder()
	beforeExpiry := time.Now().Add(manager.MaxLifetime - time.Second)

	sess, err := manager.Start(w, req)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID() == "legacy-sid" {
		t.Fatal("unknown legacy SID was reused")
	}
	if _, ok := sess.Authority(); ok {
		t.Fatal("new local session unexpectedly has Authority")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Value != url.QueryEscape(sess.ID()) || cookie.Path != manager.Path || cookie.MaxAge != int(manager.MaxLifetime.Seconds()) {
		t.Fatalf("anonymous session cookie = %+v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("anonymous session cookie is not secure: %+v", cookie)
	}
	if cookie.Expires.Before(beforeExpiry) || cookie.Expires.After(time.Now().Add(manager.MaxLifetime+time.Second)) {
		t.Fatalf("anonymous session cookie expiration = %s", cookie.Expires)
	}
}

func TestManagerStartPreservesCookieOnStoreFailure(t *testing.T) {
	storeErr := errors.New("database unavailable")
	manager := &Manager{CookieName: "pcsid", Store: &stubStore{resolveErr: storeErr}, MaxLifetime: 3 * time.Hour, Path: "/"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: manager.CookieName, Value: "existing-sid"})
	w := httptest.NewRecorder()

	if _, err := manager.Start(w, req); !errors.Is(err, storeErr) {
		t.Fatalf("Start error = %v, want %v", err, storeErr)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatalf("store failure replaced cookie: %v", w.Result().Cookies())
	}
}

func TestManagerGetPreservesMissingAndInfrastructureErrors(t *testing.T) {
	manager := &Manager{CookieName: "pcsid", Store: &stubStore{resolveErr: ErrSessionMissing}}
	if _, err := manager.Get(httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, ErrSessionMissing) {
		t.Fatalf("missing cookie error = %v, want %v", err, ErrSessionMissing)
	}

	storeErr := errors.New("database unavailable")
	manager.Store = &stubStore{resolveErr: storeErr}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: manager.CookieName, Value: "sid"})
	if _, err := manager.Get(req); !errors.Is(err, storeErr) {
		t.Fatalf("infrastructure error = %v, want %v", err, storeErr)
	}
}

func TestManagerIssueSignInChallengeSetsCookieOnlyAfterSuccess(t *testing.T) {
	pending := NewSessionWithAuthority(
		Authority{State: StatePending, Version: 1, ExpiresAt: time.Now().Add(3 * time.Hour)},
		NewPayload(t.Name(), payloadStoreStub{}),
	)
	store := &stubStore{issueSignIn: func(context.Context, SignInChallengeIssue) (*ChallengeResult, error) {
		return &ChallengeResult{Outcome: TransitionSucceeded, Session: pending}, nil
	}}
	manager := &Manager{CookieName: "pcsid", Store: store, MaxLifetime: 3 * time.Hour, Path: "/"}
	anonymous := NewAnonymousSession(t.Name(), payloadStoreStub{})
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	w := httptest.NewRecorder()

	result, err := manager.IssueSignInChallenge(w, req, anonymous, 42, "123456", 15*time.Minute, 5)
	if err != nil || result.Outcome != TransitionSucceeded || result.Session.ID() != pending.ID() {
		t.Fatalf("issue result = (%+v, %v)", result, err)
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].Value != url.QueryEscape(pending.ID()) {
		t.Fatalf("pending cookie = %v", cookies)
	}

	store.issueSignIn = func(context.Context, SignInChallengeIssue) (*ChallengeResult, error) {
		return nil, errors.New("database unavailable")
	}
	w = httptest.NewRecorder()
	if _, err := manager.IssueSignInChallenge(w, req, anonymous, 42, "123456", 15*time.Minute, 5); err == nil {
		t.Fatal("issue failure was ignored")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatalf("issue failure set cookie: %v", w.Result().Cookies())
	}
}

func TestManagerTransitionFailuresPreserveCookie(t *testing.T) {
	storeErr := errors.New("database unavailable")
	store := &stubStore{
		issueRegistration: func(context.Context, RegistrationChallengeIssue) (*ChallengeResult, error) {
			return nil, storeErr
		},
		consumeSignIn: func(context.Context, SignInChallengeConsume) (*ChallengeResult, error) {
			return nil, storeErr
		},
		createRegistration: func(context.Context, RegistrationSuccessorCreate) (*ChallengeResult, error) {
			return nil, storeErr
		},
	}
	manager := &Manager{CookieName: "pcsid", Store: store, MaxLifetime: 3 * time.Hour, Path: "/"}
	predecessor := NewAnonymousSession(t.Name(), payloadStoreStub{})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: manager.CookieName, Value: predecessor.ID()})
	tests := []struct {
		name string
		run  func(http.ResponseWriter) error
	}{
		{name: "registration issue", run: func(w http.ResponseWriter) error {
			_, err := manager.IssueRegistrationChallenge(w, req, predecessor, "user@example.com", "123456", 15*time.Minute, 5)
			return err
		}},
		{name: "sign-in consume", run: func(w http.ResponseWriter) error {
			_, err := manager.ConsumeSignInChallenge(w, req, predecessor, "123456", 5)
			return err
		}},
		{name: "registration successor", run: func(w http.ResponseWriter) error {
			_, err := manager.CreateRegistrationSuccessor(w, req, predecessor, 42)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			if err := tt.run(w); !errors.Is(err, storeErr) {
				t.Fatalf("transition error = %v, want %v", err, storeErr)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatalf("transition failure replaced cookie: %v", w.Result().Cookies())
			}
		})
	}
}

func TestManagerConsumeSignInChallengeRotatesServerGeneratedSID(t *testing.T) {
	predecessor := NewSessionWithAuthority(
		Authority{State: StatePending, Version: 2, ExpiresAt: time.Now().Add(time.Hour)},
		NewPayload("predecessor", payloadStoreStub{}),
	)
	store := &stubStore{consumeSignIn: func(_ context.Context, consume SignInChallengeConsume) (*ChallengeResult, error) {
		successor := NewSessionWithAuthority(
			Authority{State: StateAuthenticated, Version: 1, UserID: 42, ExpiresAt: time.Now().Add(3 * time.Hour)},
			NewPayload(consume.SuccessorSessionID, payloadStoreStub{}),
		)
		return &ChallengeResult{Outcome: TransitionSucceeded, Session: successor}, nil
	}}
	manager := &Manager{CookieName: "pcsid", Store: store, MaxLifetime: 3 * time.Hour, Path: "/"}
	req := httptest.NewRequest(http.MethodPost, "/2fa", nil)
	w := httptest.NewRecorder()

	result, err := manager.ConsumeSignInChallenge(w, req, predecessor, "123456", 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.ID() == predecessor.ID() {
		t.Fatal("successful sign-in reused predecessor SID")
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].Value != url.QueryEscape(result.Session.ID()) {
		t.Fatalf("successor cookie = %v", cookies)
	}
}

func TestManagerRevokeClearsCookieOnlyAfterSuccess(t *testing.T) {
	storeErr := errors.New("database unavailable")
	store := &stubStore{revokeSession: func(context.Context, string) (*RevocationResult, error) { return nil, storeErr }}
	manager := &Manager{CookieName: "pcsid", Store: store, Path: "/portal", SecureCookie: true}
	req := httptest.NewRequest(http.MethodGet, "/portal/logout", nil)
	req.AddCookie(&http.Cookie{Name: manager.CookieName, Value: "sid"})
	w := httptest.NewRecorder()

	if _, err := manager.Revoke(w, req); !errors.Is(err, storeErr) {
		t.Fatalf("Revoke error = %v, want %v", err, storeErr)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatalf("revocation failure cleared cookie: %v", w.Result().Cookies())
	}

	store.revokeSession = func(context.Context, string) (*RevocationResult, error) {
		return &RevocationResult{SessionID: "sid", State: StateRevoked, Version: 2, UserID: 42}, nil
	}
	w = httptest.NewRecorder()
	result, err := manager.Revoke(w, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.UserID != 42 {
		t.Fatalf("revoked user ID = %d, want 42", result.UserID)
	}
	if result.SessionHash != common.HashSessionID("sid") {
		t.Fatalf("revoked session hash = %q, want %q", result.SessionHash.String(), common.HashSessionID("sid").String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || !cookies[0].Expires.Before(time.Now()) || !cookies[0].Secure {
		t.Fatalf("cleared cookie = %v", cookies)
	}
}

func TestScheduleExpirationRenewalRefreshesCookieWithoutChangingAuthority(t *testing.T) {
	store := &stubStore{}
	manager := &Manager{
		CookieName:  "pcsid",
		Store:       store,
		MaxLifetime: 3 * time.Hour,
		Path:        "/portal",
	}
	authority := Authority{
		State:      StateAuthenticated,
		Version:    7,
		UserID:     42,
		ExpiresAt:  time.Now().Add(2 * time.Hour),
		LeaseUntil: time.Now().Add(5 * time.Minute),
	}
	sess := NewSessionWithAuthority(authority, NewPayload(t.Name(), payloadStoreStub{}))
	req := httptest.NewRequest(http.MethodGet, "/portal/dashboard", nil)
	w := httptest.NewRecorder()

	manager.ScheduleExpirationRenewal(w, req, sess)

	actualAuthority, _ := sess.Authority()
	if actualAuthority != authority {
		t.Fatalf("scheduling changed Authority: got %+v, want %+v", actualAuthority, authority)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("renewal cookies = %d, want 1", len(cookies))
	}
	if cookies[0].Value != url.QueryEscape(t.Name()) || cookies[0].MaxAge != int(manager.MaxLifetime.Seconds()) {
		t.Fatalf("renewal cookie = %+v", cookies[0])
	}
}

func TestScheduleExpirationRenewalSkipsIneligibleSessions(t *testing.T) {
	tests := []struct {
		name      string
		authority Authority
	}{
		{name: "outside window", authority: Authority{State: StateAuthenticated, ExpiresAt: time.Now().Add(3 * time.Hour)}},
		{name: "pending", authority: Authority{State: StatePending, ExpiresAt: time.Now().Add(time.Hour)}},
		{name: "expired", authority: Authority{State: StateAuthenticated, ExpiresAt: time.Now().Add(-time.Hour)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubStore{}
			manager := &Manager{CookieName: "pcsid", Store: store, MaxLifetime: 3 * time.Hour, Path: "/"}
			sess := NewSessionWithAuthority(tt.authority, NewPayload(t.Name(), payloadStoreStub{}))
			w := httptest.NewRecorder()

			manager.ScheduleExpirationRenewal(w, httptest.NewRequest(http.MethodGet, "/", nil), sess)

			if len(w.Result().Cookies()) != 0 {
				t.Fatalf("ineligible session refreshed cookie: %v", w.Result().Cookies())
			}
		})
	}
}

func TestExpirationRenewalWindowBoundary(t *testing.T) {
	now := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		authority Authority
		want      bool
	}{
		{name: "at boundary", authority: Authority{State: StateAuthenticated, ExpiresAt: now.Add(sessionExpirationRenewalWindow)}, want: true},
		{name: "outside boundary", authority: Authority{State: StateAuthenticated, ExpiresAt: now.Add(sessionExpirationRenewalWindow + time.Nanosecond)}},
		{name: "expired", authority: Authority{State: StateAuthenticated, ExpiresAt: now}},
		{name: "pending", authority: Authority{State: StatePending, ExpiresAt: now.Add(time.Hour)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := shouldScheduleExpirationRenewal(tt.authority, now); actual != tt.want {
				t.Fatalf("shouldScheduleExpirationRenewal() = %t, want %t", actual, tt.want)
			}
		})
	}
}
