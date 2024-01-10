package db

import (
	"bytes"
	"context"
	"encoding/gob"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/utils"
	"github.com/redis/go-redis/v9"
)

type Cache struct {
	Redis *redis.Client
}

func (c *Cache) GetProperty(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	val, err := c.Redis.Get(ctx, sitekey).Bytes()
	if err == redis.Nil {
		slog.Log(ctx, common.LevelTrace, "Property is missing from cache", "sitekey", sitekey)
		return nil, nil
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to get item from Redis", common.ErrAttr(err))
		return nil, err
	}

	property := &dbgen.Property{}

	buffer := bytes.NewBuffer(val)
	decoder := gob.NewDecoder(buffer)

	if err := decoder.Decode(&property); err != nil {
		slog.ErrorContext(ctx, "Failed to parse property from Redis data", common.ErrAttr(err))
		return nil, err
	}

	slog.Log(ctx, common.LevelTrace, "Serving property from cache", "pid", property.ID)

	return property, nil
}

func (c *Cache) UpdateExpiration(ctx context.Context, key string, expiration time.Duration) error {
	return c.Redis.Expire(ctx, key, expiration).Err()
}

func (c *Cache) SetProperty(ctx context.Context, property *dbgen.Property, expiration time.Duration) error {
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(property); err != nil {
		slog.ErrorContext(ctx, "Failed to serialize property", common.ErrAttr(err))
		return err
	}

	sitekey := utils.UUIDToSiteKey(property.ExternalID)

	err := c.Redis.Set(ctx, sitekey, buffer.Bytes(), expiration).Err()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to save property to cache", "pid", property.ID, common.ErrAttr(err))
	}

	slog.Log(ctx, common.LevelTrace, "Cached property", "pid", property.ID)

	return err
}
