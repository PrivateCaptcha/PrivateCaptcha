package db

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestNewTxCache(t *testing.T) {
	t.Parallel()

	cache := NewTxCache()
	if cache == nil {
		t.Fatal("Expected non-nil cache")
	}

	if cache.set == nil || cache.del == nil || cache.missing == nil {
		t.Error("Expected initialized maps")
	}
}

func TestTxCacheHitRatio(t *testing.T) {
	t.Parallel()

	cache := NewTxCache()
	if cache.HitRatio() != 0.0 {
		t.Errorf("Expected HitRatio to return 0.0, got %v", cache.HitRatio())
	}
}

func TestTxCacheMissing(t *testing.T) {
	t.Parallel()

	cache := NewTxCache()
	if cache.Missing() != nil {
		t.Errorf("Expected Missing to return nil, got %v", cache.Missing())
	}
}

func TestTxCacheGet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewTxCache()

	key := UserCacheKey(1)
	_, err := cache.Get(ctx, key)

	if err != errTransactionCache {
		t.Errorf("Expected errTransactionCache, got %v", err)
	}
}

type mockCacheLoader struct {
	loadFunc func(ctx context.Context, key CacheKey) (any, error)
}

func (m *mockCacheLoader) Load(ctx context.Context, key CacheKey) (any, error) {
	return m.loadFunc(ctx, key)
}

func (m *mockCacheLoader) Reload(ctx context.Context, key CacheKey, oldValue any) (any, error) {
	return m.loadFunc(ctx, key)
}

func TestTxCacheGetEx(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewTxCache()

	key := UserCacheKey(1)
	expectedValue := "test-value"

	loader := &mockCacheLoader{
		loadFunc: func(ctx context.Context, k CacheKey) (any, error) {
			return expectedValue, nil
		},
	}

	result, err := cache.GetEx(ctx, key, loader)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result != expectedValue {
		t.Errorf("Expected %q, got %v", expectedValue, result)
	}
}

func TestTxCacheSetMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewTxCache()

	key := UserCacheKey(1)
	err := cache.SetMissing(ctx, key)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if _, ok := cache.missing[key]; !ok {
		t.Error("Expected key to be in missing map")
	}
}

func TestTxCacheSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewTxCache()

	key := UserCacheKey(1)
	value := "test-value"

	err := cache.Set(ctx, key, value)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if cached, ok := cache.set[key]; !ok {
		t.Error("Expected key to be in set map")
	} else if cached.item != value {
		t.Errorf("Expected value %q, got %v", value, cached.item)
	}
}

func TestTxCacheSetWithTTL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewTxCache()

	key := UserCacheKey(1)
	value := "test-value"
	ttl := 5 * time.Minute

	err := cache.SetEx(ctx, key, value, ttl, 15*time.Minute)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if cached, ok := cache.set[key]; !ok {
		t.Error("Expected key to be in set map")
	} else {
		if cached.item != value {
			t.Errorf("Expected value %q, got %v", value, cached.item)
		}
		if cached.ttl != ttl {
			t.Errorf("Expected TTL %v, got %v", ttl, cached.ttl)
		}
	}
}

func TestTxCacheSetTTL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewTxCache()

	key := UserCacheKey(1)
	value := "test-value"

	// First set the value
	_ = cache.Set(ctx, key, value)

	// Then update TTL
	ttl := 10 * time.Minute
	err := cache.SetTTL(ctx, key, ttl)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if cached := cache.set[key]; cached.ttl != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, cached.ttl)
	}
}

func TestTxCacheSetTTLNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewTxCache()

	key := UserCacheKey(999)
	err := cache.SetTTL(ctx, key, 5*time.Minute)

	if err != ErrRecordNotFound {
		t.Errorf("Expected ErrRecordNotFound, got %v", err)
	}
}

func TestTxCacheDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewTxCache()

	key := UserCacheKey(1)
	result := cache.Delete(ctx, key)

	if !result {
		t.Error("Expected Delete to return true")
	}

	if _, ok := cache.del[key]; !ok {
		t.Error("Expected key to be in del map")
	}
}

