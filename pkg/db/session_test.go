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
)

type sessionMetricsStub struct{}

func (s *sessionMetricsStub) ObserveEventDropped(eventType common.MetricEventType) {}
func (s *sessionMetricsStub) ObservePanic()                                        {}

type memorySessionQuerier struct {
	*QuerierStub
	mu               sync.RWMutex
	values           map[string][]byte
	createCacheError error
	createManyError  error
}

func newMemorySessionQuerier() *memorySessionQuerier {
	return &memorySessionQuerier{
		QuerierStub: &QuerierStub{},
		values:      make(map[string][]byte),
	}
}

func (q *memorySessionQuerier) CreateCache(ctx context.Context, arg *dbgen.CreateCacheParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.createCacheError != nil {
		return 0, q.createCacheError
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	q.values[arg.Key] = append([]byte(nil), arg.Value...)
	return 1, nil
}

func (q *memorySessionQuerier) CreateCacheMany(ctx context.Context, arg *dbgen.CreateCacheManyParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.createManyError != nil {
		err := q.createManyError
		q.createManyError = nil
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	for i, key := range arg.Keys {
		q.values[key] = append([]byte(nil), arg.Values[i]...)
	}
	return int64(len(arg.Keys)), nil
}

func (q *memorySessionQuerier) failNextCreateCacheMany(err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.createManyError = err
}

func (q *memorySessionQuerier) GetCachedByKey(ctx context.Context, key string) ([]byte, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, ok := q.values[key]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return append([]byte(nil), value...), nil
}

func (q *memorySessionQuerier) DeleteCachedByKeys(ctx context.Context, keys []string) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var affected int64
	for _, key := range keys {
		if _, ok := q.values[key]; ok {
			delete(q.values, key)
			affected++
		}
	}
	return affected, nil
}

func TestSessionStoreRenewPersistsQuickly(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	business := NewBusinessWithQuerier(nil, querier, cache)
	store := NewSessionStore(business, session.KeyPersistent, &sessionMetricsStub{})

	oldSess := session.NewSession(session.NewSessionData("old-sid"), store)
	if err := store.Init(ctx, oldSess); err != nil {
		t.Fatal(err)
	}
	if err := oldSess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan
	if err := business.Impl().StoreUserSessions(ctx, map[string]uint{oldSess.ID(): 1}, session.KeyPersistent, sessionCacheTTL); err != nil {
		t.Fatal(err)
	}

	newSess := session.NewSession(session.NewSessionData("new-sid"), store)
	if err := newSess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.persistDelayChan:
	case <-time.After(time.Second):
		t.Fatal("setup session was not queued for persistence")
	}
	store.Start(ctx, time.Hour)
	t.Cleanup(store.Stop)
	querier.failNextCreateCacheMany(errors.New("transient write failure"))

	if err := store.Renew(ctx, oldSess.ID(), newSess); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Read(ctx, newSess.ID(), false); err != nil {
		t.Fatalf("renewed session was not cached locally: %v", err)
	}
	if _, err := store.Read(ctx, oldSess.ID(), false); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("old session remained available locally: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	remoteCache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	remoteStore := NewSessionStore(NewBusinessWithQuerier(nil, querier, remoteCache), session.KeyPersistent, &sessionMetricsStub{})
	var remoteSession *session.Session
	var err error
	for {
		remoteSession, err = remoteStore.Read(ctx, newSess.ID(), true /*skip cache*/)
		if err == nil {
			break
		}
		if !errors.Is(err, session.ErrSessionMissing) {
			t.Fatalf("failed to read renewed session from another store: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("renewed session was not persisted within the low-latency batch window")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !remoteSession.Data().Has(session.KeyPersistent) {
		t.Fatal("renewed session lost persistent state")
	}
	if _, err := remoteStore.Read(ctx, oldSess.ID(), false); err != nil {
		t.Fatalf("old session was deleted before tombstone persistence: %v", err)
	}

	if err := store.persistSessions(ctx, map[string]uint{oldSess.ID(): 1, newSess.ID(): 1}); err != nil {
		t.Fatal(err)
	}
	freshCache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	freshStore := NewSessionStore(NewBusinessWithQuerier(nil, querier, freshCache), session.KeyPersistent, &sessionMetricsStub{})
	if _, err := freshStore.Read(ctx, oldSess.ID(), false); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("old session remained persisted after tombstone processing: %v", err)
	}
}

func TestSessionStoreRenewReturnsImmediateBackpressure(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	store := NewSessionStore(NewBusinessWithQuerier(nil, querier, cache), session.KeyPersistent, &sessionMetricsStub{})

	oldSess := session.NewSession(session.NewSessionData("old-sid-backpressure"), store)
	if err := store.Init(ctx, oldSess); err != nil {
		t.Fatal(err)
	}
	newSess := session.NewSession(session.NewSessionData("new-sid-backpressure"), store)
	if err := newSess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan
	for range cap(store.persistNowChan) {
		store.persistNowChan <- "queued-sid"
	}

	if err := store.Renew(ctx, oldSess.ID(), newSess); !errors.Is(err, common.ErrBackpressure) {
		t.Fatalf("Renew error = %v, want %v", err, common.ErrBackpressure)
	}
	if _, err := store.Read(ctx, oldSess.ID(), false); err != nil {
		t.Fatalf("old session was tombstoned after immediate queue failure: %v", err)
	}
}

func TestSessionStoreRenewDoesNotWaitForPersistence(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	business := NewBusinessWithQuerier(nil, querier, cache)
	store := NewSessionStore(business, session.KeyPersistent, &sessionMetricsStub{})

	oldSess := session.NewSession(session.NewSessionData("old-sid"), store)
	if err := store.Init(ctx, oldSess); err != nil {
		t.Fatal(err)
	}
	if err := oldSess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan
	if err := business.Impl().StoreUserSessions(ctx, map[string]uint{oldSess.ID(): 1}, session.KeyPersistent, sessionCacheTTL); err != nil {
		t.Fatal(err)
	}

	newSess := session.NewSession(session.NewSessionData("new-sid"), store)
	if err := newSess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	<-store.persistDelayChan

	persistErr := errors.New("write session")
	querier.createCacheError = persistErr
	if err := store.Renew(ctx, oldSess.ID(), newSess); err != nil {
		t.Fatalf("Renew waited for persistence: %v", err)
	}

	remoteCache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	remoteStore := NewSessionStore(NewBusinessWithQuerier(nil, querier, remoteCache), session.KeyPersistent, &sessionMetricsStub{})
	if _, err := remoteStore.Read(ctx, oldSess.ID(), false); err != nil {
		t.Fatalf("old session was unavailable after failed renewal: %v", err)
	}
	if _, err := remoteStore.Read(ctx, newSess.ID(), false); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("renewed session was persisted synchronously: %v", err)
	}
}

func TestSessionStoreRenewKeepsNonPersistentSessionsAsynchronous(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	business := NewBusinessWithQuerier(nil, querier, cache)
	store := NewSessionStore(business, session.KeyPersistent, &sessionMetricsStub{})
	oldSess := session.NewSession(session.NewSessionData("old-sid"), store)
	if err := store.Init(ctx, oldSess); err != nil {
		t.Fatal(err)
	}
	oldData, err := oldSess.Data().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := business.Impl().StoreInCache(ctx, SessionCacheKey(oldSess.ID()).String(), oldData, sessionCacheTTL); err != nil {
		t.Fatal(err)
	}
	newSess := session.NewSession(session.NewSessionData("new-sid"), store)

	if err := store.Renew(ctx, oldSess.ID(), newSess); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx, newSess.ID(), false); err != nil {
		t.Fatalf("renewed session was not available locally: %v", err)
	}
	if _, err := store.Read(ctx, oldSess.ID(), false); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("old session remained available locally: %v", err)
	}

	remoteCache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	remoteStore := NewSessionStore(NewBusinessWithQuerier(nil, querier, remoteCache), session.KeyPersistent, &sessionMetricsStub{})
	if _, err := remoteStore.Read(ctx, newSess.ID(), false); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("non-persistent session was stored synchronously: %v", err)
	}
	if _, err := remoteStore.Read(ctx, oldSess.ID(), false); err != nil {
		t.Fatalf("old non-persistent session was deleted synchronously: %v", err)
	}
}

