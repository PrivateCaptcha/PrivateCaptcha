package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/maypok86/otter/v2"
)

const (
	sessionBatchSize           = 20
	sessionCacheTTL            = 3 * time.Hour
	sessionBackpressureTimeout = 200 * time.Millisecond
)

type SessionStore struct {
	store         Implementor
	persistChan   chan string
	batchSize     int
	processCancel context.CancelFunc
	persistKey    session.SessionKey
	metrics       common.BaseMetrics
}

func NewSessionStore(store Implementor, persistKey session.SessionKey, metrics common.BaseMetrics) *SessionStore {
	return &SessionStore{
		store:         store,
		persistChan:   make(chan string, sessionBatchSize),
		batchSize:     sessionBatchSize,
		persistKey:    persistKey,
		metrics:       metrics,
		processCancel: func() {},
	}
}

func (ss *SessionStore) Start(ctx context.Context, interval time.Duration) {
	var cancelCtx context.Context
	cancelCtx, ss.processCancel = context.WithCancel(
		context.WithValue(ctx, common.TraceIDContextKey, "persist_session"))
	go common.ProcessBatchMap(cancelCtx, ss.persistChan, interval, ss.batchSize, ss.batchSize*100, ss.persistSessions)
}

var _ session.Store = (*SessionStore)(nil)

func (ss *SessionStore) Shutdown() {
	slog.Debug("Shutting down persisting sessions")
	ss.processCancel()
	close(ss.persistChan)
}

func (ss *SessionStore) Init(ctx context.Context, session *session.Session) error {
	return ss.store.Impl().CacheUserSession(ctx, session.Data())
}

func (ss *SessionStore) Read(ctx context.Context, sid string, skipCache bool) (*session.Session, error) {
	sd, err := ss.store.Impl().RetrieveUserSession(ctx, sid, skipCache)
	if err != nil {
		if (err == ErrNegativeCacheHit) || (err == ErrCacheMiss) || (err == otter.ErrNotFound) {
			return nil, session.ErrSessionMissing
		}

		return nil, err
	}

	return session.NewSession(sd, ss), nil
}

func (ss *SessionStore) Update(ctx context.Context, sd *session.Session) error {
	timer := time.NewTimer(sessionBackpressureTimeout)
	defer timer.Stop()

	select {
	case ss.persistChan <- sd.ID():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		ss.metrics.ObserveEventDropped(common.SessionEventType)
		return nil
	}
}

func (ss *SessionStore) TTL() time.Duration {
	return sessionCacheTTL
}

func (ss *SessionStore) Destroy(ctx context.Context, sid string) error {
	return ss.store.Impl().DeleteUserSession(ctx, sid)
}

func (ss *SessionStore) persistSessions(ctx context.Context, batch map[string]uint) error {
	// we actually do not care if we failed to save sessions to cache
	_ = ss.store.Impl().StoreUserSessions(ctx, batch, ss.persistKey, sessionCacheTTL)
	return nil
}
