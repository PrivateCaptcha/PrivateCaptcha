package db

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/maypok86/otter"
)

var (
	ErrNegativeCacheHit = errors.New("negative hit")
	ErrCacheMiss        = errors.New("cache miss")
	ErrSetMissing       = errors.New("cannot set missing value directly")
)

const (
	UserLimitTTL    = 3 * time.Hour
	DefaultCacheTTL = 5 * time.Minute
)

type memcache[TKey comparable, TValue comparable] struct {
	store        otter.CacheWithVariableTTL[TKey, TValue]
	missingValue TValue
}

func NewMemoryCache[TKey comparable, TValue comparable](maxCacheSize int, missingValue TValue) (*memcache[TKey, TValue], error) {
	store, err := otter.MustBuilder[TKey, TValue](maxCacheSize).
		WithVariableTTL().
		Build()

	if err != nil {
		return nil, err
	}

	return &memcache[TKey, TValue]{
		store:        store,
		missingValue: missingValue,
	}, nil
}

var _ common.Cache[int, any] = (*memcache[int, any])(nil)

func (c *memcache[TKey, TValue]) Get(ctx context.Context, key TKey) (TValue, error) {
	data, found := c.store.Get(key)
	if !found {
		slog.Log(ctx, common.LevelTrace, "Item not found in memory cache", "key", key)
		var zero TValue
		return zero, ErrCacheMiss
	}

	if data == c.missingValue {
		slog.Log(ctx, common.LevelTrace, "Item set as missing in memory cache", "key", key)
		var zero TValue
		return zero, ErrNegativeCacheHit
	}

	slog.Log(ctx, common.LevelTrace, "Found item in memory cache", "key", key)

	return data, nil
}

func (c *memcache[TKey, TValue]) SetMissing(ctx context.Context, key TKey, ttl time.Duration) error {
	c.store.Set(key, c.missingValue, ttl)

	slog.Log(ctx, common.LevelTrace, "Set item as missing in memory cache", "key", key, "ttl", ttl)

	return nil
}

func (c *memcache[TKey, TValue]) Set(ctx context.Context, key TKey, t TValue, ttl time.Duration) error {
	if t == c.missingValue {
		return ErrSetMissing
	}

	c.store.Set(key, t, ttl)

	slog.Log(ctx, common.LevelTrace, "Saved item to memory cache", "key", key, "ttl", ttl)

	return nil
}

func (c *memcache[TKey, TValue]) Delete(ctx context.Context, key TKey) error {
	c.store.Delete(key)

	slog.Log(ctx, common.LevelTrace, "Deleted item from memory cache", "key", key)

	return nil
}
