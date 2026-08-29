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
	rotateCommitError              error
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

type concurrentCreateQuerier struct {
	*memorySessionQuerier
	started chan string
	waits   map[string]<-chan struct{}
}

func (q *concurrentCreateQuerier) CreateSession(ctx context.Context, arg *dbgen.CreateSessionParams) (*dbgen.Session, error) {
	q.started <- arg.SessionID
	if wait := q.waits[arg.SessionID]; wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return q.memorySessionQuerier.CreateSession(ctx, arg)
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
func (q *memorySessionQuerier) GetSessionByIDUnfiltered(ctx context.Context, sid string) (*dbgen.Session, error) {
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
func (q *memorySessionQuerier) RotateSession(ctx context.Context, arg *dbgen.RotateSessionParams) (*dbgen.RotateSessionRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dbCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	old, ok := q.sessions[arg.OldSessionID]
	now := q.now
	if now.IsZero() {
		now = time.Now()
	}
	if !ok || old.State != dbgen.SessionStateAuthenticated || old.Version != arg.ExpectedVersion || !old.UserID.Valid || old.UserID.Int32 != arg.ExpectedUserID || !old.ExpiresAt.Valid || now.After(old.ExpiresAt.Time) {
		return nil, pgx.ErrNoRows
	}
	if _, exists := q.sessions[arg.NewSessionID]; exists {
		return nil, errors.New("session already exists")
	}
	old.State = dbgen.SessionStateRevoked
	old.Version++
	successor := &dbgen.Session{
		SessionID: arg.NewSessionID,
		State:     dbgen.SessionStateAuthenticated,
		Version:   1,
		UserID:    old.UserID,
		Data:      append([]byte(nil), arg.SuccessorData...),
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(arg.SuccessorTtl), Valid: true},
	}
	q.sessions[arg.NewSessionID] = successor
	if q.rotateCommitError != nil {
		err := q.rotateCommitError
		q.rotateCommitError = nil
		return nil, err
	}
	return &dbgen.RotateSessionRow{
		SessionID: successor.SessionID,
		State:     successor.State,
		Version:   successor.Version,
		UserID:    successor.UserID,
		Data:      append([]byte(nil), successor.Data...),
		ExpiresAt: successor.ExpiresAt,
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

func TestSessionStoreRenewDoesNotReplaceDestroyedSession(t *testing.T) {
	store, querier, sess, _ := setupAuthenticatedSessionStore(t)
	if _, err := store.Destroy(t.Context(), sess.ID()); err != nil {
		t.Fatal(err)
	}

	successor := session.NewSession(session.NewSessionData("renew-after-destroy"), store)
	if err := store.Renew(t.Context(), sess, successor, func() { successor.Merge(sess) }); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("Renew error = %v, want session missing", err)
	}
	querier.mu.Lock()
	_, created := querier.sessions[successor.ID()]
	querier.mu.Unlock()
	if created {
		t.Fatal("renewal created a successor after the predecessor was destroyed")
	}
}

func TestSessionStoreRenewRejectsRevokedPostgresPredecessor(t *testing.T) {
	store, querier, sess, _ := setupAuthenticatedSessionStore(t)
	querier.mu.Lock()
	querier.sessions[sess.ID()].State = dbgen.SessionStateRevoked
	querier.sessions[sess.ID()].Version++
	querier.mu.Unlock()

	successor := session.NewSession(session.NewSessionData("renew-after-remote-destroy"), store)
	if err := store.Renew(t.Context(), sess, successor, func() { successor.Merge(sess) }); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("Renew error = %v, want session missing", err)
	}
	querier.mu.Lock()
	_, created := querier.sessions[successor.ID()]
	querier.mu.Unlock()
	if created {
		t.Fatal("renewal created a successor for a remotely revoked predecessor")
	}
}

func TestSessionStoreRenewAdoptsCommittedSuccessorAfterError(t *testing.T) {
	store, querier, sess, _ := setupAuthenticatedSessionStore(t)
	successor := session.NewSession(session.NewSessionData("renew-after-commit-error"), store)
	querier.rotateCommitError = errors.New("connection lost after commit")

	if err := store.Renew(t.Context(), sess, successor, func() { successor.Merge(sess) }); err != nil {
		t.Fatal(err)
	}
	state, _, validatedAt := successor.Data().Authority()
	if state != session.StateAuthenticated || validatedAt.IsZero() {
		t.Fatalf("successor authority = (%q, %v), want authenticated with validation lease", state, validatedAt)
	}
	cached, err := store.store.Impl().GetCachedUserSession(t.Context(), successor.ID())
	if err != nil || cached != successor.Data() {
		t.Fatalf("renewed cache = (%p, %v), want %p", cached, err, successor.Data())
	}
}

func TestSessionStoreRenewUsesPredecessorAuthority(t *testing.T) {
	store, querier, sess, _ := setupAuthenticatedSessionStore(t)
	if err := sess.Delete(t.Context(), session.KeyPersistent); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan
	successor := session.NewSession(session.NewSessionData("renew-without-marker"), store)

	if err := store.Renew(t.Context(), sess, successor, func() { successor.Merge(sess) }); err != nil {
		t.Fatal(err)
	}
	querier.mu.Lock()
	old := querier.sessions[sess.ID()]
	created := querier.sessions[successor.ID()]
	querier.mu.Unlock()
	if old.State != dbgen.SessionStateRevoked || created == nil || created.State != dbgen.SessionStateAuthenticated {
		t.Fatalf("renewal authority = (old %q, successor %v)", old.State, created)
	}
}

func TestSessionStoreRenewRejectsPendingPredecessor(t *testing.T) {
	store, querier, sess, now := setupAuthenticatedSessionStore(t)
	sess.Data().SetAuthority(session.StatePending, 1, now.Add(time.Hour), *now)
	querier.mu.Lock()
	querier.sessions[sess.ID()].State = dbgen.SessionStatePending
	querier.mu.Unlock()
	successor := session.NewSession(session.NewSessionData("renew-pending"), store)

	if err := store.Renew(t.Context(), sess, successor, func() { successor.Merge(sess) }); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("Renew error = %v, want session missing", err)
	}
	querier.mu.Lock()
	_, created := querier.sessions[successor.ID()]
	querier.mu.Unlock()
	if created {
		t.Fatal("ordinary renewal promoted a pending predecessor")
	}
}

