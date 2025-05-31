package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sessionPrefix        = "session/"
	sessionCacheDuration = 3 * time.Hour
	sessionBatchSize     = 20
)

type SessionStore struct {
	db            *dbgen.Queries
	fallback      common.SessionStore
	persistChan   chan string
	batchSize     int
	processCancel context.CancelFunc
	persistKey    common.SessionKey
}

func NewSessionStore(pool *pgxpool.Pool, fallback common.SessionStore, interval time.Duration, persistKey common.SessionKey) *SessionStore {
	store := &SessionStore{
		db:          dbgen.New(pool),
		fallback:    fallback,
		persistChan: make(chan string, sessionBatchSize),
		batchSize:   sessionBatchSize,
		persistKey:  persistKey,
	}

	var cancelCtx context.Context
	cancelCtx, store.processCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "persist_session"))
	go common.ProcessBatchMap(cancelCtx, store.persistChan, interval, store.batchSize, store.batchSize*100, store.persistSessions)

	return store
}

var _ common.SessionStore = (*SessionStore)(nil)

func (ss *SessionStore) MaxLifetime() time.Duration {
	return sessionCacheDuration
}

func (ss *SessionStore) Shutdown() {
	slog.Debug("Shutting down persisting sessions")
	ss.processCancel()
	close(ss.persistChan)
}

func (ss *SessionStore) Init(ctx context.Context, s *common.Session) error {
	return ss.fallback.Init(ctx, s)
}

func (ss *SessionStore) Read(ctx context.Context, sid string) (*common.Session, error) {
	s, err := ss.fallback.Read(ctx, sid)

	if err == common.ErrSessionMissing {
		data, cerr := ss.db.GetCachedByKey(ctx, sessionPrefix+sid)
		if (cerr == nil) && (len(data) > 0) {
			slog.DebugContext(ctx, "Found session cached in DB", "sid", sid)
			s = common.NewSession(sid, ss)
			if uerr := s.UnmarshalBinary(data); uerr != nil {
				slog.ErrorContext(ctx, "Failed to unmarshal session from cache", common.ErrAttr(uerr))
				return nil, uerr
			}
			err = ss.Init(ctx, s)
			return s, err
		} else if cerr != pgx.ErrNoRows {
			slog.ErrorContext(ctx, "Failed to read session from DB cache", common.ErrAttr(err))
		} else {
			slog.DebugContext(ctx, "Session not found in DB", "sid", sid)
		}
	}

	return s, err
}

func (ss *SessionStore) Update(s *common.Session) error {
	if err := ss.fallback.Update(s); err != nil {
		return err
	}

	ss.persistChan <- s.SessionID()

	return nil
}

func (ss *SessionStore) Destroy(ctx context.Context, sid string) error {
	if err := ss.fallback.Destroy(ctx, sid); err != nil {
		return err
	}

	return ss.db.DeleteCachedByKey(ctx, sessionPrefix+sid)
}

func (ss *SessionStore) GC(ctx context.Context, d time.Duration) {
	ss.fallback.GC(ctx, d)
}

func (ss *SessionStore) persistSessions(ctx context.Context, batch map[string]struct{}) error {
	slog.DebugContext(ctx, "Persisting sessions to DB", "count", len(batch))

	keys := make([]string, 0, len(batch))
	values := make([][]byte, 0, len(batch))
	intervals := make([]time.Duration, 0, len(batch))

	for sid := range batch {
		sess, err := ss.fallback.Read(ctx, sid)
		if err != nil {
			slog.WarnContext(ctx, "Failed to find session to persist", "sid", sid, common.ErrAttr(err))
			continue
		}

		if !sess.Has(ss.persistKey) {
			slog.Log(ctx, common.LevelTrace, "Skipping persisting session without persist key", "sid", sid)
			continue
		}

		data, err := sess.MarshalBinary()
		if err != nil {
			slog.ErrorContext(ctx, "Failed to marshal session", common.ErrAttr(err))
			continue
		}

		keys = append(keys, sessionPrefix+sid)
		values = append(values, data)
		intervals = append(intervals, sessionCacheDuration)
	}

	if len(keys) == 0 {
		slog.WarnContext(ctx, "No sessions to save")
		return nil
	}

	if err := ss.db.CreateCacheMany(ctx, &dbgen.CreateCacheManyParams{
		Keys:      keys,
		Values:    values,
		Intervals: intervals,
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to cache sessions", "count", len(keys), common.ErrAttr(err))
	}

	// we actually do not care if we failed to save sessions to cache
	return nil
}
