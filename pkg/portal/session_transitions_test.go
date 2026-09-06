package portal

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5"
)

type sessionChallengeQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func assertSessionChallengeCodeCleared(t *testing.T, querier sessionChallengeQuerier, sid string) {
	t.Helper()
	var cleared bool
	if err := querier.QueryRow(t.Context(),
		"SELECT challenge_code IS NULL FROM backend.sessions WHERE session_id = $1", sid,
	).Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatalf("session %q retained challenge code", sid)
	}
}

func TestAuthoritativeSessionManagerSignInLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	prefix := transitionTestPrefix(t)
	userID, email := transitionTestUser(t, prefix)
	sessionStore := db.NewSessionStore(store, server.Metrics)
	manager := &session.Manager{CookieName: "pcsid", Store: sessionStore, MaxLifetime: 3 * time.Hour, Path: "/portal"}
	req := httptest.NewRequest(http.MethodPost, "/portal/login", nil)
	w := httptest.NewRecorder()

	anonymous, err := manager.Start(w, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := anonymous.Set(req.Context(), session.KeyUserName, "Signed In User"); err != nil {
		t.Fatal(err)
	}

	issueW := httptest.NewRecorder()
	issued, err := manager.IssueSignInChallenge(issueW, req, anonymous, userID, "111111", 15*time.Minute, 5)
	if err != nil || issued.Outcome != session.TransitionSucceeded || issued.Session == nil {
		t.Fatalf("issue = (%+v, %v), want succeeded", issued, err)
	}
	pendingAuthority, ok := issued.Session.Authority()
	if !ok || pendingAuthority.State != session.StatePending || pendingAuthority.UserID != userID || pendingAuthority.ChallengeEmail != email {
		t.Fatalf("pending Authority = %+v", pendingAuthority)
	}
	if issueW.Result().Cookies()[0].Value != issued.Session.ID() {
		t.Fatalf("pending cookie = %v", issueW.Result().Cookies())
	}
	pendingReq := httptest.NewRequest(http.MethodPost, "/portal/2fa", nil)
	pendingReq.AddCookie(issueW.Result().Cookies()[0])
	authorityServer := &Server{Sessions: manager}
	if key := authorityServer.csrfPendingEmailAuthorityKeyFunc(httptest.NewRecorder(), pendingReq); key != email {
		t.Fatalf("pending CSRF key = %q, want %q", key, email)
	}

	failed, err := manager.ConsumeSignInChallenge(httptest.NewRecorder(), req, issued.Session, "000000", 5)
	if err != nil || failed.Outcome != session.TransitionInvalidCode || failed.FailedAttempts != 1 {
		t.Fatalf("failed consume = (%+v, %v), want first invalid attempt", failed, err)
	}
	var challengeCodeRetained bool
	if err := store.Pool.QueryRow(req.Context(),
		"SELECT challenge_code = '111111' FROM backend.sessions WHERE session_id = $1", issued.Session.ID(),
	).Scan(&challengeCodeRetained); err != nil || !challengeCodeRetained {
		t.Fatalf("challenge code retained after invalid attempt = (%t, %v), want true", challengeCodeRetained, err)
	}
	resent, err := manager.ResendPendingChallenge(req.Context(), failed.Session, "222222", 15*time.Minute, 5)
	if err != nil || resent.Outcome != session.TransitionSucceeded || resent.Session == nil {
		t.Fatalf("resend = (%+v, %v), want succeeded", resent, err)
	}

	successW := httptest.NewRecorder()
	succeeded, err := manager.ConsumeSignInChallenge(successW, req, resent.Session, "222222", 5)
	if err != nil || succeeded.Outcome != session.TransitionSucceeded || succeeded.Session == nil {
		t.Fatalf("successful consume = (%+v, %v), want authenticated successor", succeeded, err)
	}
	assertSessionChallengeCodeCleared(t, store.Pool, issued.Session.ID())
	if succeeded.Session.ID() == anonymous.ID() {
		t.Fatal("authenticated successor reused predecessor SID")
	}
	successorAuthority, ok := succeeded.Session.Authority()
	if !ok || successorAuthority.State != session.StateAuthenticated || successorAuthority.UserID != userID {
		t.Fatalf("successor Authority = %+v", successorAuthority)
	}

	successCookie := successW.Result().Cookies()[0]
	authenticatedReq := httptest.NewRequest(http.MethodGet, "/portal/dashboard", nil)
	authenticatedReq.AddCookie(successCookie)
	if key := authorityServer.csrfAuthenticatedUserAuthorityKeyFunc(httptest.NewRecorder(), authenticatedReq); key != fmt.Sprint(userID) {
		t.Fatalf("authenticated CSRF key = %q, want %d", key, userID)
	}
	resolved, err := manager.Get(authenticatedReq)
	if err != nil || resolved.ID() != succeeded.Session.ID() {
		t.Fatalf("resolved successor = (%v, %v), want SID %q", resolved, err, succeeded.Session.ID())
	}
	resolvedAuthority, ok := resolved.Authority()
	if !ok || resolvedAuthority.State != session.StateAuthenticated || resolvedAuthority.UserID != userID ||
		resolved.Get(req.Context(), session.KeyUserName) != "Signed In User" {
		t.Fatalf("resolved successor state = (%+v, %v)", resolvedAuthority, resolved.Get(req.Context(), session.KeyUserName))
	}
	emailIssued, err := manager.IssueEmailChangeChallenge(req.Context(), resolved, "333333", 15*time.Minute)
	if err != nil || emailIssued.Outcome != session.TransitionSucceeded || emailIssued.Session == nil {
		t.Fatalf("email-change issue = (%+v, %v), want succeeded", emailIssued, err)
	}
	emailFailed, err := manager.ConsumeEmailChangeChallenge(req.Context(), emailIssued.Session, "000000", 5)
	if err != nil || emailFailed.Outcome != session.TransitionInvalidCode || emailFailed.FailedAttempts != 1 {
		t.Fatalf("email-change failure = (%+v, %v), want first invalid attempt", emailFailed, err)
	}
	emailConsumed, err := manager.ConsumeEmailChangeChallenge(req.Context(), emailFailed.Session, "333333", 5)
	if err != nil || emailConsumed.Outcome != session.TransitionSucceeded || emailConsumed.Session == nil {
		t.Fatalf("email-change consume = (%+v, %v), want succeeded", emailConsumed, err)
	}

	revokeW := httptest.NewRecorder()
	if _, err := manager.Revoke(revokeW, authenticatedReq); err != nil {
		t.Fatal(err)
	}
	if revokeW.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("revocation cookie = %v", revokeW.Result().Cookies())
	}
	if _, err := manager.Get(authenticatedReq); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("revoked session error = %v, want %v", err, session.ErrSessionMissing)
	}
	if _, err := manager.Revoke(httptest.NewRecorder(), authenticatedReq); err != nil {
		t.Fatalf("repeated revocation failed: %v", err)
	}
	missingReq := httptest.NewRequest(http.MethodGet, "/portal/logout", nil)
	missingReq.AddCookie(&http.Cookie{Name: manager.CookieName, Value: "missing-sid"})
	if _, err := manager.Revoke(httptest.NewRecorder(), missingReq); err != nil {
		t.Fatalf("missing-session revocation failed: %v", err)
	}
}

func TestAuthoritativeSessionManagerRegistrationLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	prefix := transitionTestPrefix(t)
	registrationEmail := prefix + "-registration@privatecaptcha.com"
	sessionStore := db.NewSessionStore(store, server.Metrics)
	manager := &session.Manager{CookieName: "pcsid", Store: sessionStore, MaxLifetime: 3 * time.Hour, Path: "/portal"}
	req := httptest.NewRequest(http.MethodPost, "/portal/register", nil)
	anonymous, err := manager.Start(httptest.NewRecorder(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := anonymous.Set(req.Context(), session.KeyUserName, "Registrant"); err != nil {
		t.Fatal(err)
	}

	issueW := httptest.NewRecorder()
	issued, err := manager.IssueRegistrationChallenge(issueW, req, anonymous, registrationEmail, "111111", 15*time.Minute, 5)
	if err != nil || issued.Outcome != session.TransitionSucceeded || issued.Session == nil {
		t.Fatalf("registration issue = (%+v, %v), want succeeded", issued, err)
	}
	if issueW.Result().Cookies()[0].Value != issued.Session.ID() {
		t.Fatalf("registration cookie = %v", issueW.Result().Cookies())
	}

	failed, err := manager.ConsumeRegistrationChallenge(req.Context(), issued.Session, "000000", 5)
	if err != nil || failed.Outcome != session.TransitionInvalidCode || failed.FailedAttempts != 1 {
		t.Fatalf("registration failure = (%+v, %v), want first invalid attempt", failed, err)
	}
	consumed, err := manager.ConsumeRegistrationChallenge(req.Context(), issued.Session, "111111", 5)
	if err != nil || consumed.Outcome != session.TransitionSucceeded || consumed.Email != registrationEmail || consumed.Name != "Registrant" {
		t.Fatalf("registration consume = (%+v, %v), want authoritative registration data", consumed, err)
	}
	assertSessionChallengeCodeCleared(t, store.Pool, issued.Session.ID())

	userID, _ := transitionTestUser(t, prefix)
	successW := httptest.NewRecorder()
	successor, err := manager.CreateRegistrationSuccessor(successW, req, issued.Session, userID)
	if err != nil || successor.Outcome != session.TransitionSucceeded || successor.Session == nil {
		t.Fatalf("registration successor = (%+v, %v), want succeeded", successor, err)
	}
	if successor.Session.ID() == issued.Session.ID() {
		t.Fatal("registration successor reused predecessor SID")
	}
	authority, ok := successor.Session.Authority()
	if !ok || authority.State != session.StateAuthenticated || authority.UserID != userID {
		t.Fatalf("registration successor Authority = %+v", authority)
	}
	if successW.Result().Cookies()[0].Value != successor.Session.ID() {
		t.Fatalf("registration successor cookie = %v", successW.Result().Cookies())
	}
	authenticatedReq := httptest.NewRequest(http.MethodGet, "/portal/dashboard", nil)
	authenticatedReq.AddCookie(successW.Result().Cookies()[0])
	if _, err := manager.Get(authenticatedReq); err != nil {
		t.Fatal(err)
	}
	if err := manager.RevokeUserSessions(req.Context(), userID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(authenticatedReq); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("user-revoked session error = %v, want %v", err, session.ErrSessionMissing)
	}
}

func transitionTestPrefix(t *testing.T) string {
	t.Helper()
	prefix := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), "DELETE FROM backend.sessions WHERE session_id LIKE $1", prefix+"%")
		_, _ = store.Pool.Exec(context.Background(), "DELETE FROM backend.users WHERE email LIKE $1", prefix+"%")
	})
	return prefix
}

