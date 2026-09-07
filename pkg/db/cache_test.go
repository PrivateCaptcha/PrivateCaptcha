package db

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

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

func TestCacheKeyPrefixNumericValuesRemainStable(t *testing.T) {
	fixtures := []struct {
		prefix CacheKeyPrefix
		value  CacheKeyPrefix
	}{
		{retiredSessionCacheKeyPrefix, 12},
		{userAuditLogsCacheKeyPrefix, 13},
		{propertyAuditLogsCacheKeyPrefix, 14},
		{orgAuditLogsCacheKeyPrefix, 15},
		{userPropertiesCountCacheKeyPrefix, 16},
		{userAccountStatsCacheKeyPrefix, 17},
		{propertyStatsCacheKeyPrefix, 18},
		{asyncTaskCacheKeyPrefix, 19},
		{orgPropertiesCountCacheKeyPrefix, 20},
		{orgInviteCacheKeyPrefix, 21},
		{compiledPropertyRulesCacheKeyPrefix, 22},
		{compiledOrgRulesCacheKeyPrefix, 23},
		{rawPropertyRulesCacheKeyPrefix, 24},
		{rawOrgRulesCacheKeyPrefix, 25},
		{difficultyRuleCacheKeyPrefix, 26},
		{propertyRuleStatsCacheKeyPrefix, 27},
		{userSettingsCacheKeyPrefix, 28},
		{orgFormsCacheKeyPrefix, 29},
		{orgFormsCountCacheKeyPrefix, 30},
		{formByExternalIDCacheKeyPrefix, 31},
		{formCacheKeyPrefix, 32},
		{formStatsCacheKeyPrefix, 33},
		{formAuditLogsCacheKeyPrefix, 34},
		{userFormsCountCacheKeyPrefix, 35},
		{orgSearchCacheKeyPrefix, 36},
		{orgStatsCacheKeyPrefix, 37},
		{CACHE_KEY_PREFIXES_COUNT, 38},
	}
	for _, fixture := range fixtures {
		if fixture.prefix != fixture.value {
			t.Fatalf("cache key prefix = %d, want %d", fixture.prefix, fixture.value)
		}
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

	const maxEntries = 100

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

	var buf bytes.Buffer
	if err := store.Cache.SaveTo(ctx, &buf, maxEntries); err != nil {
		t.Fatalf("failed to save business cache: %v", err)
	}

	newStore := NewBusiness(nil)
	if err := newStore.Cache.LoadFrom(ctx, &buf, 24*time.Hour); err != nil {
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
