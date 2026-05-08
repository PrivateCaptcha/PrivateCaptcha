package db

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/maypok86/otter/v2"
)

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
