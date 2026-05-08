//go:build enterprise

package db

import (
	"context"
	"testing"
)

type testCacheEntity struct {
	ID int32
}

const maxCacheSize = 32

func newReaderTestCache() *StaticCache[CacheKey, any] {
	return NewStaticCache[CacheKey, any](maxCacheSize, &CacheMissingValue{})
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

	reader := &StoreOneReader[int32, testCacheEntity]{
		CacheKey: cacheKey,
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
}

func TestStoreArrayReaderDropsInvalidCacheItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cacheKey := UserOrgsCacheKey(456)
	cache := newReaderTestCache()
	if err := cache.Set(ctx, cacheKey, "invalid"); err != nil {
		t.Fatal(err)
	}

	reader := &StoreArrayReader[int32, testCacheEntity]{
		CacheKey: cacheKey,
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
