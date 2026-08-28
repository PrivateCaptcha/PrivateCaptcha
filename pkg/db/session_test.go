package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type sessionMetricsStub struct{}

func (s *sessionMetricsStub) ObserveEventDropped(eventType common.MetricEventType) {}
func (s *sessionMetricsStub) ObservePanic()                                        {}

type memorySessionQuerier struct {
	*QuerierStub
	mu                             sync.RWMutex
	sessions                       map[string]*dbgen.Session
	consumeCommitError             error
	consumeRegistrationCommitError error
	finalizeCommitError            error
	getSessionError                error
	getStarted                     chan struct{}
	getContinue                    chan struct{}
	createStarted                  chan struct{}
	createContinue                 chan struct{}
	updateStarted                  chan struct{}
	updateContinue                 chan struct{}
	dbCalls                        int
	now                            time.Time
}

func newMemorySessionQuerier() *memorySessionQuerier {
	return &memorySessionQuerier{
		QuerierStub: &QuerierStub{},
		sessions:    make(map[string]*dbgen.Session),
	}
}

func (q *memorySessionQuerier) CreateSession(ctx context.Context, arg *dbgen.CreateSessionParams) (*dbgen.Session, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dbCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.createStarted != nil {
		close(q.createStarted)
		q.createStarted = nil
		<-q.createContinue
	}
	if _, ok := q.sessions[arg.SessionID]; ok {
		return nil, errors.New("session already exists")
	}
	row := &dbgen.Session{
		SessionID:          arg.SessionID,
		State:              arg.State,
		Version:            1,
		UserID:             arg.UserID,
		Data:               append([]byte(nil), arg.Data...),
		ExpiresAt:          arg.ExpiresAt,
		ChallengeKind:      arg.ChallengeKind,
		ChallengeCode:      arg.ChallengeCode,
		ChallengeEmail:     arg.ChallengeEmail,
		ChallengeExpiresAt: arg.ChallengeExpiresAt,
	}
	q.sessions[arg.SessionID] = row
	return cloneSessionRow(row), nil
}

func (q *memorySessionQuerier) ConsumeSignInChallenge(ctx context.Context, arg *dbgen.ConsumeSignInChallengeParams) (*dbgen.ConsumeSignInChallengeRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dbCalls++
	old, ok := q.sessions[arg.OldSessionID]
	if !ok || old.State != dbgen.SessionStatePending || !old.UserID.Valid || old.UserID.Int32 != arg.ExpectedUserID || !old.ChallengeCode.Valid || old.ChallengeCode.String != arg.EncodedCode {
		return nil, pgx.ErrNoRows
	}
	old.State = dbgen.SessionStateRevoked
	old.Version++
	successor := &dbgen.Session{
		SessionID: arg.NewSessionID,
		State:     dbgen.SessionStateAuthenticated,
		Version:   1,
		UserID:    old.UserID,
		Data:      append([]byte(nil), arg.SuccessorData...),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(arg.SuccessorTtl), Valid: true},
	}
	q.sessions[arg.NewSessionID] = successor
	if q.consumeCommitError != nil {
		err := q.consumeCommitError
		q.consumeCommitError = nil
		return nil, err
	}
	return &dbgen.ConsumeSignInChallengeRow{
		Consumed:  true,
		SessionID: pgtype.Text{String: successor.SessionID, Valid: true},
		State: dbgen.NullSessionState{
			SessionState: successor.State,
			Valid:        true,
		},
		Version:   Int(successor.Version),
		UserID:    successor.UserID,
		ExpiresAt: successor.ExpiresAt,
	}, nil
}

