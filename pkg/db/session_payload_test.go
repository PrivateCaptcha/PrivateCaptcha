package db

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/maypok86/otter/v2"
)

var errUnexpectedPayloadWrite = errors.New("unexpected Payload write")

type payloadWriteResponse struct {
	results []*dbgen.UpdateSessionPayloadsRow
	err     error
	started chan<- *dbgen.UpdateSessionPayloadsParams
	release <-chan struct{}
}

type scriptedPayloadQuerier struct {
	*QuerierStub
	mu        sync.Mutex
	responses []payloadWriteResponse
}

func newScriptedPayloadQuerier() *scriptedPayloadQuerier {
	return &scriptedPayloadQuerier{QuerierStub: &QuerierStub{}}
}

func (q *scriptedPayloadQuerier) enqueue(response payloadWriteResponse) {
	q.mu.Lock()
	q.responses = append(q.responses, response)
	q.mu.Unlock()
}

func (q *scriptedPayloadQuerier) UpdateSessionPayloads(
	ctx context.Context,
	params *dbgen.UpdateSessionPayloadsParams,
) ([]*dbgen.UpdateSessionPayloadsRow, error) {
	q.mu.Lock()
	if len(q.responses) == 0 {
		q.mu.Unlock()
		return nil, errUnexpectedPayloadWrite
	}
	response := q.responses[0]
	q.responses = q.responses[1:]
	q.mu.Unlock()

	copyParams := &dbgen.UpdateSessionPayloadsParams{
		SessionIds:       append([]string(nil), params.SessionIds...),
		ExpectedVersions: append([]int32(nil), params.ExpectedVersions...),
		Payloads:         make([][]byte, len(params.Payloads)),
	}
	for i := range params.Payloads {
		copyParams.Payloads[i] = append([]byte(nil), params.Payloads[i]...)
	}
	if response.started != nil {
		response.started <- copyParams
	}
	if response.release != nil {
		select {
		case <-response.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return response.results, response.err
}

func newPayloadWorkerStore(q *scriptedPayloadQuerier) *SessionStore {
	business := NewBusinessWithQuerier(nil, q, NewStaticCache[CacheKey, any](100, &CacheMissingValue{}))
	return NewSessionStore(business, &sessionMetricsStub{})
}

func cachePayloadSession(store *SessionStore, sid string, authority session.Authority) *session.Session {
	payload := session.NewPayload(sid, store)
	sess := session.NewSessionWithAuthority(authority, payload)
	store.sessionCache.Set(sid, sess)
	return sess
}

func payloadName(t *testing.T, data []byte) string {
	t.Helper()
	payload := session.NewPayload("", noopPayloadStore{})
	if err := payload.Replace(data); err != nil {
		t.Fatal(err)
	}
	name, _ := payload.Get(session.KeyUserName).(string)
	return name
}

func waitPayloadWrite(t *testing.T, started <-chan *dbgen.UpdateSessionPayloadsParams) *dbgen.UpdateSessionPayloadsParams {
	t.Helper()
	select {
	case params := <-started:
		return params
	case <-time.After(time.Second):
		t.Fatal("Payload write did not start")
		return nil
	}
}

func waitSessionVersion(t *testing.T, store *SessionStore, sid string, version int32) *session.Session {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		cached, ok := store.sessionCache.GetIfPresent(sid)
		if ok {
			authority, _ := cached.Authority()
			if authority.Version == version {
				return cached
			}
		}
		select {
		case <-deadline:
			t.Fatalf("cached session did not reach version %d", version)
		default:
			runtime.Gosched()
		}
	}
}

type lifecycleWorkerQuerier struct {
	*QuerierStub
	renewedAt time.Time
}

func (q *lifecycleWorkerQuerier) UpdateSessionPayloads(
	_ context.Context,
	params *dbgen.UpdateSessionPayloadsParams,
) ([]*dbgen.UpdateSessionPayloadsRow, error) {
	results := make([]*dbgen.UpdateSessionPayloadsRow, 0, len(params.SessionIds))
	for i, sid := range params.SessionIds {
		results = append(results, &dbgen.UpdateSessionPayloadsRow{SessionID: sid, Version: params.ExpectedVersions[i] + 1})
	}
	return results, nil
}

func (q *lifecycleWorkerQuerier) RenewSessionExpirations(
	_ context.Context,
	params *dbgen.RenewSessionExpirationsParams,
) ([]*dbgen.RenewSessionExpirationsRow, error) {
	results := make([]*dbgen.RenewSessionExpirationsRow, 0, len(params.SessionIds))
	for _, sid := range params.SessionIds {
		results = append(results, &dbgen.RenewSessionExpirationsRow{
			SessionID: sid,
			Version:   7,
			ExpiresAt: pgtype.Timestamptz{Time: q.renewedAt, Valid: true},
		})
	}
	return results, nil
}

func TestSessionStoreStartRunsOnlyPayloadAndExpirationRenewalWorkers(t *testing.T) {
	payloadSID := t.Name() + "/payload"
	renewalSID := t.Name() + "/renewal"
	renewedAt := time.Now().Add(sessionCacheTTL)
	querier := &lifecycleWorkerQuerier{QuerierStub: &QuerierStub{}, renewedAt: renewedAt}
	store := NewSessionStore(
		NewBusinessWithQuerier(nil, querier, NewStaticCache[CacheKey, any](100, &CacheMissingValue{})),
		&sessionMetricsStub{},
	)
	store.batchSize = 1

	payloadSession := cachePayloadSession(store, payloadSID, session.Authority{
		State: session.StateAuthenticated, Version: 1, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err := payloadSession.Set(t.Context(), session.KeyUserName, "updated"); err != nil {
		t.Fatal(err)
	}
	cachePayloadSession(store, renewalSID, session.Authority{
		State: session.StateAuthenticated, Version: 7, ExpiresAt: time.Now().Add(time.Hour),
	})
	store.EnqueueExpirationRenewal(t.Context(), renewalSID)
	store.Start(t.Context(), time.Hour)
	t.Cleanup(store.Stop)

	waitSessionVersion(t, store, payloadSID, 2)
	deadline := time.After(time.Second)
	for {
		cached, ok := store.sessionCache.GetIfPresent(renewalSID)
		if ok {
			authority, _ := cached.Authority()
			if authority.ExpiresAt.Equal(renewedAt) {
				break
			}
		}
		select {
		case <-deadline:
			t.Fatal("cached session expiration was not renewed")
		default:
			runtime.Gosched()
		}
	}

	store.Shutdown()
}

func TestSessionPayloadWorkerPersistsMutationQueuedDuringWrite(t *testing.T) {
	q := newScriptedPayloadQuerier()
	firstStarted := make(chan *dbgen.UpdateSessionPayloadsParams, 1)
	firstRelease := make(chan struct{})
	secondStarted := make(chan *dbgen.UpdateSessionPayloadsParams, 1)
	q.enqueue(payloadWriteResponse{
		results: []*dbgen.UpdateSessionPayloadsRow{{SessionID: t.Name(), Version: 2}},
		started: firstStarted,
		release: firstRelease,
	})
	q.enqueue(payloadWriteResponse{
		results: []*dbgen.UpdateSessionPayloadsRow{{SessionID: t.Name(), Version: 9}},
		started: secondStarted,
	})
	store := newPayloadWorkerStore(q)
	store.batchSize = 1
	store.Start(t.Context(), time.Hour)
	t.Cleanup(store.Stop)

	expiresAt := time.Now().Add(time.Hour)
	leaseUntil := time.Now().Add(10 * time.Minute)
	sess := cachePayloadSession(store, t.Name(), session.Authority{
		State:      session.StateAuthenticated,
		Version:    1,
		UserID:     42,
		ExpiresAt:  expiresAt,
		LeaseUntil: leaseUntil,
	})
	originalPayload := sess.Payload()
	if err := sess.Set(t.Context(), session.KeyUserName, "first"); err != nil {
		t.Fatal(err)
	}
	first := waitPayloadWrite(t, firstStarted)
	if len(first.SessionIds) != 1 || first.SessionIds[0] != t.Name() || first.ExpectedVersions[0] != 1 || payloadName(t, first.Payloads[0]) != "first" {
		t.Fatalf("first Payload write = %+v", first)
	}

	if err := sess.Set(t.Context(), session.KeyUserName, "second"); err != nil {
		t.Fatal(err)
	}
	close(firstRelease)
	second := waitPayloadWrite(t, secondStarted)
	if len(second.SessionIds) != 1 || second.ExpectedVersions[0] != 2 || payloadName(t, second.Payloads[0]) != "second" {
		t.Fatalf("second Payload write = %+v", second)
	}

	cached := waitSessionVersion(t, store, t.Name(), 9)
	authority, _ := cached.Authority()
	if authority.UserID != 42 || !authority.ExpiresAt.Equal(expiresAt) || !authority.LeaseUntil.Equal(leaseUntil) {
		t.Fatalf("Payload publication changed Authority: %+v", authority)
	}
	if cached.Payload() != originalPayload {
		t.Fatal("Payload publication replaced local Payload")
	}
}

func TestSessionPayloadWorkerConflictEvictsOnlyMatchingVersion(t *testing.T) {
	t.Run("Matching", func(t *testing.T) {
		q := newScriptedPayloadQuerier()
		q.enqueue(payloadWriteResponse{})
		store := newPayloadWorkerStore(q)
		cachePayloadSession(store, t.Name(), session.Authority{State: session.StatePending, Version: 3, ExpiresAt: time.Now().Add(time.Hour)})

		if err := store.persistPayloads(t.Context(), map[string]uint{t.Name(): 1}); err != nil {
			t.Fatal(err)
		}
		if _, ok := store.sessionCache.GetIfPresent(t.Name()); ok {
			t.Fatal("matching conflicted version remained cached")
		}
	})

	t.Run("Newer", func(t *testing.T) {
		q := newScriptedPayloadQuerier()
		started := make(chan *dbgen.UpdateSessionPayloadsParams, 1)
		release := make(chan struct{})
		q.enqueue(payloadWriteResponse{started: started, release: release})
		store := newPayloadWorkerStore(q)
		old := cachePayloadSession(store, t.Name(), session.Authority{State: session.StatePending, Version: 3, ExpiresAt: time.Now().Add(time.Hour)})

		result := make(chan error, 1)
		go func() {
			result <- store.persistPayloads(t.Context(), map[string]uint{t.Name(): 1})
		}()
		waitPayloadWrite(t, started)
		newer := session.NewSessionWithAuthority(
			session.Authority{State: session.StatePending, Version: 4, ExpiresAt: time.Now().Add(time.Hour)},
			old.Payload(),
		)
		store.sessionCache.Compute(t.Name(), func(*session.Session, bool) (*session.Session, otter.ComputeOp) {
			return newer, otter.WriteOp
		})
		close(release)
		if err := <-result; err != nil {
			t.Fatal(err)
		}

		cached, ok := store.sessionCache.GetIfPresent(t.Name())
		if !ok || cached != newer {
			t.Fatal("conflict evicted a newer cached version")
		}
	})
}

func TestSessionPayloadWorkerEvictsSameVersionReplacementAfterWrite(t *testing.T) {
	q := newScriptedPayloadQuerier()
	started := make(chan *dbgen.UpdateSessionPayloadsParams, 1)
	release := make(chan struct{})
	q.enqueue(payloadWriteResponse{
		results: []*dbgen.UpdateSessionPayloadsRow{{SessionID: t.Name(), Version: 4}},
		started: started,
		release: release,
	})
	store := newPayloadWorkerStore(q)
	authority := session.Authority{State: session.StatePending, Version: 3, ExpiresAt: time.Now().Add(time.Hour)}
	cachePayloadSession(store, t.Name(), authority)

	result := make(chan error, 1)
	go func() {
		result <- store.persistPayloads(t.Context(), map[string]uint{t.Name(): 1})
	}()
	waitPayloadWrite(t, started)
	replacementPayload := session.NewPayload(t.Name(), noopPayloadStore{})
	if err := replacementPayload.Set(t.Context(), session.KeyUserName, "reloaded"); err != nil {
		t.Fatal(err)
	}
	replacement := session.NewSessionWithAuthority(authority, replacementPayload)
	store.sessionCache.Compute(t.Name(), func(*session.Session, bool) (*session.Session, otter.ComputeOp) {
		return replacement, otter.WriteOp
	})
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if _, ok := store.sessionCache.GetIfPresent(t.Name()); ok {
		t.Fatal("SQL result was published onto a different Payload")
	}
}

func TestSessionPayloadQueueSaturationDoesNotBlockMutation(t *testing.T) {
	store := newPayloadWorkerStore(newScriptedPayloadQuerier())
	for i := 0; i < cap(store.payloadChan); i++ {
		store.payloadChan <- t.Name()
	}
	payload := session.NewPayload(t.Name(), store)

	done := make(chan error, 1)
	go func() {
		done <- payload.Set(t.Context(), session.KeyUserName, "retained")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Payload mutation blocked on a full queue")
	}
	if payload.Get(session.KeyUserName) != "retained" {
		t.Fatal("queue saturation rolled back Payload mutation")
	}
}

func TestSessionPayloadMutationDuringShutdownIsSafe(t *testing.T) {
	store := newPayloadWorkerStore(newScriptedPayloadQuerier())
	payload := session.NewPayload(t.Name(), store)
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-start
		for i := 0; i < 100; i++ {
			_ = payload.Set(t.Context(), session.KeyUserName, i)
		}
		close(done)
	}()
	close(start)
	store.Shutdown()
	<-done
	if err := payload.Set(t.Context(), session.KeyUserName, "after shutdown"); err != nil {
		t.Fatal(err)
	}
	if payload.Get(session.KeyUserName) != "after shutdown" {
		t.Fatal("mutation after shutdown was not retained locally")
	}
}
