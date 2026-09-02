package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var errUnexpectedLiveSessionRead = errors.New("unexpected live session read")

type liveSessionResponse struct {
	row     *dbgen.GetLiveSessionRow
	err     error
	started chan struct{}
	release <-chan struct{}
}

type noopPayloadStore struct{}

func (noopPayloadStore) UpdatePayload(context.Context, string) {}

type scriptedLiveSessionQuerier struct {
	*QuerierStub
	mu        sync.Mutex
	responses []liveSessionResponse
}

type missingSignInSuccessorQuerier struct {
	*QuerierStub
}

type successfulRegistrationQuerier struct {
	*QuerierStub
	row *dbgen.ConsumeRegistrationChallengeRow
}

type userRevocationQuerier struct {
	*QuerierStub
	sid string
}

type staticSessionRevocationQuerier struct {
	*QuerierStub
	row *dbgen.RevokeSessionRow
	err error
}

type revocableSessionQuerier struct {
	*QuerierStub
	mu      sync.Mutex
	row     *dbgen.GetLiveSessionRow
	revoked bool
	reads   int
}

func (q *missingSignInSuccessorQuerier) ConsumeSignInChallenge(context.Context, *dbgen.ConsumeSignInChallengeParams) (*dbgen.ConsumeSignInChallengeRow, error) {
	return &dbgen.ConsumeSignInChallengeRow{
		Outcome: "succeeded", SessionID: "predecessor", State: dbgen.SessionStateRevoked, Version: 2,
	}, nil
}

func (q *successfulRegistrationQuerier) ConsumeRegistrationChallenge(context.Context, *dbgen.ConsumeRegistrationChallengeParams) (*dbgen.ConsumeRegistrationChallengeRow, error) {
	return q.row, nil
}

func (q *userRevocationQuerier) RevokeUserSessions(context.Context, pgtype.Int4) ([]*dbgen.RevokeUserSessionsRow, error) {
	return []*dbgen.RevokeUserSessionsRow{{SessionID: q.sid, State: dbgen.SessionStateRevoked, Version: 2}}, nil
}

func (q *staticSessionRevocationQuerier) RevokeSession(context.Context, string) (*dbgen.RevokeSessionRow, error) {
	return q.row, q.err
}

func (q *revocableSessionQuerier) GetLiveSession(context.Context, string) (*dbgen.GetLiveSessionRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.reads++
	if q.revoked {
		return nil, pgx.ErrNoRows
	}
	row := *q.row
	row.Data = append([]byte(nil), q.row.Data...)
	return &row, nil
}

func (q *revocableSessionQuerier) RevokeSession(_ context.Context, sid string) (*dbgen.RevokeSessionRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.revoked = true
	return &dbgen.RevokeSessionRow{
		SessionID: sid,
		State:     dbgen.SessionStateRevoked,
		Version:   q.row.Version + 1,
		UserID:    q.row.UserID,
	}, nil
}

func newScriptedLiveSessionQuerier() *scriptedLiveSessionQuerier {
	return &scriptedLiveSessionQuerier{QuerierStub: &QuerierStub{}}
}

func (q *scriptedLiveSessionQuerier) enqueue(response liveSessionResponse) {
	q.mu.Lock()
	q.responses = append(q.responses, response)
	q.mu.Unlock()
}