func (q *memorySessionQuerier) ConsumeRegistrationChallenge(ctx context.Context, arg *dbgen.ConsumeRegistrationChallengeParams) (*dbgen.ConsumeRegistrationChallengeRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dbCalls++
	old, ok := q.sessions[arg.OldSessionID]
	if !ok || old.State != dbgen.SessionStatePending || old.UserID.Valid || !old.ChallengeKind.Valid || old.ChallengeKind.SessionChallengeKind != dbgen.SessionChallengeKindRegistration || !old.ChallengeCode.Valid || !old.ChallengeEmail.Valid {
		return nil, pgx.ErrNoRows
	}
	verified := old.FailedAttempts < arg.MaxFailedAttempts && old.ChallengeCode.String == arg.EncodedCode
	result := &dbgen.ConsumeRegistrationChallengeRow{
		Verified:          pgtype.Bool{Bool: verified, Valid: true},
		AttemptsExhausted: old.FailedAttempts >= arg.MaxFailedAttempts,
		ChallengeEmail:    old.ChallengeEmail,
	}
	if !verified {
		if old.FailedAttempts < arg.MaxFailedAttempts {
			old.FailedAttempts++
		}
		return result, nil
	}
	if !arg.AllowConsumption {
		return result, nil
	}
	old.State = dbgen.SessionStateRevoked
	old.Version++
	successor := &dbgen.Session{
		SessionID: arg.NewSessionID,
		State:     dbgen.SessionStateRegistrationProcessing,
		Version:   1,
		Data:      append([]byte(nil), arg.SuccessorData...),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(arg.SuccessorTtl), Valid: true},
	}
	q.sessions[arg.NewSessionID] = successor
	if q.consumeRegistrationCommitError != nil {
		err := q.consumeRegistrationCommitError
		q.consumeRegistrationCommitError = nil
		return nil, err
	}
	result.Consumed = true
	result.SessionID = pgtype.Text{String: successor.SessionID, Valid: true}
	result.State = dbgen.NullSessionState{SessionState: successor.State, Valid: true}
	result.Version = Int(successor.Version)
	result.ExpiresAt = successor.ExpiresAt
	return result, nil
}