func TestSessionStoreRenewKeepsPredecessorOnPrequeryError(t *testing.T) {
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, nil, cache), session.KeyPersistent, &sessionMetricsStub{})
	current := session.NewSession(session.NewSessionData("renew-maintenance-old"), store)
	current.Data().SetAuthoritativeUserID(42)
	current.Data().SetAuthority(session.StateAuthenticated, 1, time.Now().Add(time.Hour), time.Now())
	if err := store.Init(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	if err := current.Set(t.Context(), session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan
	successor := session.NewSession(session.NewSessionData("renew-maintenance-new"), store)

	if err := store.Renew(t.Context(), current, successor, func() { successor.Merge(current) }); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("Renew error = %v, want maintenance", err)
	}
	if current.Data().IsStale() {
		t.Fatal("pre-query failure invalidated a usable predecessor")
	}
	cached, err := store.store.Impl().GetCachedUserSession(t.Context(), current.ID())
	if err != nil || cached != current.Data() {
		t.Fatalf("predecessor cache = (%p, %v), want %p", cached, err, current.Data())
	}
}

func TestSessionStoreRenewRejectsReplacedCacheIncarnation(t *testing.T) {
	store, querier, sess, _ := setupAuthenticatedSessionStore(t)
	replacement := session.NewSessionData(sess.ID())
	replacement.Replace(sess.Data())
	if err := store.store.Impl().CacheUserSession(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	successor := session.NewSession(session.NewSessionData("renew-replaced-cache"), store)

	if err := store.Renew(t.Context(), sess, successor, func() { successor.Merge(sess) }); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("Renew error = %v, want session missing", err)
	}
	if !sess.Data().IsStale() {
		t.Fatal("replaced predecessor incarnation remained usable")
	}
	querier.mu.Lock()
	_, created := querier.sessions[successor.ID()]
	querier.mu.Unlock()
	if created {
		t.Fatal("renewal created a successor from a replaced cache incarnation")
	}
}

