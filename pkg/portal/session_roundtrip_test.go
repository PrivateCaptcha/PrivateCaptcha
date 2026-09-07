package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5/pgtype"
)

type countingSessionQuerier struct {
	dbgen.Querier
	calls atomic.Int32
}

func (q *countingSessionQuerier) GetLiveSession(ctx context.Context, sid string) (*dbgen.GetLiveSessionRow, error) {
	q.calls.Add(1)
	return q.Querier.GetLiveSession(ctx, sid)
}

func (q *countingSessionQuerier) ConsumeSignInChallenge(
	ctx context.Context,
	arg *dbgen.ConsumeSignInChallengeParams,
) (*dbgen.ConsumeSignInChallengeRow, error) {
	q.calls.Add(1)
	return q.Querier.ConsumeSignInChallenge(ctx, arg)
}

func (q *countingSessionQuerier) ConsumeEmailChangeChallenge(
	ctx context.Context,
	arg *dbgen.ConsumeEmailChangeChallengeParams,
) (*dbgen.ConsumeEmailChangeChallengeRow, error) {
	q.calls.Add(1)
	return q.Querier.ConsumeEmailChangeChallenge(ctx, arg)
}

func (q *countingSessionQuerier) ConsumeRegistrationChallenge(
	ctx context.Context,
	arg *dbgen.ConsumeRegistrationChallengeParams,
) (*dbgen.ConsumeRegistrationChallengeRow, error) {
	q.calls.Add(1)
	return q.Querier.ConsumeRegistrationChallenge(ctx, arg)
}

func (q *countingSessionQuerier) CreateRegistrationSuccessor(ctx context.Context, arg *dbgen.CreateRegistrationSuccessorParams) (*dbgen.Session, error) {
	q.calls.Add(1)
	return q.Querier.CreateRegistrationSuccessor(ctx, arg)
}

func (q *countingSessionQuerier) InspectSessionChallenge(ctx context.Context, sid string) (*dbgen.InspectSessionChallengeRow, error) {
	q.calls.Add(1)
	return q.Querier.InspectSessionChallenge(ctx, sid)
}

func (q *countingSessionQuerier) IssueEmailChangeChallenge(ctx context.Context, arg *dbgen.IssueEmailChangeChallengeParams) (*dbgen.Session, error) {
	q.calls.Add(1)
	return q.Querier.IssueEmailChangeChallenge(ctx, arg)
}

func (q *countingSessionQuerier) IssueRegistrationChallenge(ctx context.Context, arg *dbgen.IssueRegistrationChallengeParams) (*dbgen.Session, error) {
	q.calls.Add(1)
	return q.Querier.IssueRegistrationChallenge(ctx, arg)
}

func (q *countingSessionQuerier) IssueSignInChallenge(ctx context.Context, arg *dbgen.IssueSignInChallengeParams) (*dbgen.Session, error) {
	q.calls.Add(1)
	return q.Querier.IssueSignInChallenge(ctx, arg)
}

func (q *countingSessionQuerier) ResendPendingChallenge(ctx context.Context, arg *dbgen.ResendPendingChallengeParams) (*dbgen.Session, error) {
	q.calls.Add(1)
	return q.Querier.ResendPendingChallenge(ctx, arg)
}

func (q *countingSessionQuerier) RevokeSession(ctx context.Context, sid string) (*dbgen.RevokeSessionRow, error) {
	q.calls.Add(1)
	return q.Querier.RevokeSession(ctx, sid)
}

func (q *countingSessionQuerier) RevokeUserSessions(ctx context.Context, userID pgtype.Int4) ([]*dbgen.RevokeUserSessionsRow, error) {
	q.calls.Add(1)
	return q.Querier.RevokeUserSessions(ctx, userID)
}

func (q *countingSessionQuerier) RenewSessionExpirations(
	ctx context.Context,
	arg *dbgen.RenewSessionExpirationsParams,
) ([]*dbgen.RenewSessionExpirationsRow, error) {
	q.calls.Add(1)
	return q.Querier.RenewSessionExpirations(ctx, arg)
}

func (q *countingSessionQuerier) UpdateSessionPayloads(ctx context.Context, arg *dbgen.UpdateSessionPayloadsParams) ([]*dbgen.UpdateSessionPayloadsRow, error) {
	q.calls.Add(1)
	return q.Querier.UpdateSessionPayloads(ctx, arg)
}

