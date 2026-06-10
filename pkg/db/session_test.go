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
	createCacheManyCalls   int
	deleteCachedByKeyCalls int
}

func (q *countingSessionQuerier) CreateCacheMany(ctx context.Context, arg *dbgen.CreateCacheManyParams) (int64, error) {
	q.createCacheManyCalls++
	return 0, nil
}

func (q *countingSessionQuerier) DeleteCachedByKey(ctx context.Context, key string) (int64, error) {
	q.deleteCachedByKeyCalls++
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
	if querier.deleteCachedByKeyCalls != 0 {
		t.Fatalf("Renew called DeleteCachedByKey inline %d time(s)", querier.deleteCachedByKeyCalls)
	}

	if _, err := store.Read(ctx, newSess.ID(), false); err != nil {
		t.Fatalf("renewed session was not cached locally: %v", err)
	}
	if _, err := cache.Get(ctx, SessionCacheKey(oldSess.ID())); err != ErrCacheMiss {
		t.Fatalf("old session was not evicted locally: %v", err)
	}

	select {
	case sid := <-store.persistChan:
		if sid != newSess.ID() {
			t.Fatalf("queued persist SID %q, want %q", sid, newSess.ID())
		}
	case <-time.After(time.Second):
		t.Fatal("renewed session was not queued for async persistence")
	}

	select {
	case sid := <-store.deleteChan:
		if sid != oldSess.ID() {
			t.Fatalf("queued delete SID %q, want %q", sid, oldSess.ID())
		}
	case <-time.After(time.Second):
		t.Fatal("old session was not queued for async deletion")
	}
}