func TestSessionStoreRenewIncludesCompletedMutation(t *testing.T) {
	store, querier, sess, _ := setupAuthenticatedSessionStore(t)
	successor := session.NewSession(session.NewSessionData("renew-after-mutation"), store)
	if err := sess.Set(t.Context(), session.KeyUserName, "latest"); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan

	if err := store.Renew(t.Context(), sess, successor, func() { successor.Merge(sess) }); err != nil {
		t.Fatal(err)
	}
	querier.mu.Lock()
	row := cloneSessionRow(querier.sessions[successor.ID()])
	querier.mu.Unlock()
	payload := session.NewSessionData(successor.ID())
	if err := payload.UnmarshalBinary(row.Data); err != nil {
		t.Fatal(err)
	}
	stored := session.NewSession(payload, store)
	if name := stored.Get(t.Context(), session.KeyUserName); name != "latest" {
		t.Fatalf("successor payload name = %v, want latest", name)
	}
}

func TestSessionStoreRejectsMutationAfterDestroy(t *testing.T) {
	store, _, sess, _ := setupAuthenticatedSessionStore(t)
	if _, err := store.Destroy(t.Context(), sess.ID()); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(t.Context(), session.KeyUserName, "after-destroy"); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("Set error = %v, want session missing", err)
	}
	if name, ok := sess.Get(t.Context(), session.KeyUserName).(string); ok {
		t.Fatalf("destroyed session accepted mutation %q", name)
	}
}

func TestSessionStoreValidationCancellationIsCallerScoped(t *testing.T) {
	store, querier, _, now := setupAuthenticatedSessionStore(t)
	*now = now.Add(sessionValidationLease)
	getStarted := make(chan struct{})
	getContinue := make(chan struct{})
	querier.mu.Lock()
	querier.getStarted = getStarted
	querier.getContinue = getContinue
	querier.mu.Unlock()
	leaderCtx, cancelLeader := context.WithCancel(t.Context())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := store.Read(leaderCtx, "authenticated-sid", false)
		leaderDone <- err
	}()
	<-getStarted

	followerStarted := make(chan struct{})
	followerDone := make(chan error, 1)
	go func() {
		close(followerStarted)
		_, err := store.Read(t.Context(), "authenticated-sid", false)
		followerDone <- err
	}()
	<-followerStarted
	time.Sleep(20 * time.Millisecond)
	cancelLeader()
	close(getContinue)

	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want canceled", err)
	}
	if err := <-followerDone; err != nil {
		t.Fatalf("healthy follower inherited leader cancellation: %v", err)
	}
	querier.mu.Lock()
	dbCalls := querier.dbCalls
	querier.mu.Unlock()
	if dbCalls != 2 {
		t.Fatalf("validation database calls = %d, want canceled and successful attempts", dbCalls)
	}
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

