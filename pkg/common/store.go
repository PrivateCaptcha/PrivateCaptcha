package common

import (
	"context"
	"time"
)

type Cache[TKey comparable, TValue any] interface {
	Get(ctx context.Context, key TKey) (TValue, error)
	SetMissing(ctx context.Context, key TKey) error
	Set(ctx context.Context, key TKey, t TValue) error
	Delete(ctx context.Context, key TKey) error
}

type SessionStore interface {
	Init(ctx context.Context, session *Session) error
	Read(ctx context.Context, sid string) (*Session, error)
	Update(session *Session) error
	Destroy(ctx context.Context, sid string) error
	GC(ctx context.Context, d time.Duration)
}
