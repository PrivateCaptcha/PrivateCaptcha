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

func (ss *SessionStore) Stop() {
	ss.processCancel()
}

func (ss *SessionStore) Shutdown() {
	slog.Debug("Shutting down persisting sessions")
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

	if sd.Has(session.KeyTombstone) {
		return nil, session.ErrSessionMissing
	}

	return session.NewSession(sd, ss), nil
}

func (ss *SessionStore) Update(ctx context.Context, sd *session.Session) error {
	if sd == nil {
		return nil
	}

	return ss.enqueuePersistSession(ctx, sd.ID())
}

func (ss *SessionStore) enqueuePersistSession(ctx context.Context, sid string) error {
	timer := time.NewTimer(sessionBackpressureTimeout)
	defer timer.Stop()

	select {
	case ss.persistChan <- sid:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		ss.metrics.ObserveEventDropped(common.SessionEventType)
		return nil
	}
}

func (ss *SessionStore) Renew(ctx context.Context, oldSID string, sess *session.Session) error {
	if err := ss.Init(ctx, sess); err != nil {
		return err
	}

	oldSession := session.NewSession(session.NewTombstoneSessionData(oldSID), ss)
	if err := ss.Init(ctx, oldSession); err != nil {
		return err
	}

	if err := ss.enqueuePersistSession(ctx, oldSID); err != nil {
		slog.ErrorContext(ctx, "Failed to queue old session tombstone for persistence", common.SessionIDAttr(oldSID), common.ErrAttr(err))
		return err
	}

	if err := ss.enqueuePersistSession(ctx, sess.ID()); err != nil {
		slog.ErrorContext(ctx, "Failed to queue renewed session for persistence", common.SessionIDAttr(sess.ID()), common.ErrAttr(err))
		return err
	}

	return nil
}

func (ss *SessionStore) TTL() time.Duration {
	return sessionCacheTTL
}

func (ss *SessionStore) Destroy(ctx context.Context, sid string) error {
	return ss.store.Impl().DeleteUserSession(ctx, sid)
}

func (ss *SessionStore) persistSessions(ctx context.Context, batch map[string]uint) error {
	toStore := make(map[string]uint, len(batch))
	toDelete := make([]string, 0)

	impl := ss.store.Impl()
	if cached, err := impl.GetCachedUserSessions(ctx, batch); err == nil {
		for _, sd := range cached {
			sid := sd.ID()
			if sd.Has(session.KeyTombstone) {
				toDelete = append(toDelete, sid)
				continue
			}
			toStore[sid] = batch[sid]
		}

		// we actually do not care if we failed to save or delete sessions in the DB cache
		_ = impl.StoreUserSessions(ctx, toStore, ss.persistKey, sessionCacheTTL)
		_ = impl.DeleteUserSessions(ctx, toDelete)
	} else {
		slog.ErrorContext(ctx, "Failed to read cached sessions for persistence", common.ErrAttr(err))
		return err
	}

	return nil
}

// rollback tombstone when renew fails
func (ss *SessionStore) RollbackRenew(ctx context.Context, oldSID string) {
	impl := ss.store.Impl()
	_ = impl.DeleteUserSession(ctx, oldSID)
}