func TestSessionStoreCreatesDifferentSessionsConcurrently(t *testing.T) {
	ctx := t.Context()
	firstContinue := make(chan struct{})
	querier := &concurrentCreateQuerier{
		memorySessionQuerier: newMemorySessionQuerier(),
		started:              make(chan string, 2),
		waits:                map[string]<-chan struct{}{"first-sid": firstContinue},
	}
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})

	newPersistentSession := func(sid string) *session.Session {
		sess := session.NewSession(session.NewSessionData(sid), store)
		if err := store.Init(ctx, sess); err != nil {
			t.Fatal(err)
		}
		if err := sess.Set(ctx, session.KeyPersistent, true); err != nil {
			t.Fatal(err)
		}
		return sess
	}
	first := newPersistentSession("first-sid")
	second := newPersistentSession("second-sid")

	firstDone := make(chan error, 1)
	go func() { firstDone <- store.Create(ctx, first) }()
	if sid := <-querier.started; sid != first.ID() {
		t.Fatalf("first started SID = %q, want %q", sid, first.ID())
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- store.Create(ctx, second) }()
	select {
	case sid := <-querier.started:
		if sid != second.ID() {
			t.Fatalf("second started SID = %q, want %q", sid, second.ID())
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second session create was blocked by the first SID")
	}

	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	close(firstContinue)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestSessionStoreConsumeAdoptsCommittedSuccessorAfterError(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	now := time.Now()
	current := session.NewSession(session.NewSessionData("ambiguous-consume-old"), store)
	if err := store.Init(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := current.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	if err := current.Set(ctx, session.KeyUserID, int32(42)); err != nil {
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
		UserID:             pgtype.Int4{Int32: 42, Valid: true},
		Data:               payload,
		ExpiresAt:          pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ChallengeKind:      dbgen.NullSessionChallengeKind{SessionChallengeKind: dbgen.SessionChallengeKindSignIn, Valid: true},
		ChallengeCode:      pgtype.Text{String: "encoded-code", Valid: true},
		ChallengeEmail:     pgtype.Text{String: "user@example.com", Valid: true},
		ChallengeExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true},
	}
	successor := session.NewSession(session.NewSessionData("ambiguous-consume-new"), store)
	querier.failNextConsumeSignInChallengeAfterCommit(errors.New("connection lost after commit"))

	result, err := store.ConsumeSignInChallenge(ctx, current, successor, func() { successor.Merge(current) }, "encoded-code", 5)
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

func TestSessionStoreChallengeMutationForcesOneAuthoritativeReload(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})
	now := time.Now()
	current := session.NewSession(session.NewSessionData("failed-challenge-old"), store)
	if err := store.Init(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := current.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	if err := current.Set(ctx, session.KeyUserID, int32(42)); err != nil {
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
		UserID:             pgtype.Int4{Int32: 42, Valid: true},
		Data:               payload,
		ExpiresAt:          pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ChallengeKind:      dbgen.NullSessionChallengeKind{SessionChallengeKind: dbgen.SessionChallengeKindSignIn, Valid: true},
		ChallengeCode:      pgtype.Text{String: "encoded-code", Valid: true},
		ChallengeEmail:     pgtype.Text{String: "user@example.com", Valid: true},
		ChallengeExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true},
	}
	replacement := session.NewSessionData(current.ID())
	replacement.Replace(current.Data())
	if err := store.store.Impl().CacheUserSession(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	replacementSession := session.NewSession(replacement, store)
	if err := replacementSession.Set(ctx, session.KeyReturnURL, "/latest"); err != nil {
		t.Fatal(err)
	}
	successor := session.NewSession(session.NewSessionData("failed-challenge-new"), store)

	result, err := store.ConsumeSignInChallenge(ctx, replacementSession, successor, func() { successor.Merge(replacementSession) }, "wrong-code", 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Consumed {
		t.Fatal("wrong challenge code was consumed")
	}
	if _, err := store.store.Impl().GetCachedUserSession(ctx, current.ID()); !isSessionCacheMiss(err) {
		t.Fatalf("mutated challenge remained cached: %v", err)
	}

	if _, err := store.Read(ctx, current.ID(), false); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Read(ctx, current.ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	if returnURL := reloaded.Get(ctx, session.KeyReturnURL); returnURL != "/latest" {
		t.Fatalf("reloaded return URL = %v, want /latest", returnURL)
	}
	querier.mu.Lock()
	dbCalls := querier.dbCalls
	querier.mu.Unlock()
	if dbCalls != 3 {
		t.Fatalf("dirty challenge mutation and two reads caused %d database calls, want 3", dbCalls)
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
	querier.consumeRegistrationCommitError = errors.New("connection lost after commit")

	result, err := store.ConsumeRegistrationChallenge(ctx, current, successor, func() { successor.Merge(current) }, "encoded-code", 5, true)
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
	cached, err := store.store.Impl().GetCachedUserSession(t.Context(), sess.ID())
	if err != nil || cached != sess.Data() {
		t.Fatalf("finalized registration cache = (%p, %v), want %p", cached, err, sess.Data())
	}
}

func TestSessionStoreRegistrationFinalizePersistsMutationAfterDelayedReconciliation(t *testing.T) {
	store, querier, sess := setupLocallyAuthenticatedRegistration(t)
	commitErr := errors.New("connection lost after commit")
	querier.finalizeCommitError = commitErr
	querier.getSessionError = errors.New("reconciliation temporarily unavailable")
	if finalized, err := store.FinalizeRegistration(t.Context(), sess, 42); !errors.Is(err, commitErr) || finalized {
		t.Fatalf("first FinalizeRegistration() = (%v, %v), want (false, commit error)", finalized, err)
	}
	if err := sess.Set(t.Context(), session.KeyUserName, "after-commit"); err != nil {
		t.Fatal(err)
	}

	finalized, err := store.FinalizeRegistration(t.Context(), sess, 42)
	if err != nil || !finalized {
		t.Fatalf("retry FinalizeRegistration() = (%v, %v), want (true, nil)", finalized, err)
	}
	if sid := <-store.persistDelayChan; sid != sess.ID() {
		t.Fatalf("queued SID = %q, want %q", sid, sess.ID())
	}
	if err := store.persistSessions(t.Context(), map[string]uint{sess.ID(): 1}); err != nil {
		t.Fatal(err)
	}
	querier.mu.Lock()
	row := cloneSessionRow(querier.sessions[sess.ID()])
	querier.mu.Unlock()
	payload := session.NewSessionData(sess.ID())
	if err := payload.UnmarshalBinary(row.Data); err != nil {
		t.Fatal(err)
	}
	stored := session.NewSession(payload, store)
	if name := stored.Get(t.Context(), session.KeyUserName); name != "after-commit" {
		t.Fatalf("finalized payload name = %v, want after-commit", name)
	}
}

func TestSessionStoreRegistrationFinalizeWaitsForPersistenceQueue(t *testing.T) {
	store, _, sess := setupLocallyAuthenticatedRegistration(t)
	for range cap(store.persistDelayChan) {
		store.persistDelayChan <- "blocked"
	}
	drained := make(chan struct{})
	go func() {
		time.Sleep(sessionBackpressureTimeout + 50*time.Millisecond)
		<-store.persistDelayChan
		close(drained)
	}()

	finalized, err := store.FinalizeRegistration(t.Context(), sess, 42)
	if err != nil || !finalized {
		t.Fatalf("FinalizeRegistration() = (%v, %v), want (true, nil)", finalized, err)
	}
	<-drained
	queued := false
	for range len(store.persistDelayChan) {
		if sid := <-store.persistDelayChan; sid == sess.ID() {
			queued = true
		}
	}
	if !queued {
		t.Fatal("finalized payload was not admitted to the persistence queue")
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

func TestSessionStoreSerializesMutationWithRecovery(t *testing.T) {
	store, querier, sess, _ := setupAuthenticatedSessionStore(t)
	ctx := t.Context()
	getStarted := make(chan struct{})
	getContinue := make(chan struct{})
	querier.mu.Lock()
	querier.getStarted = getStarted
	querier.getContinue = getContinue
	querier.mu.Unlock()

	recoverDone := make(chan error, 1)
	go func() { recoverDone <- store.Recover(ctx, sess) }()
	<-getStarted

	mutationDone := make(chan error, 1)
	go func() { mutationDone <- sess.Set(ctx, session.KeyUserName, "during-recovery") }()
	select {
	case err := <-mutationDone:
		t.Fatalf("mutation completed during authoritative recovery: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(getContinue)
	if err := <-recoverDone; err != nil {
		t.Fatal(err)
	}
	if err := <-mutationDone; err != nil {
		t.Fatal(err)
	}
	if name, ok := sess.Get(ctx, session.KeyUserName).(string); !ok || name != "during-recovery" {
		t.Fatalf("session mutation after recovery = %v, want during-recovery", name)
	}
}

func TestSessionStorePreservesMutationBeforeRecovery(t *testing.T) {
	store, _, sess, _ := setupAuthenticatedSessionStore(t)
	if err := sess.Set(t.Context(), session.KeyUserName, "before-recovery"); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan

	if err := store.Recover(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	if name := sess.Get(t.Context(), session.KeyUserName); name != "before-recovery" {
		t.Fatalf("session mutation after recovery = %v, want before-recovery", name)
	}
}

func TestSessionStoreAuthoritativeMissEvictsCachedSession(t *testing.T) {
	store, querier, sess, _ := setupAuthenticatedSessionStore(t)
	querier.mu.Lock()
	delete(querier.sessions, sess.ID())
	querier.mu.Unlock()

	if err := store.Recover(t.Context(), sess); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("Recover error = %v, want session missing", err)
	}
	if !sess.Data().IsStale() {
		t.Fatal("authoritative miss left session incarnation live")
	}
	if _, err := store.store.Impl().GetCachedUserSession(t.Context(), sess.ID()); !isSessionCacheMiss(err) {
		t.Fatalf("authoritative miss left cached session: %v", err)
	}
}