type roundtripFixture struct {
	prefix       string
	userID       int32
	querier      *countingSessionQuerier
	manager      *session.Manager
	sessionStore *db.SessionStore
}

func newRoundtripFixture(t *testing.T) *roundtripFixture {
	t.Helper()
	prefix := transitionTestPrefix(t)
	userID, _ := transitionTestUser(t, prefix)
	querier := &countingSessionQuerier{Querier: dbgen.New(store.Pool)}
	business := db.NewBusinessWithQuerier(
		store.Pool,
		querier,
		db.NewStaticCache[db.CacheKey, any](100, &db.CacheMissingValue{}),
	)
	sessionStore := db.NewSessionStore(business, server.Metrics)
	return &roundtripFixture{
		prefix: prefix, userID: userID, querier: querier, sessionStore: sessionStore,
		manager: &session.Manager{CookieName: "pcsid", Store: sessionStore, MaxLifetime: 3 * time.Hour, Path: "/portal"},
	}
}

func (f *roundtripFixture) cookie(suffix string) *http.Cookie {
	return &http.Cookie{Name: f.manager.CookieName, Value: url.QueryEscape(f.prefix + "-" + suffix)}
}

func (f *roundtripFixture) insertAuthenticated(t *testing.T, suffix string) *http.Cookie {
	t.Helper()
	cookie := f.cookie(suffix)
	sid, _ := url.QueryUnescape(cookie.Value)
	payload, err := session.NewPayload(sid, f.sessionStore).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool.Exec(t.Context(), `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at)
		VALUES ($1, 'authenticated', $2, $3, NOW() + INTERVAL '1 hour')
	`, sid, f.userID, payload); err != nil {
		t.Fatal(err)
	}
	return cookie
}

func (f *roundtripFixture) insertPending(t *testing.T, suffix string, kind session.ChallengeKind, payload []byte) *http.Cookie {
	t.Helper()
	cookie := f.cookie(suffix)
	sid, _ := url.QueryUnescape(cookie.Value)
	var userID any = f.userID
	var requiresVerification any
	if kind == session.ChallengeKindRegistration {
		userID = nil
		requiresVerification = false
	}
	if _, err := store.Pool.Exec(t.Context(), `
		INSERT INTO backend.sessions (
			session_id, state, user_id, data, expires_at,
			challenge_kind, challenge_code, challenge_email, challenge_expires_at,
			verify_registration
		) VALUES ($1, 'pending', $2, $3, NOW() + INTERVAL '3 hours', $4, '111111', $5, NOW() + INTERVAL '15 minutes', $6)
	`, sid, userID, payload, kind, f.prefix+"@privatecaptcha.com", requiresVerification); err != nil {
		t.Fatal(err)
	}
	return cookie
}

func (f *roundtripFixture) assertCalls(t *testing.T, want int32) {
	t.Helper()
	if got := f.querier.calls.Load(); got != want {
		t.Fatalf("session-authority roundtrips = %d, want %d", got, want)
	}
}

func requestWithSessionCookie(method, path string, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(cookie)
	return req
}

func TestCriticalSessionAuthorityRoundtripBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("warmPrivateWrite", func(t *testing.T) {
		f := newRoundtripFixture(t)
		cookie := f.insertAuthenticated(t, "warm-write")
		if _, err := f.manager.Get(requestWithSessionCookie(http.MethodGet, "/portal", cookie)); err != nil {
			t.Fatal(err)
		}
		f.querier.calls.Store(0)
		localServer := &Server{
			Sessions:    f.manager,
			XSRF:        server.XSRF,
			RateLimiter: server.RateLimiter,
		}
		handled := false
		next := localServer.private(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handled = true }))
		handler := localServer.csrf(localServer.csrfAuthenticatedUserAuthorityKeyFunc)(next)
		req := requestWithSessionCookie(http.MethodPut, "/portal/settings", cookie)
		req.Header.Set(common.HeaderCSRFToken, localServer.XSRF.Token(strconv.Itoa(int(f.userID))))
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if !handled {
			t.Fatal("warm private-write middleware rejected the authenticated request")
		}
		f.assertCalls(t, 0)
	})

	t.Run("coldSignInConsume", func(t *testing.T) {
		f := newRoundtripFixture(t)
		payload, err := session.NewPayload("", f.sessionStore).Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		cookie := f.insertPending(t, "sign-in", session.ChallengeKindSignIn, payload)
		f.querier.calls.Store(0)
		req := requestWithSessionCookie(http.MethodPost, "/portal/2fa", cookie)
		pending, err := f.manager.Get(req)
		if err != nil {
			t.Fatal(err)
		}
		result, err := f.manager.ConsumeSignInChallenge(httptest.NewRecorder(), req, pending, "111111", 5)
		if err != nil || result.Outcome != session.TransitionSucceeded || result.Session == nil {
			t.Fatalf("sign-in consume = (%+v, %v), want authenticated successor", result, err)
		}
		authority, _ := result.Session.Authority()
		if authority.State != session.StateAuthenticated || authority.UserID != f.userID {
			t.Fatalf("sign-in successor Authority = %+v", authority)
		}
		if calls := f.querier.calls.Load(); calls > 2 {
			t.Fatalf("cold sign-in roundtrips = %d, want at most 2", calls)
		}
	})

	for _, tt := range []struct {
		name string
		warm bool
		want int32
	}{
		{name: "warmExhaustedResend", warm: true, want: 2},
		{name: "coldExhaustedResend", want: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRoundtripFixture(t)
			payload, err := session.NewPayload("", f.sessionStore).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			cookie := f.insertPending(t, "exhausted-resend", session.ChallengeKindSignIn, payload)
			sid, _ := url.QueryUnescape(cookie.Value)
			if _, err := store.Pool.Exec(t.Context(),
				"UPDATE backend.sessions SET failed_attempts = 5 WHERE session_id = $1", sid,
			); err != nil {
				t.Fatal(err)
			}
			pending, err := f.manager.Get(requestWithSessionCookie(http.MethodPost, "/portal/resend", cookie))
			if err != nil {
				t.Fatal(err)
			}
			if tt.warm {
				f.querier.calls.Store(0)
			}
			result, err := f.manager.ResendPendingChallenge(t.Context(), pending, "222222", 15*time.Minute, 5)
			if err != nil || result.Outcome != session.TransitionAttemptsExhausted {
				t.Fatalf("exhausted resend = (%+v, %v), want attempts_exhausted", result, err)
			}
			f.assertCalls(t, tt.want)
		})
	}

	t.Run("registrationCompletion", func(t *testing.T) {
		f := newRoundtripFixture(t)
		cookie := f.insertPending(t, "registration", session.ChallengeKindRegistration, transitionRegistrationPayload(t, "Registrant", 0))
		req := requestWithSessionCookie(http.MethodPost, "/portal/2fa", cookie)
		pending, err := f.manager.Get(req)
		if err != nil {
			t.Fatal(err)
		}
		f.querier.calls.Store(0)
		consumed, err := f.manager.ConsumeRegistrationChallenge(t.Context(), pending, "111111", 5)
		if err != nil || consumed.Outcome != session.TransitionSucceeded {
			t.Fatalf("registration consume = (%+v, %v), want succeeded", consumed, err)
		}
		successor, err := f.manager.CreateRegistrationSuccessor(httptest.NewRecorder(), req, pending, f.userID)
		if err != nil || successor.Outcome != session.TransitionSucceeded || successor.Session == nil {
			t.Fatalf("registration successor = (%+v, %v), want authenticated session", successor, err)
		}
		f.assertCalls(t, 2)
	})

	t.Run("logout", func(t *testing.T) {
		f := newRoundtripFixture(t)
		cookie := f.insertAuthenticated(t, "logout")
		f.querier.calls.Store(0)
		w := httptest.NewRecorder()
		result, err := f.manager.Revoke(w, requestWithSessionCookie(http.MethodGet, "/portal/logout", cookie))
		if err != nil || result == nil || result.State != session.StateRevoked || result.UserID != f.userID {
			t.Fatalf("logout = (%+v, %v), want revoked user session", result, err)
		}
		if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
			t.Fatalf("logout cookie = %v, want cleared", cookies)
		}
		f.assertCalls(t, 1)
	})
}
