package db

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/redis/go-redis/v9"
)

var (
	missingData         = []byte{0xC0, 0x00, 0x10, 0xFF} // cool off
	ErrNegativeCacheHit = errors.New("negative hit")
	ErrCacheMiss        = errors.New("cache miss")
)

// TODO: Add local limited cache before accessing Redis
// smth like map[string]interface{}
type cache struct {
	redis *redis.Client
}

func NewRedisCache(opts *redis.Options) *cache {
	return &cache{redis: redis.NewClient(opts)}
}

var _ common.Cache = (*cache)(nil)

func (c *cache) Ping(ctx context.Context) error {
	return c.redis.Ping(ctx).Err()
}

func (c *cache) GetAndExpireItem(ctx context.Context, key string, expiration time.Duration, dst any) error {
	txPipeline := c.redis.TxPipeline()

	getCmd := txPipeline.Get(ctx, key)
	_ = txPipeline.Expire(ctx, key, expiration)

	_, err := txPipeline.Exec(ctx)
	if err == redis.Nil {
		slog.Log(ctx, common.LevelTrace, "Item is missing from cache", "key", key)
		return ErrCacheMiss
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute operation with Redis", common.ErrAttr(err))
		return err
	}

	val, err := getCmd.Bytes()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get READ response", common.ErrAttr(err))
		return err
	}

	if bytes.Equal(val, missingData) {
		return ErrNegativeCacheHit
	}

	buffer := bytes.NewBuffer(val)
	decoder := gob.NewDecoder(buffer)

	if err := decoder.Decode(dst); err != nil {
		slog.ErrorContext(ctx, "Failed to parse item from Redis data", common.ErrAttr(err))
		return err
	}

	slog.Log(ctx, common.LevelTrace, "Found item in cache", "key", key)

	return nil
}

func (c *cache) UpdateExpiration(ctx context.Context, key string, expiration time.Duration) error {
	err := c.redis.Expire(ctx, key, expiration).Err()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update key expiration in Redis", "key", key, common.ErrAttr(err))
	}

	return err
}

func (c *cache) SetMissing(ctx context.Context, key string, expiration time.Duration) error {
	err := c.redis.Set(ctx, key, missingData, expiration).Err()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to cache missing item", "key", key, common.ErrAttr(err))
	}

	slog.Log(ctx, common.LevelTrace, "Set item as missing in cache", "key", key)

	return err
}

func (c *cache) SetItem(ctx context.Context, key string, t any, expiration time.Duration) error {
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(t); err != nil {
		slog.ErrorContext(ctx, "Failed to serialize item", common.ErrAttr(err))
		return err
	}

	err := c.redis.Set(ctx, key, buffer.Bytes(), expiration).Err()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to save item to cache", "pid", key, common.ErrAttr(err))
	}

	slog.Log(ctx, common.LevelTrace, "Saved item to cache", "key", key)

	return err
}