func (q *scriptedLiveSessionQuerier) GetLiveSession(ctx context.Context, _ string) (*dbgen.GetLiveSessionRow, error) {
	q.mu.Lock()
	if len(q.responses) == 0 {
		q.mu.Unlock()
		return nil, errUnexpectedLiveSessionRead
	}
	response := q.responses[0]
	q.responses = q.responses[1:]
	q.mu.Unlock()

	if response.started != nil {
		close(response.started)
	}
	if response.release != nil {
		select {
		case <-response.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if response.row == nil {
		return nil, response.err
	}
	row := *response.row
	row.Data = append([]byte(nil), response.row.Data...)
	return &row, response.err
}

func (q *scriptedLiveSessionQuerier) RevokeSession(_ context.Context, sid string) (*dbgen.RevokeSessionRow, error) {
	return &dbgen.RevokeSessionRow{SessionID: sid, State: dbgen.SessionStateRevoked, Version: 2, UserID: Int(42)}, nil
}

func testLiveSessionRow(t *testing.T, sid string, version int32, name string, expiresAt time.Time) *dbgen.GetLiveSessionRow {
	t.Helper()
	payload := session.NewPayload(sid, noopPayloadStore{})
	if err := payload.Set(t.Context(), session.KeyUserName, name); err != nil {
		t.Fatal(err)
	}
	data, err := payload.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return &dbgen.GetLiveSessionRow{
		SessionID: sid,
		State:     dbgen.SessionStateAuthenticated,
		Version:   version,
		UserID:    Int(42),
		Data:      data,
		ExpiresAt: Timestampz(expiresAt),
	}
}

func assertCachedRevocation(t *testing.T, store *SessionStore, sid string, version int32) {
	t.Helper()
	cached, ok := store.sessionCache.GetIfPresent(sid)
	if !ok {
		t.Fatal("revoked session is absent from cache")
	}
	authority, ok := cached.Authority()
	if !ok || authority.State != session.StateRevoked || authority.Version != version {
		t.Fatalf("cached revocation Authority = %+v, want revoked version %d", authority, version)
	}
}

func TestSessionStoreRevocationTombstoneRejectsDelayedResolve(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	started := make(chan struct{})
	release := make(chan struct{})
	q := newScriptedLiveSessionQuerier()
	q.enqueue(liveSessionResponse{
		row:     testLiveSessionRow(t, sid, 3, "database", now.Add(time.Hour)),
		started: started,
		release: release,
	})
	_, store := testResolveStore(q)
	errResult := make(chan error, 1)
	go func() {
		_, err := store.resolve(t.Context(), sid, now)
		errResult <- err
	}()

	<-started
	if _, err := store.RevokeSession(t.Context(), sid); err != nil {
		t.Fatal(err)
	}
	close(release)

	if err := <-errResult; !errors.Is(err, ErrStaleSessionRead) {
		t.Fatalf("delayed resolve error = %v, want %v", err, ErrStaleSessionRead)
	}
	assertCachedRevocation(t, store, sid, 2)
	if _, err := store.resolve(t.Context(), sid, now); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("cached tombstone resolve error = %v, want %v", err, session.ErrSessionMissing)
	}
}

func TestSessionStoreRevocationTombstoneRejectsDelayedTransition(t *testing.T) {
	sid := t.Name()
	q := newScriptedLiveSessionQuerier()
	_, store := testResolveStore(q)
	if _, err := store.RevokeSession(t.Context(), sid); err != nil {
		t.Fatal(err)
	}
	row := testLiveSessionRow(t, sid, 3, "delayed transition", time.Now().Add(time.Hour))

	_, _, err := store.publishTransitionSession(&session.StoredSession{
		SessionID: row.SessionID,
		State:     session.StateAuthenticated,
		Version:   row.Version,
		UserID:    row.UserID.Int32,
		Payload:   row.Data,
		ExpiresAt: row.ExpiresAt.Time,
	})
	if !errors.Is(err, ErrStaleSessionRead) {
		t.Fatalf("delayed transition error = %v, want %v", err, ErrStaleSessionRead)
	}
	assertCachedRevocation(t, store, sid, 2)
}

func TestSessionWorkersPreserveRevocationTombstones(t *testing.T) {
	tests := map[string]func(*SessionStore, string, *session.Session){
		"publish expiration": func(store *SessionStore, sid string, _ *session.Session) {
			store.publishExpirationRenewal(session.ExpirationRenewalResult{SessionID: sid, Version: 2, ExpiresAt: time.Now().Add(time.Hour)})
		},
		"evict expiration": func(store *SessionStore, sid string, _ *session.Session) {
			store.evictExpirationRenewal(sid)
		},
		"publish payload": func(store *SessionStore, sid string, tombstone *session.Session) {
			store.publishPayloadVersion(sid, 2, 3, tombstone.Payload())
		},
		"evict payload": func(store *SessionStore, sid string, _ *session.Session) {
			store.evictPayloadVersion(sid, 2)
		},
	}
	for name, callback := range tests {
		t.Run(name, func(t *testing.T) {
			sid := t.Name()
			q := newScriptedLiveSessionQuerier()
			_, store := testResolveStore(q)
			if _, err := store.RevokeSession(t.Context(), sid); err != nil {
				t.Fatal(err)
			}
			tombstone, ok := store.sessionCache.GetIfPresent(sid)
			if !ok {
				t.Fatal("revocation did not leave a tombstone for worker publication")
			}

			callback(store, sid, tombstone)

			cached, ok := store.sessionCache.GetIfPresent(sid)
			if !ok || cached != tombstone {
				t.Fatal("worker callback replaced or evicted the revocation tombstone")
			}
			assertCachedRevocation(t, store, sid, 2)
		})
	}
}

func TestSessionStoreUserRevocationLeavesTombstone(t *testing.T) {
	sid := t.Name()
	querier := &userRevocationQuerier{QuerierStub: &QuerierStub{}, sid: sid}
	business := NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
	store := NewSessionStore(business, &sessionMetricsStub{})

	if err := store.RevokeUserSessions(t.Context(), 42); err != nil {
		t.Fatal(err)
	}
	assertCachedRevocation(t, store, sid, 2)
}

func TestSessionStoreNoRowRevocationPreservesTombstone(t *testing.T) {
	sid := t.Name()
	querier := &staticSessionRevocationQuerier{QuerierStub: &QuerierStub{}, err: pgx.ErrNoRows}
	business := NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
	store := NewSessionStore(business, &sessionMetricsStub{})
	tombstone := session.NewSessionWithAuthority(
		session.Authority{State: session.StateRevoked, Version: 2}, session.NewPayload(sid, store))
	store.sessionCache.Set(sid, tombstone)

	result, err := store.RevokeSession(t.Context(), sid)
	if err != nil || result != nil {
		t.Fatalf("no-row revocation = (%+v, %v), want nil result", result, err)
	}
	cached, ok := store.sessionCache.GetIfPresent(sid)
	if !ok || cached != tombstone {
		t.Fatal("no-row revocation removed the existing tombstone")
	}
}

func TestSessionStoreRejectsMalformedRevocationResults(t *testing.T) {
	tests := map[string]*dbgen.RevokeSessionRow{
		"wrong state": {SessionID: "sid", State: dbgen.SessionStateAuthenticated, Version: 3},
		"wrong SID":   {SessionID: "other", State: dbgen.SessionStateRevoked, Version: 3},
	}
	for name, row := range tests {
		t.Run(name, func(t *testing.T) {
			querier := &staticSessionRevocationQuerier{QuerierStub: &QuerierStub{}, row: row}
			business := NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
			store := NewSessionStore(business, &sessionMetricsStub{})
			tombstone := session.NewSessionWithAuthority(
				session.Authority{State: session.StateRevoked, Version: 2}, session.NewPayload("sid", store))
			store.sessionCache.Set("sid", tombstone)

			if _, err := store.RevokeSession(t.Context(), "sid"); !errors.Is(err, session.ErrInvalidTransitionResult) {
				t.Fatalf("malformed revocation error = %v, want %v", err, session.ErrInvalidTransitionResult)
			}
			cached, ok := store.sessionCache.GetIfPresent("sid")
			if !ok || cached != tombstone {
				t.Fatal("malformed revocation replaced the existing tombstone")
			}
		})
	}
}

func TestTransitionAuthorityRemainsExactWhenCacheIsNewer(t *testing.T) {
	const sid = "transition-authority"
	payload := session.NewPayload(sid, noopPayloadStore{})
	data, err := payload.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	store := &SessionStore{sessionCache: newSessionCache(nil)}
	newer := &session.StoredSession{
		SessionID:      sid,
		State:          session.StatePending,
		Version:        3,
		Payload:        data,
		ExpiresAt:      time.Now().Add(time.Hour),
		ChallengeKind:  session.ChallengeKindRegistration,
		ChallengeEmail: "newer@example.com",
	}
	if _, _, err := store.publishTransitionSession(newer); err != nil {
		t.Fatal(err)
	}
	older := &session.StoredSession{
		SessionID:      sid,
		State:          session.StatePending,
		Version:        2,
		UserID:         42,
		Payload:        data,
		ExpiresAt:      time.Now().Add(time.Hour),
		ChallengeKind:  session.ChallengeKindSignIn,
		ChallengeEmail: "exact@example.com",
	}
	published, exact, err := store.publishTransitionSession(older)
	if err != nil {
		t.Fatal(err)
	}
	publishedAuthority, _ := published.Authority()
	if publishedAuthority.Version != newer.Version || publishedAuthority.ChallengeEmail != newer.ChallengeEmail {
		t.Fatalf("published Authority = %+v, want newer cached transition", publishedAuthority)
	}
	if exact.Version != older.Version || exact.ChallengeKind != older.ChallengeKind || exact.ChallengeEmail != older.ChallengeEmail {
		t.Fatalf("exact Authority = %+v, want SQL transition %+v", exact, older)
	}
}

func testResolveStore(q *scriptedLiveSessionQuerier) (*BusinessStore, *SessionStore) {
	business := NewBusinessWithQuerier(nil, q, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
	return business, NewSessionStore(business, &sessionMetricsStub{})
}

func TestSessionStoreResolveUsesFixedValidationLease(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	q := newScriptedLiveSessionQuerier()
	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 1, "database", now.Add(time.Hour))})
	_, store := testResolveStore(q)

	sess, err := store.resolve(t.Context(), sid, now)
	if err != nil {
		t.Fatal(err)
	}
	authority, _ := sess.Authority()
	if want := now.Add(sessionValidationLease); !authority.LeaseUntil.Equal(want) {
		t.Fatalf("LeaseUntil = %v, want %v", authority.LeaseUntil, want)
	}
	if _, err := store.resolve(t.Context(), sid, now.Add(sessionValidationLease-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	databaseErr := errors.New("database unavailable")
	q.enqueue(liveSessionResponse{err: databaseErr})
	if _, err := store.resolve(t.Context(), sid, now.Add(sessionValidationLease)); !errors.Is(err, databaseErr) {
		t.Fatalf("expired lease error = %v, want %v", err, databaseErr)
	}
	if cached, ok := store.sessionCache.GetIfPresent(sid); !ok || cached != sess {
		t.Fatal("database error cleared the cached session")
	}
}

func TestRemoteRevocationIsAcceptedOnlyWithinValidationLease(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	q := &revocableSessionQuerier{
		QuerierStub: &QuerierStub{},
		row:         testLiveSessionRow(t, sid, 1, "database", now.Add(time.Hour)),
	}
	newStore := func() *SessionStore {
		business := NewBusinessWithQuerier(nil, q, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
		return NewSessionStore(business, &sessionMetricsStub{})
	}
	revokingStore := newStore()
	remoteStore := newStore()

	cached, err := remoteStore.resolve(t.Context(), sid, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revokingStore.RevokeSession(t.Context(), sid); err != nil {
		t.Fatal(err)
	}
	beforeBoundary, err := remoteStore.resolve(t.Context(), sid, now.Add(sessionValidationLease-time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	authority, ok := beforeBoundary.Authority()
	if beforeBoundary != cached || !ok || authority.State != session.StateAuthenticated {
		t.Fatalf("pre-boundary remote session = (%p, %+v), want cached authenticated session", beforeBoundary, authority)
	}
	if _, err := remoteStore.resolve(t.Context(), sid, now.Add(sessionValidationLease)); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("lease-boundary resolve error = %v, want %v", err, session.ErrSessionMissing)
	}
	q.mu.Lock()
	reads := q.reads
	q.mu.Unlock()
	if reads != 2 {
		t.Fatalf("live-session reads = %d, want initial read plus one lease-boundary read", reads)
	}
	if _, ok := remoteStore.sessionCache.GetIfPresent(sid); ok {
		t.Fatal("lease-boundary revocation read left stale Authority cached")
	}
}

func TestSessionStoreResolveCapsLeaseAtExpiration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	expiresAt := now.Add(time.Minute)
	q := newScriptedLiveSessionQuerier()
	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 1, "database", expiresAt)})
	_, store := testResolveStore(q)

	sess, err := store.resolve(t.Context(), sid, now)
	if err != nil {
		t.Fatal(err)
	}
	authority, _ := sess.Authority()
	if !authority.LeaseUntil.Equal(expiresAt) {
		t.Fatalf("LeaseUntil = %v, want expiration %v", authority.LeaseUntil, expiresAt)
	}
}

