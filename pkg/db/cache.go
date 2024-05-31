package db

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/maypok86/otter"
)

const (
	maxCacheSize = 1_000_000
	missingData  = 0xC0010FF // cool off
)

var (
	ErrNegativeCacheHit = errors.New("negative hit")
	ErrCacheMiss        = errors.New("cache miss")
)

type memcache struct {
	store otter.Cache[string, any]
}

func NewMemoryCache(expiration time.Duration) (*memcache, error) {
	store, err := otter.MustBuilder[string, any](maxCacheSize).
		WithTTL(expiration).
		Build()

	if err != nil {
		return nil, err
	}

	return &memcache{
		store: store,
	}, nil
}

var _ common.Cache = (*memcache)(nil)

func (c *memcache) Get(ctx context.Context, key string) (any, error) {
	data, found := c.store.Get(key)
	if !found {
		return nil, ErrCacheMiss
	}

	if mark, ok := data.(uint32); ok && mark == missingData {
		return nil, ErrNegativeCacheHit
	}

	slog.Log(ctx, common.LevelTrace, "Found item in memory cache", "key", key)

	return data, nil
}

func (c *memcache) SetMissing(ctx context.Context, key string) error {
	c.store.Set(key, uint32(missingData))

	slog.Log(ctx, common.LevelTrace, "Set item as missing in memory cache", "key", key)

	return nil
}

func (c *memcache) Set(ctx context.Context, key string, t any) error {
	c.store.Set(key, t)

	slog.Log(ctx, common.LevelTrace, "Saved item to memory cache", "key", key)

	return nil
}

func (c *memcache) Delete(ctx context.Context, key string) error {
	c.store.Delete(key)

	slog.Log(ctx, common.LevelTrace, "Deleted item from memory cache", "key", key)

	return nil
}