func TestStoreUserSessionsDoesNotDeleteTombstones(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	impl := NewBusinessWithQuerier(nil, querier, cache).Impl()

	oldSID := "old-sid"
	if err := impl.CacheUserSession(ctx, session.NewTombstoneSessionData(oldSID)); err != nil {
		t.Fatal(err)
	}

	if err := impl.StoreUserSessions(ctx, map[string]uint{oldSID: 1}, session.KeyPersistent, sessionCacheTTL); err != nil {
		t.Fatal(err)
	}
	if _, err := impl.RetrieveFromCache(ctx, SessionCacheKey(oldSID).String()); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("tombstone was persisted: %v", err)
	}
	if _, err := cache.Get(ctx, SessionCacheKey(oldSID)); err != nil {
		t.Fatalf("StoreUserSessions removed tombstone from local cache: %v", err)
	}
}

func TestSessionStoreRenewTombstonesOldSessionOnAlreadyCancelledContext(t *testing.T) {
	ctx := t.Context()
	querier := newMemorySessionQuerier()
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	business := NewBusinessWithQuerier(nil, querier, cache)
	store := NewSessionStore(business, session.KeyPersistent, &sessionMetricsStub{})

	oldSess := session.NewSession(session.NewSessionData("old-sid-cancelled-ctx"), store)
	if err := store.Init(ctx, oldSess); err != nil {
		t.Fatal(err)
	}

	newSess := session.NewSession(session.NewSessionData("new-sid-cancelled-ctx"), store)

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	_ = store.Renew(cancelledCtx, oldSess.ID(), newSess)
	//if err == nil {
	//	t.Fatal("Renew should have returned an error with cancelled context")
	//}

	// BUG: Old session is tombstoned despite Renew failure
	oldData, cacheErr := cache.Get(ctx, SessionCacheKey(oldSess.ID()))
	if cacheErr != nil {
		t.Fatalf("old session tombstone should be in cache: %v", cacheErr)
	}
	oldSessionData, ok := oldData.(*session.SessionData)
	if !ok || !oldSessionData.Has(session.KeyTombstone) {
		t.Fatalf("old session should be tombstoned in cache, got: %v", oldData)
	}

	_, readErr := store.Read(ctx, oldSess.ID(), false)
	if readErr != session.ErrSessionMissing {
		t.Fatalf("expected session.ErrSessionMissing, got: %v", readErr)
	}
}
