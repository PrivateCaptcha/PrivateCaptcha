package common

import (
	"context"
	"time"
)

type Cache interface {
	GetItem(ctx context.Context, key string, dst any) error
	UpdateExpiration(ctx context.Context, key string, expiration time.Duration) error
	SetMissing(ctx context.Context, key string, expiration time.Duration) error
	SetItem(ctx context.Context, key string, t any, expiration time.Duration) error
}