func (q *memorySessionQuerier) FinalizeRegistrationSession(ctx context.Context, arg *dbgen.FinalizeRegistrationSessionParams) (*dbgen.Session, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dbCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	row, ok := q.sessions[arg.SessionID]
	if !ok || row.State != dbgen.SessionStateRegistrationProcessing || row.Version != arg.ExpectedVersion || row.UserID.Valid || !row.ExpiresAt.Valid || time.Now().After(row.ExpiresAt.Time) {
		return nil, pgx.ErrNoRows
	}
	row.State = dbgen.SessionStateAuthenticated
	row.UserID = pgtype.Int4{Int32: arg.UserID, Valid: true}
	row.Data = append([]byte(nil), arg.Data...)
	row.Version++
	if q.finalizeCommitError != nil {
		err := q.finalizeCommitError
		q.finalizeCommitError = nil
		return nil, err
	}
	return cloneSessionRow(row), nil
}
func (q *memorySessionQuerier) GetSessionByID(ctx context.Context, sid string) (*dbgen.Session, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dbCalls++
	if q.getStarted != nil {
		close(q.getStarted)
		q.getStarted = nil
		<-q.getContinue
	}
	if q.getSessionError != nil {
		err := q.getSessionError
		q.getSessionError = nil
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	row, ok := q.sessions[sid]
	now := q.now
	if now.IsZero() {
		now = time.Now()
	}
	if !ok || !row.ExpiresAt.Valid || now.After(row.ExpiresAt.Time) {
		return nil, pgx.ErrNoRows
	}
	return cloneSessionRow(row), nil
}

func (q *memorySessionQuerier) UpdateSessionDataCAS(ctx context.Context, arg *dbgen.UpdateSessionDataCASParams) ([]*dbgen.UpdateSessionDataCASRow, error) {
	if q.updateStarted != nil {
		close(q.updateStarted)
		<-q.updateContinue
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dbCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	updated := make([]*dbgen.UpdateSessionDataCASRow, 0, len(arg.SessionIds))
	for i, sid := range arg.SessionIds {
		row, ok := q.sessions[sid]
		if !ok || row.Version != arg.ExpectedVersions[i] || time.Now().After(row.ExpiresAt.Time) {
			continue
		}
		if row.State != dbgen.SessionStatePending && row.State != dbgen.SessionStateAuthenticated {
			continue
		}
		row.Data = append([]byte(nil), arg.Payloads[i]...)
		row.Version++
		updated = append(updated, &dbgen.UpdateSessionDataCASRow{SessionID: sid, Version: row.Version})
	}
	return updated, nil
}
func (q *memorySessionQuerier) RevokeSession(ctx context.Context, sid string) (*dbgen.RevokeSessionRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dbCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	row, ok := q.sessions[sid]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	previousState := row.State
	transitioned := row.State != dbgen.SessionStateRevoked
	if transitioned {
		row.State = dbgen.SessionStateRevoked
		row.Version++
	}
	return &dbgen.RevokeSessionRow{
		SessionID:     row.SessionID,
		State:         row.State,
		Version:       row.Version,
		UserID:        row.UserID,
		PreviousState: previousState,
		Transitioned:  transitioned,
	}, nil
}
func (q *memorySessionQuerier) failNextConsumeSignInChallengeAfterCommit(err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.consumeCommitError = err
}
func cloneSessionRow(row *dbgen.Session) *dbgen.Session {
	cloned := *row
	cloned.Data = append([]byte(nil), row.Data...)
	return &cloned
}

func setupLocallyAuthenticatedRegistration(t *testing.T) (*SessionStore, *memorySessionQuerier, *session.Session) {
	t.Helper()
	now := time.Now().UTC()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	store.now = func() time.Time { return now }
	sd := session.NewSessionData("registration-processing-sid")
	sd.SetAuthoritativeUserID(42)
	sd.SetAuthority(session.StateRegistrationProcessing, 1, now.Add(time.Hour), time.Time{})
	if !sd.MarkRegistrationAuthenticatedLocally(now) {
		t.Fatal("failed to authenticate registration fixture locally")
	}
	sess := session.NewSession(sd, store)
	if err := store.Init(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	payload, err := sd.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	querier.sessions[sess.ID()] = &dbgen.Session{
		SessionID: sess.ID(),
		State:     dbgen.SessionStateRegistrationProcessing,
		Version:   1,
		Data:      payload,
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
	}
	return store, querier, sess
}

func setupAuthenticatedSessionStore(t *testing.T) (*SessionStore, *memorySessionQuerier, *session.Session, *time.Time) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	querier := newMemorySessionQuerier()
	querier.now = now
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	store.now = func() time.Time { return now }

	sess := session.NewSession(session.NewSessionData("authenticated-sid"), store)
	if err := store.Init(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(ctx, session.KeyUserID, int32(1)); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan
	if err := sess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan
	if err := store.Create(ctx, sess); err != nil {
		t.Fatal(err)
	}

	querier.mu.Lock()
	row := querier.sessions[sess.ID()]
	row.State = dbgen.SessionStateAuthenticated
	row.ExpiresAt = pgtype.Timestamptz{Time: now.Add(sessionCacheTTL), Valid: true}
	querier.mu.Unlock()
	store.store.Impl().DeleteCachedUserSession(ctx, sess.ID())

	loaded, err := store.Read(ctx, sess.ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	querier.mu.Lock()
	querier.dbCalls = 0
	querier.mu.Unlock()
	return store, querier, loaded, &now
}

func TestSessionStoreUsesOneAuthoritativeReadPerLease(t *testing.T) {
	store, querier, sess, now := setupAuthenticatedSessionStore(t)
	ctx := t.Context()

	*now = now.Add(sessionValidationLease - time.Nanosecond)
	for range 10 {
		_, err := store.Read(ctx, sess.ID(), false)
		if err != nil {
			t.Fatal(err)
		}
	}
	querier.mu.Lock()
	if querier.dbCalls != 0 {
		t.Fatalf("requests inside one lease caused %d authoritative reads", querier.dbCalls)
	}
	querier.mu.Unlock()

	*now = now.Add(time.Nanosecond)
	querier.mu.Lock()
	querier.getStarted = make(chan struct{})
	querier.getContinue = make(chan struct{})
	getStarted := querier.getStarted
	getContinue := querier.getContinue
	querier.mu.Unlock()
	const requestCount = 20
	start := make(chan struct{})
	errs := make(chan error, requestCount)
	var wg sync.WaitGroup
	for range requestCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.Read(ctx, sess.ID(), false)
			errs <- err
		}()
	}
	close(start)
	<-getStarted
	time.Sleep(20 * time.Millisecond)
	close(getContinue)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	querier.mu.Lock()
	if querier.dbCalls != 1 {
		t.Fatalf("concurrent requests after lease expiry caused %d authoritative reads, want 1", querier.dbCalls)
	}
	querier.mu.Unlock()
}

func TestSessionStoreDestroyRevokesLocallyAuthenticatedRegistration(t *testing.T) {
	store, querier, sess := setupLocallyAuthenticatedRegistration(t)

	result, err := store.Destroy(t.Context(), sess.ID())
	if err != nil {
		t.Fatal(err)
	}
	if result.UserID != 42 || !result.Transitioned {
		t.Fatalf("registration revocation result = %+v, want user 42 transition", result)
	}
	row := querier.sessions[sess.ID()]
	if row.State != dbgen.SessionStateRevoked || row.Version != 2 || row.UserID.Valid {
		t.Fatalf("registration revocation authority = (%q, %d, %v)", row.State, row.Version, row.UserID)
	}
	finalized, err := store.FinalizeRegistration(t.Context(), sess, 42)
	if err != nil || finalized {
		t.Fatalf("finalizer after logout = (%v, %v), want (false, nil)", finalized, err)
	}
}

func TestSessionStoreCoalescesAuthoritativeErrorAfterLeaseExpiry(t *testing.T) {
	store, querier, sess, now := setupAuthenticatedSessionStore(t)
	expectedErr := errors.New("database unavailable")
	*now = now.Add(sessionValidationLease)
	querier.mu.Lock()
	querier.getSessionError = expectedErr
	querier.getStarted = make(chan struct{})
	querier.getContinue = make(chan struct{})
	getStarted := querier.getStarted
	getContinue := querier.getContinue
	querier.mu.Unlock()

	const requestCount = 20
	start := make(chan struct{})
	errs := make(chan error, requestCount)
	var wg sync.WaitGroup
	for range requestCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.Read(t.Context(), sess.ID(), false)
			errs <- err
		}()
	}
	close(start)
	<-getStarted
	time.Sleep(20 * time.Millisecond)
	close(getContinue)
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, expectedErr) {
			t.Fatalf("Read error = %v, want %v", err, expectedErr)
		}
	}
	querier.mu.Lock()
	if querier.dbCalls != 1 {
		t.Fatalf("concurrent failed validations caused %d authoritative reads, want 1", querier.dbCalls)
	}
	querier.mu.Unlock()
}

