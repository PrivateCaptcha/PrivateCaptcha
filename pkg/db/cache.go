package db

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/patrickmn/go-cache"
)

var (
	missingData         = []byte{0xC0, 0x00, 0x10, 0xFF} // cool off
	ErrNegativeCacheHit = errors.New("negative hit")
	ErrCacheMiss        = errors.New("cache miss")
)

type memcache struct {
	store *cache.Cache
}

func NewMemoryCache(cleanupDuration time.Duration) *memcache {
	return &memcache{store: cache.New(5*time.Minute /*default expiration*/, cleanupDuration)}
}

var _ common.Cache = (*memcache)(nil)

func (c *memcache) GetAndExpireItem(ctx context.Context, key string, expiration time.Duration) (any, error) {
	data, found := c.store.Get(key)
	if !found {
		return nil, ErrCacheMiss
	}

	if dataBytes, ok := data.([]byte); ok && bytes.Equal(dataBytes, missingData) {
		return nil, ErrNegativeCacheHit
	}

	slog.Log(ctx, common.LevelTrace, "Found item in memory cache", "key", key)

	// TODO: update expiration in cache

	return data, nil
}

func (c *memcache) SetMissing(ctx context.Context, key string, expiration time.Duration) error {
	// TODO: Cache this based on the current cache size to prevent flood attacks
	c.store.Set(key, missingData, expiration)

	slog.Log(ctx, common.LevelTrace, "Set item as missing in memory cache", "key", key)

	return nil
}

func (c *memcache) SetItem(ctx context.Context, key string, t any, expiration time.Duration) error {
	c.store.Set(key, t, expiration)

	slog.Log(ctx, common.LevelTrace, "Saved item to memory cache", "key", key)

	return nil
}
