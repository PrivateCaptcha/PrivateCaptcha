package db

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/maypok86/otter/v2"
)

type manualCacheClock struct {
	now atomic.Int64
}

func (c *manualCacheClock) NowNano() int64 {
	return c.now.Load()
}

func (c *manualCacheClock) Tick(time.Duration) <-chan time.Time {
	return nil
}

func (c *manualCacheClock) Advance(d time.Duration) {
	c.now.Add(int64(d))
}

func TestRegisterCachePrefixString(t *testing.T) {
	if err := RegisterCachePrefixString(CACHE_KEY_PREFIXES_COUNT, "count"); err != nil {
		t.Fatal(err)
	}
}

func TestGetWithRefreshCacheMiss(t *testing.T) {
	cache, err := NewMemoryCacheEx[string, string]("test-miss", 100, "", time.Second,
		func(o *otter.Options[string, string]) {
			o.ExpiryCalculator = otter.ExpiryWriting[string, string](5 * time.Second)
			o.RefreshCalculator = otter.RefreshWriting[string, string](100 * time.Millisecond)
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = cache.GetWithRefresh(context.Background(), "nonexistent")
	if err != ErrCacheMiss {
		t.Fatalf("expected ErrCacheMiss, got %v", err)
	}
}

func TestGetWithRefreshNegativeCacheHit(t *testing.T) {
	cache, err := NewMemoryCacheEx[string, string]("test-negative", 100, "", time.Second,
		func(o *otter.Options[string, string]) {
			o.ExpiryCalculator = otter.ExpiryWriting[string, string](5 * time.Second)
			o.RefreshCalculator = otter.RefreshWriting[string, string](100 * time.Millisecond)
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := cache.SetMissing(context.Background(), "key1"); err != nil {
		t.Fatal(err)
	}

	_, _, err = cache.GetWithRefresh(context.Background(), "key1")
	if err != ErrNegativeCacheHit {
		t.Fatalf("expected ErrNegativeCacheHit, got %v", err)
	}
}

func TestGetWithRefreshFreshEntry(t *testing.T) {
	cache, err := NewMemoryCacheEx[string, string]("test-fresh", 100, "", time.Second,
		func(o *otter.Options[string, string]) {
			o.ExpiryCalculator = otter.ExpiryWriting[string, string](5 * time.Second)
			o.RefreshCalculator = otter.RefreshWriting[string, string](500 * time.Millisecond)
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := cache.Set(ctx, "key1", "value1"); err != nil {
		t.Fatal(err)
	}

	val, needsRefresh, err := cache.GetWithRefresh(ctx, "key1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "value1" {
		t.Fatalf("expected value1, got %s", val)
	}
	if needsRefresh {
		t.Fatal("expected needsRefresh=false for a fresh entry")
	}
}

func TestGetWithRefreshStaleEntry(t *testing.T) {
	refreshTTL := 100 * time.Millisecond

	cache, err := NewMemoryCacheEx[string, string]("test-stale", 100, "", time.Second,
		func(o *otter.Options[string, string]) {
			o.ExpiryCalculator = otter.ExpiryWriting[string, string](5 * time.Second)
			o.RefreshCalculator = otter.RefreshWriting[string, string](refreshTTL)
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := cache.Set(ctx, "key1", "value1"); err != nil {
		t.Fatal(err)
	}

	// wait past refresh time but well before expiry
	time.Sleep(refreshTTL + 50*time.Millisecond)

	val, needsRefresh, err := cache.GetWithRefresh(ctx, "key1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "value1" {
		t.Fatalf("expected value1, got %s", val)
	}
	if !needsRefresh {
		t.Fatal("expected needsRefresh=true for a stale entry")
	}
}

func TestMemoryCacheMissingExpiryIsNotRenewedOnRead(t *testing.T) {
	ctx := context.Background()
	clock := &manualCacheClock{}
	missingValue := "missing"
	missingTTL := time.Minute
	expiryTTL := 10 * time.Minute

	cache, err := NewMemoryCacheEx[string, string]("test-missing-expiry", 100, missingValue, missingTTL,
		func(o *otter.Options[string, string]) {
			o.ExpiryCalculator = newMemoryCacheExpiryCalculator[string, string](expiryTTL, missingTTL, missingValue)
			o.RefreshCalculator = otter.RefreshWriting[string, string](expiryTTL)
			o.Clock = clock
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := cache.Set(ctx, "normal", "value"); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetMissing(ctx, "missing"); err != nil {
		t.Fatal(err)
	}

	clock.Advance(missingTTL / 2)

	if _, err := cache.Get(ctx, "normal"); err != nil {
		t.Fatalf("expected normal cache hit before expiry, got %v", err)
	}
	if _, err := cache.Get(ctx, "missing"); err != ErrNegativeCacheHit {
		t.Fatalf("expected negative cache hit before expiry, got %v", err)
	}

	clock.Advance(missingTTL/2 + time.Nanosecond)

	if _, err := cache.Get(ctx, "normal"); err != nil {
		t.Fatalf("expected normal cache hit after read renewed expiry, got %v", err)
	}
	if _, err := cache.Get(ctx, "missing"); err != ErrCacheMiss {
		t.Fatalf("expected missing cache entry to expire without renewal, got %v", err)
	}
}

func TestMemoryCacheSetIfAbsent(t *testing.T) {
	ctx := context.Background()

	cache, err := NewMemoryCache[string, string](
		"test-set-if-absent",
		100,
		"missing",
		time.Hour,
		time.Hour,
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := cache.SetIfAbsent(ctx, "value", "first"); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetIfAbsent(ctx, "value", "second"); err != nil {
		t.Fatal(err)
	}

	value, err := cache.Get(ctx, "value")
	if err != nil {
		t.Fatal(err)
	}
	if value != "first" {
		t.Fatalf("expected first value to remain, got %q", value)
	}

	if err := cache.SetIfAbsent(ctx, "negative", cache.Missing()); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetIfAbsent(ctx, "negative", "replacement"); err != nil {
		t.Fatal(err)
	}

	if _, err := cache.Get(ctx, "negative"); err != ErrNegativeCacheHit {
		t.Fatalf("expected ErrNegativeCacheHit, got %v", err)
	}
}

func TestBusinessCacheMissingCompiledRulesRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewBusiness(nil)
	impl := store.Impl()

	impl.CacheCompiledPropertyRules(ctx, 123, nil)
	impl.CacheCompiledOrgRules(ctx, 456, nil)

	if _, _, err := impl.GetCachedCompiledPropertyRules(ctx, 123); err != ErrNegativeCacheHit {
		t.Fatalf("expected negative cache hit for property rules before save, got %v", err)
	}
	if _, _, err := impl.GetCachedCompiledOrgRules(ctx, 456); err != ErrNegativeCacheHit {
		t.Fatalf("expected negative cache hit for org rules before save, got %v", err)
	}

	snapshotDir := t.TempDir()
	if err := store.SaveCache(ctx, snapshotDir); err != nil {
		t.Fatalf("failed to save business cache: %v", err)
	}

	newStore := NewBusiness(nil)
	if err := newStore.LoadCache(ctx, snapshotDir); err != nil {
		t.Fatalf("failed to load business cache: %v", err)
	}

	newImpl := newStore.Impl()
	if _, _, err := newImpl.GetCachedCompiledPropertyRules(ctx, 123); err != ErrNegativeCacheHit {
		t.Fatalf("expected negative cache hit for property rules after load, got %v", err)
	}
	if _, _, err := newImpl.GetCachedCompiledOrgRules(ctx, 456); err != ErrNegativeCacheHit {
		t.Fatalf("expected negative cache hit for org rules after load, got %v", err)
	}
}

func TestBusinessCachePersistenceExcludesPortalSessions(t *testing.T) {
	ctx := t.Context()
	const sid = "disk-persistence-session"
	normalKey := APIKeyCacheKey("disk-persistence-value")
	sessionKey := SessionCacheKey(sid)
	source := NewBusiness(nil)
	if err := source.Cache.SetEx(ctx, normalKey, "normal-value", 2*time.Hour, time.Hour); err != nil {
		t.Fatal(err)
	}
	sessionData := session.NewSessionData(sid)
	if err := source.Impl().CacheUserSession(ctx, sessionData); err != nil {
		t.Fatal(err)
	}

	snapshotDir := t.TempDir()
	if err := source.SaveCache(ctx, snapshotDir); err != nil {
		t.Fatal(err)
	}
	if value, err := source.Cache.Get(ctx, sessionKey); err != nil || value != sessionData {
		t.Fatalf("saving removed the live in-memory session: value=%v err=%v", value, err)
	}

	restoredStore := NewBusiness(nil)
	restoredCache := restoredStore.Cache.(*memcache[CacheKey, any])
	if err := common.LoadCacheFromFile(ctx, snapshotDir, cachePersistFile, DefaultCacheTTL, restoredCache.store); err != nil {
		t.Fatal(err)
	}
	if value, err := restoredStore.Cache.Get(ctx, normalKey); err != nil || value != "normal-value" {
		t.Fatalf("non-session cache entry did not round-trip: value=%v err=%v", value, err)
	}
	if _, err := restoredStore.Cache.Get(ctx, sessionKey); err != ErrCacheMiss {
		t.Fatalf("session was serialized in the snapshot: %v", err)
	}

	legacySessionValueKey := APIKeyCacheKey("legacy-session-value")
	if err := source.Cache.Set(ctx, sessionKey, "legacy-session-key"); err != nil {
		t.Fatal(err)
	}
	if err := source.Cache.Set(ctx, legacySessionValueKey, sessionData); err != nil {
		t.Fatal(err)
	}
	legacySnapshotDir := t.TempDir()
	sourceCache := source.Cache.(*memcache[CacheKey, any])
	if err := common.SaveCacheToFile(ctx, legacySnapshotDir, cachePersistFile, cachePersistSize, sourceCache.store, nil); err != nil {
		t.Fatal(err)
	}
	sourceEntry, ok := sourceCache.store.GetEntryQuietly(normalKey)
	if !ok {
		t.Fatal("source cache entry is missing")
	}
	legacyRestoredStore := NewBusiness(nil)
	preexistingKey := APIKeyCacheKey("preexisting-value")
	if err := legacyRestoredStore.Cache.Set(ctx, preexistingKey, "preexisting"); err != nil {
		t.Fatal(err)
	}
	if err := legacyRestoredStore.LoadCache(ctx, legacySnapshotDir); err != nil {
		t.Fatal(err)
	}
	legacyRestoredCache := legacyRestoredStore.Cache.(*memcache[CacheKey, any])
	restoredEntry, ok := legacyRestoredCache.store.GetEntryQuietly(normalKey)
	if !ok || restoredEntry.Value != "normal-value" {
		t.Fatalf("legacy snapshot lost non-session entry: value=%v found=%t", restoredEntry.Value, ok)
	}
	if _, err := legacyRestoredStore.Cache.Get(ctx, sessionKey); err != ErrCacheMiss {
		t.Fatalf("legacy snapshot restored a session-prefixed entry: %v", err)
	}
	if _, err := legacyRestoredStore.Cache.Get(ctx, legacySessionValueKey); err != ErrCacheMiss {
		t.Fatalf("legacy snapshot restored portal session data under a non-session key: %v", err)
	}
	if value, err := legacyRestoredStore.Cache.Get(ctx, preexistingKey); err != nil || value != "preexisting" {
		t.Fatalf("legacy snapshot replaced a preexisting entry: value=%v err=%v", value, err)
	}

	if delta := restoredEntry.ExpiresAt().Sub(sourceEntry.ExpiresAt()).Abs(); delta > time.Second {
		t.Fatalf("expiration deadline changed by %v", delta)
	}
	if delta := restoredEntry.RefreshableAt().Sub(sourceEntry.RefreshableAt()).Abs(); delta > time.Second {
		t.Fatalf("refresh deadline changed by %v", delta)
	}
}

func TestBusinessCacheCorruptLegacySnapshotDoesNotRestorePortalSessions(t *testing.T) {
	ctx := t.Context()
	const sid = "corrupt-disk-persistence-session"
	sessionKey := SessionCacheKey(sid)
	source := NewBusiness(nil)
	if err := source.Impl().CacheUserSession(ctx, session.NewSessionData(sid)); err != nil {
		t.Fatal(err)
	}
	snapshotOnlyKey := APIKeyCacheKey("snapshot-only-value")
	collidingKey := APIKeyCacheKey("colliding-value")
	if err := source.Cache.Set(ctx, snapshotOnlyKey, "snapshot-only"); err != nil {
		t.Fatal(err)
	}
	if err := source.Cache.Set(ctx, collidingKey, "snapshot-value"); err != nil {
		t.Fatal(err)
	}
	snapshotDir := t.TempDir()
	sourceCache := source.Cache.(*memcache[CacheKey, any])
	if err := common.SaveCacheToFile(ctx, snapshotDir, cachePersistFile, cachePersistSize, sourceCache.store, nil); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(snapshotDir, cachePersistFile), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{1, 2, 3}); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	restored := NewBusiness(nil)
	if err := restored.Cache.Set(ctx, collidingKey, "live-value"); err != nil {
		t.Fatal(err)
	}
	if err := restored.LoadCache(ctx, snapshotDir); err == nil {
		t.Fatal("corrupt legacy snapshot was accepted")
	}
	if _, err := restored.Cache.Get(ctx, sessionKey); err != ErrCacheMiss {
		t.Fatalf("corrupt legacy snapshot restored a portal session: %v", err)
	}
	if _, err := restored.Cache.Get(ctx, snapshotOnlyKey); err != ErrCacheMiss {
		t.Fatalf("corrupt snapshot partially restored a non-session entry: %v", err)
	}
	if value, err := restored.Cache.Get(ctx, collidingKey); err != nil || value != "live-value" {
		t.Fatalf("failed snapshot load changed live cache: value=%v err=%v", value, err)
	}
}