func TestSessionStoreClaimsExpirationRenewalOnceBelowThreshold(t *testing.T) {
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	store.now = func() time.Time { return now }

	atThreshold := session.NewSession(session.NewSessionData("threshold-sid"), store)
	atThreshold.Data().SetAuthority(session.StateAuthenticated, 1, now.Add(sessionExpirationRenewalThreshold), now)
	if store.RenewExpiration(t.Context(), atThreshold) {
		t.Fatal("session renewed at the threshold")
	}

	sess := session.NewSession(session.NewSessionData("renewal-sid"), store)
	sess.Data().SetAuthority(session.StateAuthenticated, 1, now.Add(sessionExpirationRenewalThreshold-time.Nanosecond), now)
	cancelledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if store.RenewExpiration(cancelledCtx, sess) {
		t.Fatal("session renewal was queued on a cancelled context")
	}
	if !store.RenewExpiration(t.Context(), sess) {
		t.Fatal("failed enqueue did not release the expiration renewal claim")
	}
	if store.RenewExpiration(t.Context(), sess) {
		t.Fatal("session queued a duplicate expiration renewal")
	}
	if sid := <-store.expirationChan; sid != sess.ID() {
		t.Fatalf("queued SID = %q, want %q", sid, sess.ID())
	}
}

func TestSessionStoreCreateAndDestroyDoNotResurrect(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	createStarted := make(chan struct{})
	createContinue := make(chan struct{})
	querier.createStarted = createStarted
	querier.createContinue = createContinue
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	sess := session.NewSession(session.NewSessionData("create-destroy-sid"), store)
	if err := store.Init(ctx, sess); err != nil {
		t.Fatal(err)
	}

	persistDone := make(chan error, 1)
	go func() { persistDone <- sess.Persist(ctx) }()
	<-createStarted
	destroyDone := make(chan error, 1)
	go func() {
		_, err := store.Destroy(ctx, sess.ID())
		destroyDone <- err
	}()
	close(createContinue)

	if err := <-persistDone; err != nil {
		t.Fatal(err)
	}
	if err := <-destroyDone; err != nil {
		t.Fatal(err)
	}
	row, err := querier.GetSessionByID(ctx, sess.ID())
	if err != nil {
		t.Fatal(err)
	}
	if row.State != dbgen.SessionStateRevoked || row.Version != 2 {
		t.Fatalf("destroyed session authority = (%q, %d), want (revoked, 2)", row.State, row.Version)
	}
}

