package db

import (
	"context"
	"errors"
	"testing"
	"time"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5/pgtype"
)

var errUnexpectedExpirationRenewal = errors.New("unexpected expiration renewal")

type expirationRenewalResponse struct {
	results []*dbgen.RenewSessionExpirationsRow
	err     error
}

type scriptedExpirationRenewalQuerier struct {
	*QuerierStub
	responses []expirationRenewalResponse
	params    []*dbgen.RenewSessionExpirationsParams
}

func newScriptedExpirationRenewalQuerier() *scriptedExpirationRenewalQuerier {
	return &scriptedExpirationRenewalQuerier{QuerierStub: &QuerierStub{}}
}

func (q *scriptedExpirationRenewalQuerier) enqueue(response expirationRenewalResponse) {
	q.responses = append(q.responses, response)
}

func (q *scriptedExpirationRenewalQuerier) RenewSessionExpirations(
	_ context.Context,
	params *dbgen.RenewSessionExpirationsParams,
) ([]*dbgen.RenewSessionExpirationsRow, error) {
	q.params = append(q.params, &dbgen.RenewSessionExpirationsParams{
		SessionIds: append([]string(nil), params.SessionIds...),
		Ttl:        params.Ttl,
	})
	if len(q.responses) == 0 {
		return nil, errUnexpectedExpirationRenewal
	}
	response := q.responses[0]
	q.responses = q.responses[1:]
	return response.results, response.err
}

func newExpirationRenewalWorkerStore(q *scriptedExpirationRenewalQuerier) *SessionStore {
	business := NewBusinessWithQuerier(nil, q, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
	return NewSessionStore(business, &sessionMetricsStub{})
}

func expirationRenewalRow(sid string, version int32, expiresAt time.Time) *dbgen.RenewSessionExpirationsRow {
	return &dbgen.RenewSessionExpirationsRow{
		SessionID: sid,
		Version:   version,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}
}

func TestSessionExpirationRenewalPublishesReturnedExpirationOnly(t *testing.T) {
	q := newScriptedExpirationRenewalQuerier()
	renewedAt := time.Now().Add(sessionCacheTTL)
	q.enqueue(expirationRenewalResponse{results: []*dbgen.RenewSessionExpirationsRow{expirationRenewalRow(t.Name(), 8, renewedAt)}})
	store := newExpirationRenewalWorkerStore(q)
	leaseUntil := time.Now().Add(5 * time.Minute)
	original := cachePayloadSession(store, t.Name(), session.Authority{
		State:      session.StateAuthenticated,
		Version:    8,
		UserID:     42,
		ExpiresAt:  time.Now().Add(time.Hour),
		LeaseUntil: leaseUntil,
	})

	if err := store.renewSessionExpirations(t.Context(), map[string]uint{t.Name(): 1}); err != nil {
		t.Fatal(err)
	}
	cached, ok := store.sessionCache.GetIfPresent(t.Name())
	if !ok {
		t.Fatal("renewed session was evicted")
	}
	authority, _ := cached.Authority()
	if authority.Version != 8 || authority.UserID != 42 || authority.State != session.StateAuthenticated || !authority.ExpiresAt.Equal(renewedAt) ||
		!authority.LeaseUntil.Equal(leaseUntil) {
		t.Fatalf("published Authority = %+v", authority)
	}
	if cached.Payload() != original.Payload() {
		t.Fatal("renewal replaced Payload")
	}
}

func TestSessionExpirationRenewalEvictsUnsafeResults(t *testing.T) {
	q := newScriptedExpirationRenewalQuerier()
	changedSID := t.Name() + "/changed"
	missingSID := t.Name() + "/missing"
	q.enqueue(expirationRenewalResponse{results: []*dbgen.RenewSessionExpirationsRow{
		expirationRenewalRow(changedSID, 4, time.Now().Add(sessionCacheTTL)),
	}})
	store := newExpirationRenewalWorkerStore(q)
	authority := session.Authority{State: session.StateAuthenticated, Version: 3, ExpiresAt: time.Now().Add(time.Hour)}
	cachePayloadSession(store, changedSID, authority)
	cachePayloadSession(store, missingSID, authority)

	if err := store.renewSessionExpirations(t.Context(), map[string]uint{changedSID: 1, missingSID: 1}); err != nil {
		t.Fatal(err)
	}
	if len(q.params) != 1 || len(q.params[0].SessionIds) != 2 || q.params[0].Ttl != sessionCacheTTL {
		t.Fatalf("renewal queries = %+v, want one two-session batch", q.params)
	}
	batched := make(map[string]bool, len(q.params[0].SessionIds))
	for _, sid := range q.params[0].SessionIds {
		batched[sid] = true
	}
	if !batched[changedSID] || !batched[missingSID] {
		t.Fatalf("renewal batch SIDs = %v, want %q and %q", q.params[0].SessionIds, changedSID, missingSID)
	}
	if _, ok := store.sessionCache.GetIfPresent(changedSID); ok {
		t.Fatal("changed-version renewal remained cached")
	}
	if _, ok := store.sessionCache.GetIfPresent(missingSID); ok {
		t.Fatal("missing renewal result remained cached")
	}
}

func TestSessionExpirationRenewalFailureKeepsCachedAuthority(t *testing.T) {
	q := newScriptedExpirationRenewalQuerier()
	q.enqueue(expirationRenewalResponse{err: errors.New("renewal failed")})
	store := newExpirationRenewalWorkerStore(q)
	authority := session.Authority{State: session.StateAuthenticated, Version: 3, ExpiresAt: time.Now().Add(time.Hour)}
	original := cachePayloadSession(store, t.Name(), authority)

	if err := store.renewSessionExpirations(t.Context(), map[string]uint{t.Name(): 1}); err == nil {
		t.Fatal("renewal failure was ignored")
	}
	cached, ok := store.sessionCache.GetIfPresent(t.Name())
	if !ok || cached != original {
		t.Fatal("renewal failure changed cached session")
	}
}

func TestSessionExpirationRenewalQueueDoesNotBlockWhenFull(t *testing.T) {
	store := newExpirationRenewalWorkerStore(newScriptedExpirationRenewalQuerier())
	for i := 0; i < cap(store.renewalChan); i++ {
		store.renewalChan <- t.Name()
	}
	done := make(chan struct{})
	go func() {
		store.EnqueueExpirationRenewal(t.Context(), t.Name())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expiration renewal enqueue blocked on a full queue")
	}
}

func TestSessionExpirationRenewalShutdownIsSafe(t *testing.T) {
	store := newExpirationRenewalWorkerStore(newScriptedExpirationRenewalQuerier())
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-start
		for i := 0; i < 100; i++ {
			store.EnqueueExpirationRenewal(t.Context(), t.Name())
		}
		close(done)
	}()
	close(start)
	store.Shutdown()
	<-done
	store.EnqueueExpirationRenewal(t.Context(), t.Name())
}
