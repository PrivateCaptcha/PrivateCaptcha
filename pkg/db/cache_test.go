package db

import (
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
