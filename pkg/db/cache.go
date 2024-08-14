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

type memcache[TKey comparable, TValue comparable] struct {
	store        otter.Cache[TKey, TValue]
	missingValue TValue
}

func NewMemoryCache[TKey comparable, TValue comparable](expiration time.Duration, maxCacheSize int, missingValue TValue) (*memcache[TKey, TValue], error) {
	store, err := otter.MustBuilder[TKey, TValue](maxCacheSize).
		WithTTL(expiration).
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

func (c *memcache[TKey, TValue]) SetMissing(ctx context.Context, key TKey) error {
	c.store.Set(key, c.missingValue)

	slog.Log(ctx, common.LevelTrace, "Set item as missing in memory cache", "key", key)

	return nil
}

func (c *memcache[TKey, TValue]) Set(ctx context.Context, key TKey, t TValue) error {
	if t == c.missingValue {
		return ErrSetMissing
	}

	c.store.Set(key, t)

	slog.Log(ctx, common.LevelTrace, "Saved item to memory cache", "key", key)

	return nil
}

func (c *memcache[TKey, TValue]) Delete(ctx context.Context, key TKey) error {
	c.store.Delete(key)

	slog.Log(ctx, common.LevelTrace, "Deleted item from memory cache", "key", key)

	return nil
}