func transitionTestUser(t *testing.T, prefix string) (int32, string) {
	t.Helper()
	email := prefix + "@privatecaptcha.com"
	var userID int32
	if err := store.Pool.QueryRow(t.Context(),
		"INSERT INTO backend.users (name, email) VALUES ($1, $2) RETURNING id", t.Name(), email,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return userID, email
}

func transitionRegistrationPayload(t *testing.T, name string, inviteID int32) []byte {
	t.Helper()
	values := map[session.SessionKey]session.SessionValue{
		session.KeyUserName:    name,
		session.KeyOrgInviteID: inviteID,
	}
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(values); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestSessionSignInTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	prefix := transitionTestPrefix(t)
	sid := prefix + "-pending"
	userID, _ := transitionTestUser(t, prefix)
	primary := store.Impl()
	secondary := db.NewBusiness(store.Pool).Impl()

	issued, err := primary.IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: sid, UserID: userID, ChallengeCode: db.Text("111111"), Data: []byte("pending"),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || issued.Outcome != session.TransitionSucceeded || issued.Session == nil {
		t.Fatalf("issue = (%+v, %v), want succeeded", issued, err)
	}

	missing, err := primary.ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
		SessionID: prefix + "-missing", SuccessorSessionID: prefix + "-unused", ChallengeCode: db.Text("111111"),
		SuccessorData: []byte("authenticated"), SuccessorTtl: 3 * time.Hour, MaxAttempts: 2,
	})
	if err != nil || missing.Outcome != session.TransitionMissing {
		t.Fatalf("missing consume = (%+v, %v), want missing", missing, err)
	}

	firstFailure, err := primary.ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
		SessionID: sid, SuccessorSessionID: prefix + "-unused", ChallengeCode: db.Text("000000"),
		SuccessorData: []byte("authenticated"), SuccessorTtl: 3 * time.Hour, MaxAttempts: 2,
	})
	if err != nil || firstFailure.Outcome != session.TransitionInvalidCode || firstFailure.FailedAttempts != 1 {
		t.Fatalf("first attempt = (%+v, %v), want invalid_code", firstFailure, err)
	}
	reissued, err := primary.IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: sid, UserID: userID, ChallengeCode: db.Text("222222"), Data: []byte("pending"),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || reissued.Outcome != session.TransitionSucceeded {
		t.Fatalf("reissue = (%+v, %v), want succeeded", reissued, err)
	}
	secondFailure, err := secondary.ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
		SessionID: sid, SuccessorSessionID: prefix + "-unused", ChallengeCode: db.Text("000000"),
		SuccessorData: []byte("authenticated"), SuccessorTtl: 3 * time.Hour, MaxAttempts: 2,
	})
	if err != nil || secondFailure.Outcome != session.TransitionAttemptsExhausted || secondFailure.FailedAttempts != 2 {
		t.Fatalf("second attempt after reissue = (%+v, %v), want attempts_exhausted", secondFailure, err)
	}
	exhausted, err := primary.ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
		SessionID: sid, SuccessorSessionID: prefix + "-unused", ChallengeCode: db.Text("222222"),
		SuccessorData: []byte("authenticated"), SuccessorTtl: 3 * time.Hour, MaxAttempts: 2,
	})
	if err != nil || exhausted.Outcome != session.TransitionAttemptsExhausted {
		t.Fatalf("exhausted consume = (%+v, %v), want attempts_exhausted", exhausted, err)
	}

	resent, err := primary.ResendPendingChallenge(ctx, &dbgen.ResendPendingChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("333333"), ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || resent.Outcome != session.TransitionAttemptsExhausted {
		t.Fatalf("exhausted resend = (%+v, %v), want attempts_exhausted", resent, err)
	}
	reissued, err = primary.IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: sid, UserID: userID, ChallengeCode: db.Text("333333"), Data: []byte("pending"),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || reissued.Outcome != session.TransitionAttemptsExhausted {
		t.Fatalf("exhausted reissue = (%+v, %v), want attempts_exhausted", reissued, err)
	}
	emailChange, err := primary.ConsumeEmailChangeChallenge(ctx, &dbgen.ConsumeEmailChangeChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("333333"), MaxAttempts: 2,
	})
	if err != nil || emailChange.Outcome != session.TransitionStale {
		t.Fatalf("email-change consume against exhausted sign-in = (%+v, %v), want stale", emailChange, err)
	}

	freshSID := prefix + "-fresh"
	if _, err := primary.IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: freshSID, UserID: userID, ChallengeCode: db.Text("333333"), Data: []byte("pending"),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	}); err != nil {
		t.Fatal(err)
	}
	freshFailure, err := primary.ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
		SessionID: freshSID, SuccessorSessionID: prefix + "-fresh-successor", ChallengeCode: db.Text("000000"),
		SuccessorData: []byte("authenticated"), SuccessorTtl: 3 * time.Hour, MaxAttempts: 2,
	})
	if err != nil || freshFailure.Outcome != session.TransitionInvalidCode || freshFailure.FailedAttempts != 1 {
		t.Fatalf("fresh SID first attempt = (%+v, %v), want invalid_code", freshFailure, err)
	}

	concurrentSID := prefix + "-concurrent"
	concurrentIssue, err := primary.IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: concurrentSID, UserID: userID, ChallengeCode: db.Text("444444"), Data: []byte("pending"),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || concurrentIssue.Outcome != session.TransitionSucceeded {
		t.Fatalf("concurrent issue = (%+v, %v), want succeeded", concurrentIssue, err)
	}

	type call struct {
		result *session.SignInChallengeConsumeResult
		err    error
	}
	start := make(chan struct{})
	calls := make(chan call, 2)
	for i, impl := range []*db.BusinessStoreImpl{primary, secondary} {
		successorID := fmt.Sprintf("%s-successor-%d", prefix, i)
		go func() {
			<-start
			result, err := impl.ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
				SessionID: concurrentSID, SuccessorSessionID: successorID, ChallengeCode: db.Text("444444"),
				SuccessorData: []byte("authenticated"), SuccessorTtl: 3 * time.Hour, MaxAttempts: 2,
			})
			calls <- call{result: result, err: err}
		}()
	}
	close(start)

	succeeded, stale := 0, 0
	for range 2 {
		call := <-calls
		if call.err != nil {
			t.Fatal(call.err)
		}
		switch call.result.Outcome {
		case session.TransitionSucceeded:
			succeeded++
			if call.result.Successor == nil || call.result.Successor.UserID != userID {
				t.Fatalf("successful consume has no bound successor: %+v", call.result)
			}
		case session.TransitionStale:
			stale++
		default:
			t.Fatalf("unexpected concurrent result: %+v", call.result)
		}
	}
	if succeeded != 1 || stale != 1 {
		t.Fatalf("concurrent results = (%d succeeded, %d stale), want (1, 1)", succeeded, stale)
	}

	expiredSID := prefix + "-expired"
	if _, err := primary.IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: expiredSID, UserID: userID, ChallengeCode: db.Text("555555"), Data: []byte("pending"),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool.Exec(ctx,
		"UPDATE backend.sessions SET challenge_expires_at = NOW() - INTERVAL '1 second' WHERE session_id = $1", expiredSID,
	); err != nil {
		t.Fatal(err)
	}
	expiredResend, err := primary.ResendPendingChallenge(ctx, &dbgen.ResendPendingChallengeParams{
		SessionID: expiredSID, ChallengeCode: db.Text("666666"), ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || expiredResend.Outcome != session.TransitionExpired {
		t.Fatalf("expired resend = (%+v, %v), want expired", expiredResend, err)
	}
	expired, err := primary.ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
		SessionID: expiredSID, SuccessorSessionID: prefix + "-expired-successor", ChallengeCode: db.Text("555555"),
		SuccessorData: []byte("authenticated"), SuccessorTtl: 3 * time.Hour, MaxAttempts: 2,
	})
	if err != nil || expired.Outcome != session.TransitionExpired {
		t.Fatalf("expired consume = (%+v, %v), want expired", expired, err)
	}
}

func TestSessionRegistrationTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	prefix := transitionTestPrefix(t)
	email := prefix + "@privatecaptcha.com"
	impl := store.Impl()
	sessionStore := db.NewSessionStore(store, server.Metrics)

	requiredSID := prefix + "-registration-required"
	issued, err := impl.IssueRegistrationChallenge(ctx, &dbgen.IssueRegistrationChallengeParams{
		SessionID: requiredSID, ChallengeEmail: db.Text(email), ChallengeCode: db.Text("111111"), Data: transitionRegistrationPayload(t, "Registrant", 42),
		InviteID: db.Int(42), SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 3,
	})
	if err != nil || issued.Outcome != session.TransitionSucceeded {
		t.Fatalf("issue = (%+v, %v), want succeeded", issued, err)
	}
	pending, err := sessionStore.Resolve(ctx, requiredSID)
	if err != nil {
		t.Fatal(err)
	}
	beforeAuthority, _ := pending.Authority()
	if beforeAuthority.VerifyRegistration {
		t.Fatal("new registration unexpectedly requires verification")
	}
	if err := sessionStore.SetVerifyRegistration(ctx, requiredSID); err != nil {
		t.Fatal(err)
	}
	marked, err := sessionStore.Resolve(ctx, requiredSID)
	if err != nil {
		t.Fatal(err)
	}
	markedAuthority, _ := marked.Authority()
	if !markedAuthority.VerifyRegistration || marked == pending {
		t.Fatalf("marked registration Authority = %+v", markedAuthority)
	}
	if current, _ := pending.Authority(); current.VerifyRegistration {
		t.Fatal("marking registration mutated the previous Authority snapshot")
	}
	issued, err = impl.IssueRegistrationChallenge(ctx, &dbgen.IssueRegistrationChallengeParams{
		SessionID: requiredSID, ChallengeEmail: db.Text(email), ChallengeCode: db.Text("222222"), Data: transitionRegistrationPayload(t, "Registrant", 42),
		InviteID: db.Int(42), SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 3,
	})
	if err != nil || issued.Session == nil || !issued.Session.VerifyRegistration {
		t.Fatalf("marked reissue = (%+v, %v), want verification preserved", issued, err)
	}
	required, err := impl.ConsumeRegistrationChallenge(ctx, &dbgen.ConsumeRegistrationChallengeParams{
		SessionID: requiredSID, ChallengeCode: db.Text("222222"), MaxAttempts: 3,
	})
	if err != nil || required.Outcome != session.TransitionVerificationRequired {
		t.Fatalf("consume = (%+v, %v), want verification_required", required, err)
	}
	if _, err := sessionStore.RevokeSession(ctx, requiredSID); err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.SetVerifyRegistration(ctx, requiredSID); !errors.Is(err, db.ErrRecordNotFound) {
		t.Fatalf("late registration check error = %v, want %v", err, db.ErrRecordNotFound)
	}
	if _, err := sessionStore.Resolve(ctx, requiredSID); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("late registration check restored revoked session: %v", err)
	}

	sid := prefix + "-registration"
	issued, err = impl.IssueRegistrationChallenge(ctx, &dbgen.IssueRegistrationChallengeParams{
		SessionID: sid, ChallengeEmail: db.Text(email), ChallengeCode: db.Text("111111"), Data: transitionRegistrationPayload(t, "Registrant", 42),
		InviteID: db.Int(42), SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 3,
	})
	if err != nil || issued.Outcome != session.TransitionSucceeded || issued.Session.VerifyRegistration {
		t.Fatalf("ordinary issue = (%+v, %v), want unmarked registration", issued, err)
	}
	firstFailure, err := impl.ConsumeRegistrationChallenge(ctx, &dbgen.ConsumeRegistrationChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("000000"), MaxAttempts: 3,
	})
	if err != nil || firstFailure.Outcome != session.TransitionInvalidCode || firstFailure.FailedAttempts != 1 {
		t.Fatalf("first invalid consume = (%+v, %v), want invalid_code", firstFailure, err)
	}

	issued, err = impl.IssueRegistrationChallenge(ctx, &dbgen.IssueRegistrationChallengeParams{
		SessionID: sid, ChallengeEmail: db.Text(email), ChallengeCode: db.Text("222222"), Data: transitionRegistrationPayload(t, "Registrant", 42),
		InviteID: db.Int(42), SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 3,
	})
	if err != nil || issued.Session == nil {
		t.Fatalf("reissue = (%+v, %v), want succeeded", issued, err)
	}
	updated, err := impl.UpdateSessionPayloads(ctx, []session.PayloadUpdate{{
		SessionID: sid, ExpectedVersion: issued.Session.Version, Payload: transitionRegistrationPayload(t, "Registrant", 999),
	}})
	if err != nil || len(updated) != 1 {
		t.Fatalf("Payload update = (%+v, %v), want one update", updated, err)
	}

	invalid, err := impl.ConsumeRegistrationChallenge(ctx, &dbgen.ConsumeRegistrationChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("000000"), MaxAttempts: 3,
	})
	if err != nil || invalid.Outcome != session.TransitionInvalidCode || invalid.FailedAttempts != 2 {
		t.Fatalf("invalid consume after reissue = (%+v, %v), want second cumulative invalid attempt", invalid, err)
	}
	consumed, err := impl.ConsumeRegistrationChallenge(ctx, &dbgen.ConsumeRegistrationChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("222222"), MaxAttempts: 3,
	})
	if err != nil || consumed.Outcome != session.TransitionSucceeded || consumed.Email != email ||
		consumed.Name != "Registrant" || consumed.InviteID != 42 || consumed.Session == nil ||
		consumed.Session.State != session.StateRevoked {
		t.Fatalf("successful consume = (%+v, %v), want authoritative registration data", consumed, err)
	}
	repeated, err := impl.ConsumeRegistrationChallenge(ctx, &dbgen.ConsumeRegistrationChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("222222"), MaxAttempts: 3,
	})
	if err != nil || repeated.Outcome != session.TransitionStale {
		t.Fatalf("repeated consume = (%+v, %v), want stale", repeated, err)
	}

	userID, _ := transitionTestUser(t, prefix)
	exhaustedSID := prefix + "-registration-exhausted"
	if _, err := impl.IssueRegistrationChallenge(ctx, &dbgen.IssueRegistrationChallengeParams{
		SessionID: exhaustedSID, ChallengeEmail: db.Text(email), ChallengeCode: db.Text("333333"), Data: transitionRegistrationPayload(t, "Registrant", 0),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	}); err != nil {
		t.Fatal(err)
	}
	firstExhaustion, err := impl.ConsumeRegistrationChallenge(ctx, &dbgen.ConsumeRegistrationChallengeParams{
		SessionID: exhaustedSID, ChallengeCode: db.Text("000000"), MaxAttempts: 2,
	})
	if err != nil || firstExhaustion.Outcome != session.TransitionInvalidCode || firstExhaustion.FailedAttempts != 1 {
		t.Fatalf("first exhaustion attempt = (%+v, %v), want invalid_code", firstExhaustion, err)
	}
	registrationResend, err := impl.ResendPendingChallenge(ctx, &dbgen.ResendPendingChallengeParams{
		SessionID: exhaustedSID, ChallengeCode: db.Text("444444"), ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || registrationResend.Outcome != session.TransitionSucceeded {
		t.Fatalf("registration resend = (%+v, %v), want succeeded", registrationResend, err)
	}
	finalExhaustion, err := impl.ConsumeRegistrationChallenge(ctx, &dbgen.ConsumeRegistrationChallengeParams{
		SessionID: exhaustedSID, ChallengeCode: db.Text("000000"), MaxAttempts: 2,
	})
	if err != nil || finalExhaustion.Outcome != session.TransitionAttemptsExhausted || finalExhaustion.FailedAttempts != 2 {
		t.Fatalf("final exhaustion attempt = (%+v, %v), want attempts_exhausted", finalExhaustion, err)
	}
	if _, err := store.Pool.Exec(ctx,
		"UPDATE backend.sessions SET challenge_expires_at = NOW() - INTERVAL '1 second' WHERE session_id = $1", exhaustedSID,
	); err != nil {
		t.Fatal(err)
	}
	exhaustedResend, err := impl.ResendPendingChallenge(ctx, &dbgen.ResendPendingChallengeParams{
		SessionID: exhaustedSID, ChallengeCode: db.Text("555555"), ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || exhaustedResend.Outcome != session.TransitionAttemptsExhausted {
		t.Fatalf("expired exhausted resend = (%+v, %v), want attempts_exhausted", exhaustedResend, err)
	}
	crossKindIssue, err := impl.IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: exhaustedSID, UserID: userID, ChallengeCode: db.Text("555555"), Data: []byte("pending"),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 2,
	})
	if err != nil || crossKindIssue.Outcome != session.TransitionAttemptsExhausted {
		t.Fatalf("cross-kind exhausted reissue = (%+v, %v), want attempts_exhausted", crossKindIssue, err)
	}

	successor, err := impl.CreateRegistrationSuccessor(ctx, &dbgen.CreateRegistrationSuccessorParams{
		SessionID: prefix + "-successor", UserID: userID, Data: []byte("authenticated"), SessionTtl: 3 * time.Hour,
	})
	if err != nil || successor.Outcome != session.TransitionSucceeded || successor.Session == nil ||
		successor.Session.State != session.StateAuthenticated || successor.Session.UserID != userID {
		t.Fatalf("successor = (%+v, %v), want authenticated session", successor, err)
	}
}

func TestSessionEmailChangeTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	prefix := transitionTestPrefix(t)
	sid := prefix + "-authenticated"
	userID, email := transitionTestUser(t, prefix)
	if _, err := store.Pool.Exec(ctx, `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at)
		VALUES ($1, 'authenticated', $2, 'payload', NOW() + INTERVAL '3 hours')
	`, sid, userID); err != nil {
		t.Fatal(err)
	}
	primary := store.Impl()
	secondary := db.NewBusiness(store.Pool).Impl()

	issued, err := primary.IssueEmailChangeChallenge(ctx, &dbgen.IssueEmailChangeChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("111111"), ChallengeTtl: 15 * time.Minute,
	})
	if err != nil || issued.Outcome != session.TransitionSucceeded || issued.Session == nil ||
		issued.Session.ChallengeEmail != email {
		t.Fatalf("issue = (%+v, %v), want database email", issued, err)
	}
	invalid, err := secondary.ConsumeEmailChangeChallenge(ctx, &dbgen.ConsumeEmailChangeChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("000000"), MaxAttempts: 2,
	})
	if err != nil || invalid.Outcome != session.TransitionInvalidCode || invalid.FailedAttempts != 1 {
		t.Fatalf("invalid consume = (%+v, %v), want invalid_code", invalid, err)
	}
	consumed, err := primary.ConsumeEmailChangeChallenge(ctx, &dbgen.ConsumeEmailChangeChallengeParams{
		SessionID: sid, ChallengeCode: db.Text("111111"), MaxAttempts: 2,
	})
	if err != nil || consumed.Outcome != session.TransitionSucceeded || consumed.Session == nil ||
		consumed.Session.ChallengeKind != "" || consumed.Session.ChallengeEmail != "" {
		t.Fatalf("successful consume = (%+v, %v), want cleared challenge", consumed, err)
	}
	assertSessionChallengeCodeCleared(t, store.Pool, sid)
}

