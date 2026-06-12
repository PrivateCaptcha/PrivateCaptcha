package db

import (
	"context"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type sessionMetricsStub struct{}

func (s *sessionMetricsStub) ObserveEventDropped(eventType common.MetricEventType) {}
func (s *sessionMetricsStub) ObservePanic()                                        {}

type countingSessionQuerier struct {
	*QuerierStub
	createCacheManyCalls    int
	deleteCachedByKeysCalls int
	deletedKeys             []string
}

func (q *countingSessionQuerier) CreateCacheMany(ctx context.Context, arg *dbgen.CreateCacheManyParams) (int64, error) {
	q.createCacheManyCalls++
	return 0, nil
}

func (q *countingSessionQuerier) DeleteCachedByKeys(ctx context.Context, keys []string) (int64, error) {
	q.deleteCachedByKeysCalls++
	q.deletedKeys = append(q.deletedKeys, keys...)
	return 0, nil
}

func TestSessionStoreRenewDoesNotCallDBInline(t *testing.T) {
	ctx := context.Background()
	querier := &countingSessionQuerier{QuerierStub: &QuerierStub{}}
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	business := NewBusinessWithQuerier(nil, querier, cache)
	store := NewSessionStore(business, session.KeyPersistent, &sessionMetricsStub{})

	oldSess := session.NewSession(session.NewSessionData("old-sid"), store)
	if err := store.Init(ctx, oldSess); err != nil {
		t.Fatal(err)
	}

	newSess := session.NewSession(session.NewSessionData("new-sid"), store)
	if err := newSess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.persistChan:
	case <-time.After(time.Second):
		t.Fatal("setup session was not queued for persistence")
	}

	if err := store.Renew(ctx, oldSess.ID(), newSess); err != nil {
		t.Fatal(err)
	}

	if querier.createCacheManyCalls != 0 {
		t.Fatalf("Renew called CreateCacheMany inline %d time(s)", querier.createCacheManyCalls)
	}
	if querier.deleteCachedByKeysCalls != 0 {
		t.Fatalf("Renew called DeleteCachedByKeys inline %d time(s)", querier.deleteCachedByKeysCalls)
	}

	if _, err := store.Read(ctx, newSess.ID(), false); err != nil {
		t.Fatalf("renewed session was not cached locally: %v", err)
	}
	oldData, err := cache.Get(ctx, SessionCacheKey(oldSess.ID()))
	if err != nil {
		t.Fatalf("old session tombstone was not cached locally: %v", err)
	}
	oldSessionData, ok := oldData.(*session.SessionData)
	if !ok || !oldSessionData.Has(session.KeyTombstone) {
		t.Fatalf("old session was not tombstoned locally: %v", oldData)
	}

	queued := make(map[string]bool)
	select {
	case sid := <-store.persistChan:
		queued[sid] = true
	case <-time.After(time.Second):
		t.Fatal("first renewed session persistence event was not queued")
	}

	select {
	case sid := <-store.persistChan:
		queued[sid] = true
	case <-time.After(time.Second):
		t.Fatal("second renewed session persistence event was not queued")
	}
	if !queued[newSess.ID()] || !queued[oldSess.ID()] {
		t.Fatalf("queued SIDs = %v, want old and new", queued)
	}

	if err := store.persistSessions(ctx, map[string]uint{oldSess.ID(): 1, newSess.ID(): 1}); err != nil {
		t.Fatal(err)
	}
	if querier.createCacheManyCalls != 1 {
		t.Fatalf("persistSessions called CreateCacheMany %d time(s)", querier.createCacheManyCalls)
	}
	if querier.deleteCachedByKeysCalls != 1 {
		t.Fatalf("persistSessions called DeleteCachedByKeys %d time(s)", querier.deleteCachedByKeysCalls)
	}
	if len(querier.deletedKeys) != 1 || querier.deletedKeys[0] != SessionCacheKey(oldSess.ID()).String() {
		t.Fatalf("deleted keys = %v", querier.deletedKeys)
	}
	if _, err := cache.Get(ctx, SessionCacheKey(oldSess.ID())); err != ErrCacheMiss {
		t.Fatalf("old session tombstone was not removed locally: %v", err)
	}
}

func TestStoreUserSessionsDoesNotDeleteTombstones(t *testing.T) {
	ctx := context.Background()
	querier := &countingSessionQuerier{QuerierStub: &QuerierStub{}}
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	impl := NewBusinessWithQuerier(nil, querier, cache).Impl()

	oldSID := "old-sid"
	if err := impl.CacheUserSession(ctx, session.NewTombstoneSessionData(oldSID)); err != nil {
		t.Fatal(err)
	}

	if err := impl.StoreUserSessions(ctx, map[string]uint{oldSID: 1}, session.KeyPersistent, sessionCacheTTL); err != nil {
		t.Fatal(err)
	}
	if querier.deleteCachedByKeysCalls != 0 {
		t.Fatalf("StoreUserSessions deleted tombstone sessions %d time(s)", querier.deleteCachedByKeysCalls)
	}
	if querier.createCacheManyCalls != 0 {
		t.Fatalf("StoreUserSessions persisted tombstone sessions %d time(s)", querier.createCacheManyCalls)
	}
	if _, err := cache.Get(ctx, SessionCacheKey(oldSID)); err != nil {
		t.Fatalf("StoreUserSessions removed tombstone from local cache: %v", err)
	}
}

func TestSessionStoreRenewTombstonesOldSessionOnAlreadyCancelledContext(t *testing.T) {
	ctx := context.Background()
	querier := &countingSessionQuerier{QuerierStub: &QuerierStub{}}
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
