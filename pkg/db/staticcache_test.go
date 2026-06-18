package db

import (
	"context"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestStaticCacheNewCache(t *testing.T) {
	cache := NewStaticCache[string, int](100, -1)

	if cache.Missing() != -1 {
		t.Errorf("Expected missing value to be -1, got %d", cache.Missing())
	}

	if cache.HitRatio() != 0.0 {
		t.Errorf("Expected hit ratio to be 0.0, got %f", cache.HitRatio())
	}
}

func TestStaticCacheSetAndGet(t *testing.T) {
	ctx := context.Background()
	cache := NewStaticCache[string, int](100, -1)

	// Set a value
	err := cache.Set(ctx, "key1", 42)
	if err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	// Get the value
	val, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}
	if val != 42 {
		t.Errorf("Expected value 42, got %d", val)
	}
}

func TestStaticCacheGetMiss(t *testing.T) {
	ctx := context.Background()
	cache := NewStaticCache[string, int](100, -1)

	// Try to get a non-existent key
	_, err := cache.Get(ctx, "nonexistent")
	if err != ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss, got %v", err)
	}
}

func TestStaticCacheSetMissing(t *testing.T) {
	ctx := context.Background()
	cache := NewStaticCache[string, int](100, -1)

	// Set a key as missing
	err := cache.SetMissing(ctx, "missing_key")
	if err != nil {
		t.Fatalf("Failed to set missing: %v", err)
	}

	// Try to get the missing key
	_, err = cache.Get(ctx, "missing_key")
	if err != ErrNegativeCacheHit {
		t.Errorf("Expected ErrNegativeCacheHit, got %v", err)
	}
}

func TestStaticCacheDelete(t *testing.T) {
	ctx := context.Background()
	cache := NewStaticCache[string, int](100, -1)

	// Set and then delete
	_ = cache.Set(ctx, "to_delete", 100)

	found := cache.Delete(ctx, "to_delete")
	if !found {
		t.Error("Expected Delete to return true for existing key")
	}

	// Verify deletion
	_, err := cache.Get(ctx, "to_delete")
	if err != ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss after delete, got %v", err)
	}

	// Delete non-existent key
	found = cache.Delete(ctx, "nonexistent")
	if found {
		t.Error("Expected Delete to return false for non-existent key")
	}
}

func TestStaticCacheSetEx(t *testing.T) {
	ctx := context.Background()
	cache := NewStaticCache[string, int](100, -1)

	// SetEx should work like Set (TTL is ignored in static cache)
	err := cache.SetEx(ctx, "ttl_key", 200, 1*time.Hour, 10*time.Minute)
	if err != nil {
		t.Fatalf("Failed to SetEx: %v", err)
	}

	val, err := cache.Get(ctx, "ttl_key")
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}
	if val != 200 {
		t.Errorf("Expected value 200, got %d", val)
	}
}

func TestStaticCacheSetTTL(t *testing.T) {
	ctx := context.Background()
	cache := NewStaticCache[string, int](100, -1)

	// SetTTL should return ErrInvalidInput (not supported)
	err := cache.SetTTL(ctx, "key1", 1*time.Hour)
	if err != ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput from SetTTL, got %v", err)
	}
}

func TestStaticCacheSetRefresh(t *testing.T) {
	ctx := context.Background()
	cache := NewStaticCache[string, int](100, -1)

	// SetRefresh should return ErrInvalidInput (not supported)
	err := cache.SetRefresh(ctx, "key1", 1*time.Hour)
	if err != ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput from SetRefresh, got %v", err)
	}
}

func TestStaticCacheCompression(t *testing.T) {
	ctx := context.Background()
	capacity := 10
	cache := NewStaticCache[int, int](capacity, -1)

	// Fill the cache beyond capacity to trigger compression
	for i := 0; i < capacity+5; i++ {
		_ = cache.Set(ctx, i, i*10)
	}

	// After compression, the cache should have approximately lowerBound entries
	// lowerBound = capacity/2 + capacity/4 = 5 + 2 = 7
	// Plus some items added after compression
	lowerBound := capacity/2 + capacity/4

	actualCount := 0
	for i := 0; i < capacity+5; i++ {
		if _, err := cache.Get(ctx, i); err == nil {
			actualCount++
		}
	}

	// Just verify compression happened - the cache should have less than capacity+5 items
	// and ideally around lowerBound + a few more
	if actualCount > capacity {
		t.Errorf("Cache should have compressed, expected at most %d items, got %d", capacity, actualCount)
	}

	if actualCount < lowerBound {
		t.Errorf("Cache should have at least %d items after compression, got %d", lowerBound, actualCount)
	}
}

type stubCacheLoader struct {
	loadCount int
	value     int
}

func (l *stubCacheLoader) Load(ctx context.Context, key string) (int, error) {
	l.loadCount++
	return l.value, nil
}

func (l *stubCacheLoader) Reload(ctx context.Context, key string, oldValue int) (int, error) {
	l.loadCount++
	return l.value, nil
}

var _ common.CacheLoader[string, int] = (*stubCacheLoader)(nil)

func TestStaticCacheGetEx(t *testing.T) {
	ctx := context.Background()
	cache := NewStaticCache[string, int](100, -1)

	loader := &stubCacheLoader{value: 999}

	// First call should trigger the loader
	val, err := cache.GetEx(ctx, "new_key", loader)
	if err != nil {
		t.Fatalf("GetEx failed: %v", err)
	}
	if val != 999 {
		t.Errorf("Expected value 999, got %d", val)
	}
	if loader.loadCount != 1 {
		t.Errorf("Expected loader to be called once, called %d times", loader.loadCount)
	}

	// Second call should use cached value (loader not called again)
	val, err = cache.GetEx(ctx, "new_key", loader)
	if err != nil {
		t.Fatalf("GetEx failed on second call: %v", err)
	}
	if val != 999 {
		t.Errorf("Expected value 999, got %d", val)
	}
	if loader.loadCount != 1 {
		t.Errorf("Expected loader to still be called once, called %d times", loader.loadCount)
	}
}

func TestStaticCacheGetExMissingValue(t *testing.T) {
	ctx := context.Background()
	missingValue := -1
	cache := NewStaticCache[string, int](100, missingValue)

	// Loader that returns the missing value
	loader := &stubCacheLoader{value: missingValue}

	// When loader returns missing value, GetEx should return ErrNegativeCacheHit
	_, err := cache.GetEx(ctx, "will_be_missing", loader)
	if err != ErrNegativeCacheHit {
		t.Errorf("Expected ErrNegativeCacheHit when loader returns missing value, got %v", err)
	}
}