func TestSessionStoreResolveUsesUnexpiredPendingCache(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	row := testLiveSessionRow(t, sid, 1, "pending", now.Add(time.Hour))
	row.State = dbgen.SessionStatePending
	row.ChallengeKind = dbgen.NullSessionChallengeKind{SessionChallengeKind: dbgen.SessionChallengeKindSignIn, Valid: true}
	row.ChallengeEmail = Text("pending@privatecaptcha.com")
	q := newScriptedLiveSessionQuerier()
	q.enqueue(liveSessionResponse{row: row})
	_, store := testResolveStore(q)

	pending, err := store.resolve(t.Context(), sid, now)
	if err != nil {
		t.Fatal(err)
	}
	warm, err := store.resolve(t.Context(), sid, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if warm.ID() != pending.ID() {
		t.Fatalf("warm pending SID = %q, want %q", warm.ID(), pending.ID())
	}
	authority, ok := warm.Authority()
	if !ok || authority.State != session.StatePending || authority.ChallengeEmail != "pending@privatecaptcha.com" {
		t.Fatalf("warm pending Authority = %+v", authority)
	}
}

func TestSessionStoreRejectsSuccessfulSignInWithoutSuccessor(t *testing.T) {
	querier := &missingSignInSuccessorQuerier{QuerierStub: &QuerierStub{}}
	business := NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
	store := NewSessionStore(business, &sessionMetricsStub{})

	_, err := store.ConsumeSignInChallenge(t.Context(), session.SignInChallengeConsume{
		SessionID: "predecessor", SuccessorSessionID: "successor", ChallengeCode: "123456",
		SuccessorPayload: []byte("payload"), SuccessorTTL: time.Hour, MaxAttempts: 5,
	})
	if !errors.Is(err, session.ErrInvalidTransitionResult) {
		t.Fatalf("malformed sign-in error = %v, want %v", err, session.ErrInvalidTransitionResult)
	}
	assertCachedRevocation(t, store, "predecessor", 2)
}

func TestSessionStoreRegistrationConsumptionLeavesPredecessorTombstone(t *testing.T) {
	const sid = "registration-predecessor"
	payload := session.NewPayload(sid, noopPayloadStore{})
	if err := payload.Set(t.Context(), session.KeyUserName, "Registrant"); err != nil {
		t.Fatal(err)
	}
	data, err := payload.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	querier := &successfulRegistrationQuerier{
		QuerierStub: &QuerierStub{},
		row: &dbgen.ConsumeRegistrationChallengeRow{
			Outcome: "succeeded", SessionID: sid, State: dbgen.SessionStateRevoked, Version: 2,
			Data: data, ExpiresAt: Timestampz(time.Now().Add(time.Hour)), ChallengeEmail: Text("registrant@example.com"),
		},
	}
	business := NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
	store := NewSessionStore(business, &sessionMetricsStub{})

	result, err := store.ConsumeRegistrationChallenge(t.Context(), session.RegistrationChallengeConsume{
		SessionID: sid, ChallengeCode: "123456", MaxAttempts: 5,
	})
	if err != nil || result.Outcome != session.TransitionSucceeded || result.Email != "registrant@example.com" {
		t.Fatalf("registration consume = (%+v, %v), want succeeded", result, err)
	}
	assertCachedRevocation(t, store, sid, 2)
}

func TestSessionStoreRegistrationDecodeErrorLeavesPredecessorTombstone(t *testing.T) {
	const sid = "invalid-registration-predecessor"
	querier := &successfulRegistrationQuerier{
		QuerierStub: &QuerierStub{},
		row: &dbgen.ConsumeRegistrationChallengeRow{
			Outcome: "succeeded", SessionID: sid, State: dbgen.SessionStateRevoked, Version: 2,
			Data: []byte("invalid Payload"), ExpiresAt: Timestampz(time.Now().Add(time.Hour)),
		},
	}
	business := NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
	store := NewSessionStore(business, &sessionMetricsStub{})

	_, err := store.ConsumeRegistrationChallenge(t.Context(), session.RegistrationChallengeConsume{
		SessionID: sid, ChallengeCode: "123456", MaxAttempts: 5,
	})
	if err == nil {
		t.Fatal("registration consumption with invalid Payload succeeded")
	}
	assertCachedRevocation(t, store, sid, 2)
}

func TestSessionStoreResolvePublishesOnlyNonStaleVersions(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	q := newScriptedLiveSessionQuerier()
	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 1, "version one", now.Add(time.Hour))})
	_, store := testResolveStore(q)

	first, err := store.resolve(t.Context(), sid, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Set(t.Context(), session.KeyUserName, "local"); err != nil {
		t.Fatal(err)
	}

	secondRead := now.Add(sessionValidationLease)
	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 2, "version two", now.Add(2*time.Hour))})
	second, err := store.resolve(t.Context(), sid, secondRead)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("higher version did not replace the cached Session")
	}
	if actual := second.Get(t.Context(), session.KeyUserName); actual != "version two" {
		t.Fatalf("higher version Payload = %v, want version two", actual)
	}
	authority, _ := second.Authority()
	if authority.Version != 2 {
		t.Fatalf("published version = %d, want 2", authority.Version)
	}

	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 1, "stale", now.Add(3*time.Hour))})
	if _, err := store.resolve(t.Context(), sid, secondRead.Add(sessionValidationLease)); err == nil {
		t.Fatal("lower-version read authenticated an expired lease")
	}
	cached, ok := store.sessionCache.GetIfPresent(sid)
	if !ok {
		t.Fatal("lower-version read evicted the newer cached Session")
	}
	authority, _ = cached.Authority()
	if authority.Version != 2 {
		t.Fatalf("lower-version read regressed cache to version %d", authority.Version)
	}
}

