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

type Cache struct {
	Redis *redis.Client
}

func (c *Cache) GetItem(ctx context.Context, key string, dst any) error {
	val, err := c.Redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		slog.Log(ctx, common.LevelTrace, "Item is missing from cache", "key", key)
		return ErrCacheMiss
	}

	if bytes.Equal(val, missingData) {
		return ErrNegativeCacheHit
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to get item from Redis", common.ErrAttr(err))
		return err
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

func (c *Cache) UpdateExpiration(ctx context.Context, key string, expiration time.Duration) error {
	err := c.Redis.Expire(ctx, key, expiration).Err()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update key expiration in Redis", "key", key, common.ErrAttr(err))
	}

	return err
}

func (c *Cache) SetMissing(ctx context.Context, key string, expiration time.Duration) error {
	err := c.Redis.Set(ctx, key, missingData, expiration).Err()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to cache missing item", "key", key, common.ErrAttr(err))
	}

	slog.Log(ctx, common.LevelTrace, "Set item as missing in cache", "key", key)

	return err
}

func (c *Cache) SetItem(ctx context.Context, key string, t any, expiration time.Duration) error {
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(t); err != nil {
		slog.ErrorContext(ctx, "Failed to serialize item", common.ErrAttr(err))
		return err
	}

	err := c.Redis.Set(ctx, key, buffer.Bytes(), expiration).Err()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to save item to cache", "pid", key, common.ErrAttr(err))
	}

	slog.Log(ctx, common.LevelTrace, "Saved item to cache", "key", key)

	return err
}