func TestSessionStoreConsumeAdoptsCommittedSuccessorAfterError(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	now := time.Now()
	current := session.NewSession(session.NewSessionData("ambiguous-consume-old"), store)
	if err := current.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	if err := current.Set(ctx, session.KeyUserID, int32(42)); err != nil {
		t.Fatal(err)
	}
	current.Data().SetAuthority(session.StatePending, 1, now.Add(time.Hour), now)
	if err := store.Init(ctx, current); err != nil {
		t.Fatal(err)
	}
	payload, err := current.Data().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	querier.sessions[current.ID()] = &dbgen.Session{
		SessionID:          current.ID(),
		State:              dbgen.SessionStatePending,
		Version:            1,
		UserID:             pgtype.Int4{Int32: 42, Valid: true},
		Data:               payload,
		ExpiresAt:          pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ChallengeKind:      dbgen.NullSessionChallengeKind{SessionChallengeKind: dbgen.SessionChallengeKindSignIn, Valid: true},
		ChallengeCode:      pgtype.Text{String: "encoded-code", Valid: true},
		ChallengeEmail:     pgtype.Text{String: "user@example.com", Valid: true},
		ChallengeExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true},
	}
	successor := session.NewSession(session.NewSessionData("ambiguous-consume-new"), store)
	successor.Merge(current)
	querier.failNextConsumeSignInChallengeAfterCommit(errors.New("connection lost after commit"))

	result, err := store.ConsumeSignInChallenge(ctx, current, successor, "encoded-code", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Consumed {
		t.Fatal("committed successor was not adopted")
	}
	state, _, validatedAt := successor.Data().Authority()
	if state != session.StateAuthenticated || validatedAt.IsZero() {
		t.Fatalf("successor authority = (%q, %v), want authenticated with a validation lease", state, validatedAt)
	}
}

func TestSessionStoreRegistrationConsumeAdoptsCommittedSuccessorAfterError(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	now := time.Now()
	current := session.NewSession(session.NewSessionData("ambiguous-registration-old"), store)
	if err := store.Init(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := current.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	current.Data().SetAuthority(session.StatePending, 1, now.Add(time.Hour), now)
	payload, err := current.Data().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	querier.sessions[current.ID()] = &dbgen.Session{
		SessionID:          current.ID(),
		State:              dbgen.SessionStatePending,
		Version:            1,
		Data:               payload,
		ExpiresAt:          pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ChallengeKind:      dbgen.NullSessionChallengeKind{SessionChallengeKind: dbgen.SessionChallengeKindRegistration, Valid: true},
		ChallengeCode:      pgtype.Text{String: "encoded-code", Valid: true},
		ChallengeEmail:     pgtype.Text{String: "registration@example.com", Valid: true},
		ChallengeExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true},
	}
	successor := session.NewSession(session.NewSessionData("ambiguous-registration-new"), store)
	successor.Merge(current)
	querier.consumeRegistrationCommitError = errors.New("connection lost after commit")

	result, err := store.ConsumeRegistrationChallenge(ctx, current, successor, "encoded-code", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Consumed || !result.Verified || result.Email != "registration@example.com" {
		t.Fatalf("committed registration result = %+v", result)
	}
	state, _, _ := successor.Data().Authority()
	if state != session.StateRegistrationProcessing {
		t.Fatalf("registration successor state = %q, want processing", state)
	}
}

func TestSessionStoreRegistrationFinalizeAdoptsCommittedUpdateAfterError(t *testing.T) {
	store, querier, sess := setupLocallyAuthenticatedRegistration(t)
	querier.finalizeCommitError = errors.New("connection lost after commit")

	finalized, err := store.FinalizeRegistration(t.Context(), sess, 42)
	if err != nil || !finalized {
		t.Fatalf("FinalizeRegistration() = (%v, %v), want (true, nil)", finalized, err)
	}
	state, _, _ := sess.Data().Authority()
	if state != session.StateAuthenticated || sess.Data().RegistrationFinalizing() {
		t.Fatalf("reconciled registration authority = (%q, finalizing %t)", state, sess.Data().RegistrationFinalizing())
	}
}

func TestSessionStoreSerializesRecoveryWithPersistence(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	sess := session.NewSession(session.NewSessionData("recover-race-sid"), store)
	if err := store.Init(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(ctx, session.KeyUserName, "before"); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan
	if err := sess.Persist(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(ctx, session.KeyUserName, "after"); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan

	updateStarted := make(chan struct{})
	updateContinue := make(chan struct{})
	querier.updateStarted = updateStarted
	querier.updateContinue = updateContinue
	persistDone := make(chan error, 1)
	go func() { persistDone <- store.persistSessions(ctx, map[string]uint{sess.ID(): 1}) }()
	<-updateStarted
	recoverDone := make(chan error, 1)
	recoverStarted := make(chan struct{})
	go func() {
		close(recoverStarted)
		recoverDone <- store.Recover(ctx, sess)
	}()
	<-recoverStarted
	select {
	case err := <-recoverDone:
		t.Fatalf("recovery completed during in-flight persistence: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(updateContinue)
	if err := <-persistDone; err != nil {
		t.Fatal(err)
	}
	if err := <-recoverDone; err != nil {
		t.Fatal(err)
	}
	if name := sess.Get(ctx, session.KeyUserName); name != "after" {
		t.Fatalf("recovered name = %v, want after", name)
	}
}