// mockCache implements common.Cache for testing Commit
type mockCache struct {
	deleted   []CacheKey
	setValues map[CacheKey]any
	setTTLs   map[CacheKey]time.Duration
	missing   []CacheKey
}

func newMockCache() *mockCache {
	return &mockCache{
		setValues: make(map[CacheKey]any),
		setTTLs:   make(map[CacheKey]time.Duration),
	}
}

func (m *mockCache) HitRatio() float64 { return 0.0 }
func (m *mockCache) SaveTo(ctx context.Context, w io.Writer, max int) error {
	return nil
}

func (m *mockCache) LoadFrom(context.Context, io.Reader, time.Duration) error {
	return nil
}

func (m *mockCache) Missing() any { return nil }
func (m *mockCache) Clear() {
	m.setValues = make(map[CacheKey]any)
	m.setTTLs = make(map[CacheKey]time.Duration)
}
func (m *mockCache) GetWithRefresh(ctx context.Context, key CacheKey) (any, bool, error) {
	val, err := m.Get(ctx, key)
	return val, false, err
}
func (m *mockCache) Get(ctx context.Context, key CacheKey) (any, error) {
	return nil, nil
}
func (m *mockCache) GetEx(ctx context.Context, key CacheKey, loader common.CacheLoader[CacheKey, any]) (any, error) {
	return loader.Load(ctx, key)
}
func (m *mockCache) SetMissing(ctx context.Context, key CacheKey) error {
	m.missing = append(m.missing, key)
	return nil
}
func (m *mockCache) Set(ctx context.Context, key CacheKey, t any) error {
	m.setValues[key] = t
	return nil
}
func (m *mockCache) SetEx(ctx context.Context, key CacheKey, t any, ttl, _ time.Duration) error {
	m.setValues[key] = t
	m.setTTLs[key] = ttl
	return nil
}
func (m *mockCache) SetTTL(ctx context.Context, key CacheKey, ttl time.Duration) error {
	return nil
}
func (m *mockCache) SetRefresh(ctx context.Context, key CacheKey, ttl time.Duration) error {
	return nil
}
func (m *mockCache) Delete(ctx context.Context, key CacheKey) bool {
	m.deleted = append(m.deleted, key)
	return true
}

func TestTxCacheCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	txCache := NewTxCache()
	realCache := newMockCache()

	// Setup operations
	setKey := UserCacheKey(1)
	setTTLKey := UserCacheKey(2)
	deleteKey := UserCacheKey(3)
	missingKey := UserCacheKey(4)

	_ = txCache.Set(ctx, setKey, "value1")
	_ = txCache.SetEx(ctx, setTTLKey, "value2", 10*time.Minute, 15*time.Minute)
	txCache.Delete(ctx, deleteKey)
	_ = txCache.SetMissing(ctx, missingKey)

	// Commit to real cache
	txCache.Commit(ctx, realCache)

	// Verify deletes
	if len(realCache.deleted) != 1 || realCache.deleted[0] != deleteKey {
		t.Errorf("Expected delete of %v, got %v", deleteKey, realCache.deleted)
	}

	// Verify sets
	if realCache.setValues[setKey] != "value1" {
		t.Errorf("Expected 'value1' for setKey, got %v", realCache.setValues[setKey])
	}

	if realCache.setValues[setTTLKey] != "value2" {
		t.Errorf("Expected 'value2' for setTTLKey, got %v", realCache.setValues[setTTLKey])
	}

	if realCache.setTTLs[setTTLKey] != 10*time.Minute {
		t.Errorf("Expected TTL 10m for setTTLKey, got %v", realCache.setTTLs[setTTLKey])
	}

	// Verify missing
	if len(realCache.missing) != 1 || realCache.missing[0] != missingKey {
		t.Errorf("Expected missing %v, got %v", missingKey, realCache.missing)
	}
}