func TestInactiveUsersCannotCompleteAuthenticationTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	prefix := transitionTestPrefix(t)
	impl := store.Impl()

	signInUserID, _ := transitionTestUser(t, prefix+"-sign-in")
	signInSID := prefix + "-sign-in"
	if _, err := impl.IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: signInSID, UserID: signInUserID, ChallengeCode: db.Text("111111"), Data: []byte("pending"),
		SessionTtl: 3 * time.Hour, ChallengeTtl: 15 * time.Minute, MaxAttempts: 5,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool.Exec(ctx, "UPDATE backend.users SET enabled = FALSE WHERE id = $1", signInUserID); err != nil {
		t.Fatal(err)
	}
	signInResult, err := impl.ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
		SessionID: signInSID, SuccessorSessionID: prefix + "-successor", ChallengeCode: db.Text("111111"),
		SuccessorData: []byte("authenticated"), SuccessorTtl: 3 * time.Hour, MaxAttempts: 5,
	})
	if err != nil || signInResult.Outcome != session.TransitionStale {
		t.Fatalf("disabled-user sign-in consume = (%+v, %v), want stale", signInResult, err)
	}

	emailUserID, _ := transitionTestUser(t, prefix+"-email")
	emailSID := prefix + "-email"
	if _, err := store.Pool.Exec(ctx, `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at)
		VALUES ($1, 'authenticated', $2, 'authenticated', NOW() + INTERVAL '3 hours')
	`, emailSID, emailUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := impl.IssueEmailChangeChallenge(ctx, &dbgen.IssueEmailChangeChallengeParams{
		SessionID: emailSID, ChallengeCode: db.Text("222222"), ChallengeTtl: 15 * time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool.Exec(ctx, "UPDATE backend.users SET deleted_at = NOW() WHERE id = $1", emailUserID); err != nil {
		t.Fatal(err)
	}
	emailResult, err := impl.ConsumeEmailChangeChallenge(ctx, &dbgen.ConsumeEmailChangeChallengeParams{
		SessionID: emailSID, ChallengeCode: db.Text("222222"), MaxAttempts: 5,
	})
	if err != nil || emailResult.Outcome != session.TransitionStale {
		t.Fatalf("deleted-user email-change consume = (%+v, %v), want stale", emailResult, err)
	}
}
