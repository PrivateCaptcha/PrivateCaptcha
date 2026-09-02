package portal

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type concurrentConsumeQuerier struct {
	dbgen.Querier
	enabled atomic.Bool
	ready   chan<- struct{}
	release <-chan struct{}
}

func (q *concurrentConsumeQuerier) ConsumeSignInChallenge(ctx context.Context, arg *dbgen.ConsumeSignInChallengeParams) (*dbgen.ConsumeSignInChallengeRow, error) {
	if q.enabled.Load() {
		q.ready <- struct{}{}
		<-q.release
	}
	return q.Querier.ConsumeSignInChallenge(ctx, arg)
}

type revokeFailureSessionStore struct {
	session.Store
	err error
}

func (s *revokeFailureSessionStore) RevokeUserSessions(context.Context, int32) error {
	return s.err
}

func TestIndependentSessionStoresShareSignInState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	prefix := transitionTestPrefix(t)
	userID, _ := transitionTestUser(t, prefix)
	ready := make(chan struct{})
	release := make(chan struct{})
	newSessionStore := func() (*db.SessionStore, *concurrentConsumeQuerier) {
		querier := &concurrentConsumeQuerier{Querier: dbgen.New(store.Pool), ready: ready, release: release}
		business := db.NewBusinessWithQuerier(
			store.Pool, querier, db.NewStaticCache[db.CacheKey, any](100, &db.CacheMissingValue{}),
		)
		return db.NewSessionStore(business, server.Metrics), querier
	}
	primary, primaryQuerier := newSessionStore()
	secondary, secondaryQuerier := newSessionStore()
	pending := primary.StartAnonymousSession(prefix + "-pending")
	payload, err := pending.Payload().Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	issued, err := primary.IssueSignInChallenge(ctx, session.SignInChallengeIssue{
		SessionID: pending.ID(), UserID: userID, ChallengeCode: "111111", Payload: payload,
		SessionTTL: 3 * time.Hour, ChallengeTTL: 15 * time.Minute, MaxAttempts: 5,
	})
	if err != nil || issued.Outcome != session.TransitionSucceeded {
		t.Fatalf("issue = (%+v, %v), want succeeded", issued, err)
	}
	failed, err := secondary.ConsumeSignInChallenge(ctx, session.SignInChallengeConsume{
		SessionID: pending.ID(), SuccessorSessionID: prefix + "-unused", ChallengeCode: "000000",
		SuccessorPayload: payload, SuccessorTTL: 3 * time.Hour, MaxAttempts: 5,
	})
	if err != nil || failed.Outcome != session.TransitionInvalidCode || failed.FailedAttempts != 1 {
		t.Fatalf("shared failed attempt = (%+v, %v), want first invalid attempt", failed, err)
	}
	secondFailure, err := primary.ConsumeSignInChallenge(ctx, session.SignInChallengeConsume{
		SessionID: pending.ID(), SuccessorSessionID: prefix + "-unused", ChallengeCode: "000000",
		SuccessorPayload: payload, SuccessorTTL: 3 * time.Hour, MaxAttempts: 5,
	})
	if err != nil || secondFailure.Outcome != session.TransitionInvalidCode || secondFailure.FailedAttempts != 2 {
		t.Fatalf("second shared failed attempt = (%+v, %v), want second invalid attempt", secondFailure, err)
	}
	resent, err := primary.ResendPendingChallenge(ctx, session.PendingChallengeResend{
		SessionID: pending.ID(), ChallengeCode: "222222", ChallengeTTL: 15 * time.Minute, MaxAttempts: 5,
	})
	if err != nil || resent.Outcome != session.TransitionSucceeded {
		t.Fatalf("cross-store resend = (%+v, %v), want succeeded", resent, err)
	}
	oldCode, err := secondary.ConsumeSignInChallenge(ctx, session.SignInChallengeConsume{
		SessionID: pending.ID(), SuccessorSessionID: prefix + "-old-code", ChallengeCode: "111111",
		SuccessorPayload: payload, SuccessorTTL: 3 * time.Hour, MaxAttempts: 5,
	})
	if err != nil || oldCode.Outcome != session.TransitionInvalidCode || oldCode.FailedAttempts != 3 {
		t.Fatalf("old code after resend = (%+v, %v), want third cumulative invalid attempt", oldCode, err)
	}

	type consumeCall struct {
		successorID string
		result      *session.ChallengeResult
		err         error
	}
	calls := make(chan consumeCall, 2)
	stores := []*db.SessionStore{primary, secondary}
	primaryQuerier.enabled.Store(true)
	secondaryQuerier.enabled.Store(true)
	for i, sessionStore := range stores {
		successorID := fmt.Sprintf("%s-successor-%d", prefix, i)
		go func() {
			result, consumeErr := sessionStore.ConsumeSignInChallenge(ctx, session.SignInChallengeConsume{
				SessionID: pending.ID(), SuccessorSessionID: successorID, ChallengeCode: "222222",
				SuccessorPayload: payload, SuccessorTTL: 3 * time.Hour, MaxAttempts: 5,
			})
			calls <- consumeCall{successorID: successorID, result: result, err: consumeErr}
		}()
	}
	<-ready
	<-ready
	close(release)

	var succeeded *consumeCall
	for range stores {
		call := <-calls
		if call.err != nil {
			t.Fatal(call.err)
		}
		if call.result.Outcome == session.TransitionSucceeded {
			if succeeded != nil {
				t.Fatal("both stores consumed the same sign-in challenge")
			}
			succeeded = &call
		} else if call.result.Outcome != session.TransitionStale {
			t.Fatalf("unexpected concurrent consume result: %+v", call.result)
		}
	}
	if succeeded == nil || succeeded.result.Session == nil {
		t.Fatal("neither store created an authenticated successor")
	}
	authority, ok := succeeded.result.Session.Authority()
	if !ok || authority.State != session.StateAuthenticated || authority.UserID != userID {
		t.Fatalf("successful successor Authority = %+v", authority)
	}

	fresh := db.NewSessionStore(db.NewBusiness(store.Pool), server.Metrics)
	for i := range stores {
		sid := fmt.Sprintf("%s-successor-%d", prefix, i)
		resolved, resolveErr := fresh.Resolve(ctx, sid)
		if sid == succeeded.successorID {
			if resolveErr != nil || resolved.ID() != sid {
				t.Fatalf("successful successor resolve = (%v, %v)", resolved, resolveErr)
			}
		} else if !errors.Is(resolveErr, session.ErrSessionMissing) {
			t.Fatalf("losing successor %q resolve error = %v, want missing", sid, resolveErr)
		}
	}
}