func TestSessionStoreResolvePreservesConcurrentPayloadOnSameVersion(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	q := newScriptedLiveSessionQuerier()
	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 1, "database", now.Add(time.Hour))})
	_, store := testResolveStore(q)
	sess, err := store.resolve(t.Context(), sid, now)
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	readTime := now.Add(sessionValidationLease)
	q.enqueue(liveSessionResponse{
		row:     testLiveSessionRow(t, sid, 1, "stale database Payload", now.Add(2*time.Hour)),
		started: started,
		release: release,
	})
	result := make(chan *session.Session, 1)
	errResult := make(chan error, 1)
	go func() {
		resolved, resolveErr := store.resolve(t.Context(), sid, readTime)
		result <- resolved
		errResult <- resolveErr
	}()
	<-started
	if err := sess.Set(t.Context(), session.KeyUserName, "concurrent local Payload"); err != nil {
		t.Fatal(err)
	}
	close(release)

	resolved := <-result
	if err := <-errResult; err != nil {
		t.Fatal(err)
	}
	if resolved.Payload() != sess.Payload() {
		t.Fatal("same version replaced the local Payload")
	}
	if actual := resolved.Get(t.Context(), session.KeyUserName); actual != "concurrent local Payload" {
		t.Fatalf("same-version read replaced local Payload with %v", actual)
	}
	authority, _ := resolved.Authority()
	if want := readTime.Add(sessionValidationLease); !authority.LeaseUntil.Equal(want) {
		t.Fatalf("replacement LeaseUntil = %v, want %v", authority.LeaseUntil, want)
	}
}

