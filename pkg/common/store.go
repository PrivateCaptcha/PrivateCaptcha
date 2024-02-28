package common

import (
	"context"
	"time"
)

type Cache interface {
	GetAndExpireItem(ctx context.Context, key string, expiration time.Duration) (any, error)
	SetMissing(ctx context.Context, key string, expiration time.Duration) error
	SetItem(ctx context.Context, key string, t any, expiration time.Duration) error
}
