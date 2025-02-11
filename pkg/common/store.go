package common

import (
	"context"
	"time"
)

type Cache[TKey comparable, TValue any] interface {
	Get(ctx context.Context, key TKey) (TValue, error)
	SetMissing(ctx context.Context, key TKey, ttl time.Duration) error
	Set(ctx context.Context, key TKey, t TValue, ttl time.Duration) error
	Delete(ctx context.Context, key TKey) error
}

type SessionStore interface {
	Init(ctx context.Context, session *Session) error
	Read(ctx context.Context, sid string) (*Session, error)
	Update(session *Session) error
	Destroy(ctx context.Context, sid string) error
	GC(ctx context.Context, d time.Duration)
}

type ConfigItem interface {
	Key() ConfigKey
	Value() string
}

type ConfigStore interface {
	Get(key ConfigKey) ConfigItem
	Update(ctx context.Context)
}