func TestSessionStoreResolveConditionallyEvictsObservedAuthority(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	q := newScriptedLiveSessionQuerier()
	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 1, "version one", now.Add(time.Hour))})
	_, store := testResolveStore(q)
	if _, err := store.resolve(t.Context(), sid, now); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	q.enqueue(liveSessionResponse{err: pgx.ErrNoRows, started: started, release: release})
	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 1, "same version", now.Add(2*time.Hour))})
	readTime := now.Add(sessionValidationLease)
	firstErr := make(chan error, 1)
	go func() {
		_, err := store.resolve(t.Context(), sid, readTime)
		firstErr <- err
	}()
	<-started

	replacement, err := store.resolve(t.Context(), sid, readTime)
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-firstErr; !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("missing read error = %v, want %v", err, session.ErrSessionMissing)
	}

	cached, ok := store.sessionCache.GetIfPresent(sid)
	if !ok || cached != replacement {
		t.Fatal("delayed missing read evicted the replacement Authority")
	}
	authority, _ := cached.Authority()
	if authority.Version != 1 {
		t.Fatalf("cached version = %d, want 1", authority.Version)
	}
	if want := readTime.Add(sessionValidationLease); !authority.LeaseUntil.Equal(want) {
		t.Fatalf("replacement LeaseUntil = %v, want %v", authority.LeaseUntil, want)
	}
}

