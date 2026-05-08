//go:build enterprise

package db

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type testCacheEntity struct {
	ID int32
}

type readerTestCache struct {
	values       map[CacheKey]any
	deleted      []CacheKey
	refresh      map[CacheKey]bool
	missingValue *CacheMissingValue
}

func newReaderTestCache() *readerTestCache {
	return &readerTestCache{
		values:       make(map[CacheKey]any),
		refresh:      make(map[CacheKey]bool),
		missingValue: &CacheMissingValue{},
	}
}

func (c *readerTestCache) HitRatio() float64 { return 0 }

func (c *readerTestCache) SaveTo(context.Context, io.Writer, int) error { return nil }

func (c *readerTestCache) LoadFrom(context.Context, io.Reader, time.Duration) error { return nil }

func (c *readerTestCache) Missing() any { return c.missingValue }

func (c *readerTestCache) isMissingValue(value any) bool {
	_, ok := value.(*CacheMissingValue)
	return ok
}

func (c *readerTestCache) Get(ctx context.Context, key CacheKey) (any, error) {
	if value, ok := c.values[key]; ok {
		if c.isMissingValue(value) {
			return c.missingValue, ErrNegativeCacheHit
		}

		return value, nil
	}

	return nil, ErrCacheMiss
}

func (c *readerTestCache) GetEx(ctx context.Context, key CacheKey, loader common.CacheLoader[CacheKey, any]) (any, error) {
	if value, err := c.Get(ctx, key); err == nil || err == ErrNegativeCacheHit {
		return value, err
	}

	value, err := loader.Load(ctx, key)
	if err != nil {
		return nil, err
	}

	c.values[key] = value
	if c.isMissingValue(value) {
		return value, ErrNegativeCacheHit
	}

	return value, nil
}

func (c *readerTestCache) GetWithRefresh(ctx context.Context, key CacheKey) (any, bool, error) {
	value, err := c.Get(ctx, key)
	if err != nil {
		return nil, false, err
	}

	return value, c.refresh[key], nil
}

func (c *readerTestCache) SetMissing(ctx context.Context, key CacheKey) error {
	c.values[key] = c.missingValue
	return nil
}

func (c *readerTestCache) Set(ctx context.Context, key CacheKey, value any) error {
	c.values[key] = value
	return nil
}

func (c *readerTestCache) SetWithTTL(ctx context.Context, key CacheKey, value any, ttl time.Duration) error {
	return c.Set(ctx, key, value)
}

func (c *readerTestCache) SetTTL(context.Context, CacheKey, time.Duration) error { return nil }

func (c *readerTestCache) Delete(ctx context.Context, key CacheKey) bool {
	delete(c.values, key)
	c.deleted = append(c.deleted, key)
	return true
}

func TestContainsInvalidNameChars(t *testing.T) {
	const orgPunct = "'-_&.:()[]"
	const propPunct = "'-_.:()[]"

	tests := []struct {
		name             string
		input            string
		allowedPunct     string
		expectedPosition int
		expectedRune     rune
	}{
		{
			name:             "ValidLettersOnly",
			input:            "HelloWorld",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "ValidWithDigits",
			input:            "Test123",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "ValidWithSpaces",
			input:            "Hello World",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "ValidOrgPunctuation",
			input:            "O'Reilly & Sons",
			allowedPunct:     orgPunct,
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "InvalidAtSign",
			input:            "Test@Name",
			allowedPunct:     "",
			expectedPosition: 4,
			expectedRune:     '@',
		},
		{
			name:             "AmpersandInvalidForProperty",
			input:            "Test&Name",
			allowedPunct:     propPunct,
			expectedPosition: 4,
			expectedRune:     '&',
		},
		{
			name:             "AmpersandValidForOrg",
			input:            "Test&Name",
			allowedPunct:     orgPunct,
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "EmptyString",
			input:            "",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "UnicodeLetters",
			input:            "Caf\u00e9",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, r := containsInvalidNameChars(tt.input, tt.allowedPunct)
			if pos != tt.expectedPosition {
				t.Errorf("position = %d, want %d", pos, tt.expectedPosition)
			}
			if r != tt.expectedRune {
				t.Errorf("rune = %q, want %q", r, tt.expectedRune)
			}
		})
	}
}

func TestStoreOneReaderDropsInvalidCacheItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cacheKey := UserCacheKey(123)
	cache := newReaderTestCache()
	if err := cache.Set(ctx, cacheKey, "invalid"); err != nil {
		t.Fatal(err)
	}

	queryCalls := 0
	reader := &StoreOneReader[int32, testCacheEntity]{
		CacheKey: cacheKey,
		QueryFunc: func(ctx context.Context, key int32) (*testCacheEntity, error) {
			queryCalls++
			return &testCacheEntity{ID: key}, nil
		},
		QueryKeyFunc: func(key CacheKey) (int32, error) {
			return key.IntValue, nil
		},
		Cache:       cache,
		DropInvalid: true,
	}

	if item, err := reader.Read(ctx); err != errInvalidCacheType {
		t.Fatalf("expected %v, got item=%v err=%v", errInvalidCacheType, item, err)
	}

	if _, err := cache.Get(ctx, cacheKey); err != ErrCacheMiss {
		t.Fatalf("expected invalid cache item to be deleted, got %v", err)
	}

	item, err := reader.Read(ctx)
	if err != nil {
		t.Fatalf("unexpected error on reread: %v", err)
	}
	if item == nil || item.ID != cacheKey.IntValue {
		t.Fatalf("unexpected item: %#v", item)
	}
	if queryCalls != 1 {
		t.Fatalf("expected query to run once after invalid item removal, got %d", queryCalls)
	}
}

