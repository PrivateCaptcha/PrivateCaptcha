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

type SessionStore interface {
	Init(ctx context.Context, session *Session) error
	Read(ctx context.Context, sid string) (*Session, error)
	Update(session *Session) error
	Destroy(ctx context.Context, sid string) error
	GC(d time.Duration)
}