func TestSessionCacheUsesSlidingResidency(t *testing.T) {
	clock := &manualCacheClock{}
	cache := newSessionCache(clock)
	sess := session.NewSessionWithAuthority(session.Authority{Version: 1}, session.NewPayload("sid", noopPayloadStore{}))
	cache.Set(sess.ID(), sess)

	clock.Advance(sessionCacheResidencyTTL - time.Minute)
	if _, ok := cache.GetIfPresent(sess.ID()); !ok {
		t.Fatal("session expired before the residency deadline")
	}
	clock.Advance(sessionCacheResidencyTTL - time.Minute)
	if _, ok := cache.GetIfPresent(sess.ID()); !ok {
		t.Fatal("cache read did not refresh sliding residency")
	}
	clock.Advance(sessionCacheResidencyTTL + time.Nanosecond)
	if _, ok := cache.GetIfPresent(sess.ID()); ok {
		t.Fatal("session survived past the refreshed residency deadline")
	}
}

func TestBusinessCacheOperationsDoNotAffectSessionCache(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sid := t.Name()
	q := newScriptedLiveSessionQuerier()
	q.enqueue(liveSessionResponse{row: testLiveSessionRow(t, sid, 1, "cached", now.Add(time.Hour))})
	businessCache, err := NewMemoryCache[CacheKey, any]("test", 100, &CacheMissingValue{}, DefaultCacheTTL, time.Hour, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	business := NewBusinessWithQuerier(nil, q, businessCache)
	store := NewSessionStore(business, &sessionMetricsStub{})
	original, err := store.resolve(t.Context(), sid, now)
	if err != nil {
		t.Fatal(err)
	}
	originalAuthority, _ := original.Authority()

	dir := t.TempDir()
	if err := business.SaveCache(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	business.ClearCache()
	if err := business.LoadCache(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.resolve(t.Context(), sid, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	resolvedAuthority, _ := resolved.Authority()
	if resolved != original || resolvedAuthority != originalAuthority || resolved.Get(t.Context(), session.KeyUserName) != "cached" {
		t.Fatalf("business cache operations changed session: Authority=%+v Payload=%v", resolvedAuthority, resolved.Get(t.Context(), session.KeyUserName))
	}
}