func TestStoreArrayReaderDropsInvalidCacheItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cacheKey := UserOrgsCacheKey(456)
	cache := newReaderTestCache()
	if err := cache.Set(ctx, cacheKey, "invalid"); err != nil {
		t.Fatal(err)
	}

	queryCalls := 0
	reader := &StoreArrayReader[int32, testCacheEntity]{
		CacheKey: cacheKey,
		QueryFunc: func(ctx context.Context, key int32) ([]*testCacheEntity, error) {
			queryCalls++
			return []*testCacheEntity{{ID: key}}, nil
		},
		QueryKeyFunc: func(key CacheKey) (int32, error) {
			return key.IntValue, nil
		},
		Cache:       cache,
		DropInvalid: true,
	}

	if items, err := reader.Read(ctx); err != errInvalidCacheType {
		t.Fatalf("expected %v, got items=%v err=%v", errInvalidCacheType, items, err)
	}

	if _, err := cache.Get(ctx, cacheKey); err != ErrCacheMiss {
		t.Fatalf("expected invalid cache item to be deleted, got %v", err)
	}

	items, err := reader.Read(ctx)
	if err != nil {
		t.Fatalf("unexpected error on reread: %v", err)
	}
	if len(items) != 1 || items[0] == nil || items[0].ID != cacheKey.IntValue {
		t.Fatalf("unexpected items: %#v", items)
	}
	if queryCalls != 1 {
		t.Fatalf("expected query to run once after invalid item removal, got %d", queryCalls)
	}
}

func TestCachedRefreshReaderDropsInvalidCacheItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := int32(789)
	cacheKey := UserCacheKey(key)
	cache := newReaderTestCache()
	if err := cache.Set(ctx, cacheKey, "invalid"); err != nil {
		t.Fatal(err)
	}

	reader := &CachedRefreshReader[int32, testCacheEntity]{
		Key:          key,
		Cache:        cache,
		CacheKeyFunc: UserCacheKey,
		DropInvalid:  true,
	}

	if item, needsRefresh, err := reader.Read(ctx); err != errInvalidCacheType {
		t.Fatalf("expected %v, got item=%v needsRefresh=%v err=%v", errInvalidCacheType, item, needsRefresh, err)
	}

	if _, err := cache.Get(ctx, cacheKey); err != ErrCacheMiss {
		t.Fatalf("expected invalid cache item to be deleted, got %v", err)
	}

	cache.refresh[cacheKey] = true
	if err := cache.Set(ctx, cacheKey, &testCacheEntity{ID: key}); err != nil {
		t.Fatal(err)
	}

	item, needsRefresh, err := reader.Read(ctx)
	if err != nil {
		t.Fatalf("unexpected error on reread: %v", err)
	}
	if item == nil || item.ID != key {
		t.Fatalf("unexpected item: %#v", item)
	}
	if !needsRefresh {
		t.Fatal("expected cached refresh state to be preserved")
	}
}

func TestStoreBulkReaderDropsInvalidCacheItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := newReaderTestCache()
	invalidKey := UserCacheKey(1)
	cachedKey := UserCacheKey(2)
	if err := cache.Set(ctx, invalidKey, "invalid"); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(ctx, cachedKey, &testCacheEntity{ID: 2}); err != nil {
		t.Fatal(err)
	}

	var queried []int32
	reader := &StoreBulkReader[int32, int32, testCacheEntity]{
		ArgFunc: func(item *testCacheEntity) int32 {
			return item.ID
		},
		QueryFunc: func(ctx context.Context, keys []int32) ([]*testCacheEntity, error) {
			queried = append(queried, keys...)
			return []*testCacheEntity{{ID: 1}}, nil
		},
		QueryKeyFunc: func(arg int32) (int32, error) {
			return arg, nil
		},
		Cache:        cache,
		CacheKeyFunc: UserCacheKey,
		DropInvalid:  true,
	}

	cachedItems, fetchedItems, err := reader.Read(ctx, map[int32]uint{1: 1, 2: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cachedItems) != 1 || cachedItems[0] == nil || cachedItems[0].ID != 2 {
		t.Fatalf("unexpected cached items: %#v", cachedItems)
	}
	if len(fetchedItems) != 1 || fetchedItems[0] == nil || fetchedItems[0].ID != 1 {
		t.Fatalf("unexpected fetched items: %#v", fetchedItems)
	}
	if len(queried) != 1 || queried[0] != 1 {
		t.Fatalf("expected only invalid item to be queried, got %#v", queried)
	}
	if _, err := cache.Get(ctx, invalidKey); err != ErrCacheMiss {
		t.Fatalf("expected invalid cache item to be deleted, got %v", err)
	}
}
