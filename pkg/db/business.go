package db

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrRecordNotFound     = errors.New("record not found")
	ErrSoftDeleted        = errors.New("record is marked as deleted")
	ErrDuplicateAccount   = errors.New("this subscrption already has an account")
	ErrLocked             = errors.New("lock is already acquired")
	ErrMaintenance        = errors.New("maintenance mode")
	ErrTestProperty       = errors.New("test property")
	ErrPermissions        = errors.New("insufficient permissions")
	errInvalidCacheType   = errors.New("cache record type does not match")
	TestPropertySitekey   = strings.ReplaceAll(TestPropertyID, "-", "")
	PortalLoginSitekey    = strings.ReplaceAll(PortalLoginPropertyID, "-", "")
	PortalRegisterSitekey = strings.ReplaceAll(PortalRegisterPropertyID, "-", "")
	TestPropertyUUID      = UUIDFromSiteKey(TestPropertySitekey)
)

const (
	PortalLoginPropertyID    = "1ca8041a-5761-40a4-addf-f715a991bfea"
	PortalRegisterPropertyID = "8981be7a-3a71-414d-bb74-e7b4456603fd"
	TestPropertyID           = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	defaultCacheTTL          = 15 * time.Minute
	defaultCacheRefresh      = 30 * time.Minute
	negativeCacheTTL         = 5 * time.Minute
	auditBatchSize           = 100
)

type BusinessStore struct {
	Pool            *pgxpool.Pool
	defaultImpl     *BusinessStoreImpl
	cacheOnlyImpl   *BusinessStoreImpl
	Cache           common.Cache[CacheKey, any]
	auditLog        *AuditLog
	discardAuditLog *DiscardAuditLog
	// this could have been a bloom/cuckoo filter with expiration, if they existed
	puzzleCache     *puzzleCache
	MaintenanceMode atomic.Bool
}

type Implementor interface {
	Impl() *BusinessStoreImpl
	WithTx(ctx context.Context, fn func(*BusinessStoreImpl) ([]*common.AuditLogEvent, error)) ([]*common.AuditLogEvent, error)
	Ping(ctx context.Context) error
	CheckVerifiedPuzzle(ctx context.Context, p puzzle.Puzzle, maxCount uint32) bool
	CacheVerifiedPuzzle(ctx context.Context, p puzzle.Puzzle, tnow time.Time)
	CheckUserPropertyAccess(ctx context.Context, property *dbgen.Property, userID int32) bool
	CacheHitRatio() float64
	AuditLog() common.AuditLog
}

var _ Implementor = (*BusinessStore)(nil)

func NewBusiness(pool *pgxpool.Pool) *BusinessStore {
	const maxCacheSize = 1_000_000
	var cache common.Cache[CacheKey, any]
	var err error
	cache, err = NewMemoryCache[CacheKey, any]("default", maxCacheSize, &struct{}{}, defaultCacheTTL, defaultCacheRefresh, negativeCacheTTL)
	if err != nil {
		slog.Error("Failed to create memory cache", common.ErrAttr(err))
		cache = NewStaticCache[CacheKey, any](maxCacheSize, &struct{}{})
	}

	return NewBusinessEx(pool, cache)
}

func NewBusinessEx(pool *pgxpool.Pool, cache common.Cache[CacheKey, any]) *BusinessStore {
	var querier dbgen.Querier
	if pool != nil {
		querier = dbgen.New(pool)
	}

	auditLog := NewAuditLog(querier, auditBatchSize)

	return &BusinessStore{
		Pool:            pool,
		auditLog:        auditLog,
		discardAuditLog: &DiscardAuditLog{},
		defaultImpl:     &BusinessStoreImpl{cache: cache, querier: querier},
		cacheOnlyImpl:   &BusinessStoreImpl{cache: cache},
		Cache:           cache,
		puzzleCache:     newPuzzleCache(puzzle.DefaultValidityPeriod),
	}
}

func (s *BusinessStore) UpdateConfig(maintenanceMode bool) {
	s.MaintenanceMode.Store(maintenanceMode)
}

func (s *BusinessStore) AuditLog() common.AuditLog {
	if s.MaintenanceMode.Load() {
		return s.discardAuditLog
	}

	return s.auditLog
}

func (s *BusinessStore) Impl() *BusinessStoreImpl {
	if s.MaintenanceMode.Load() {
		return s.cacheOnlyImpl
	}

	return s.defaultImpl
}

func (s *BusinessStore) Start(ctx context.Context, auditLogInterval time.Duration) {
	s.auditLog.Start(ctx, auditLogInterval)
}

func (s *BusinessStore) Shutdown() {
	s.auditLog.Shutdown()
}

func (s *BusinessStore) WithTx(ctx context.Context, fn func(*BusinessStoreImpl) ([]*common.AuditLogEvent, error)) ([]*common.AuditLogEvent, error) {
	if s.MaintenanceMode.Load() {
		return nil, ErrMaintenance
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			if rerr := tx.Rollback(ctx); rerr != nil {
				slog.ErrorContext(ctx, "Failed to rollback transaction", common.ErrAttr(rerr))
			}
		}
	}()

	db := dbgen.New(s.Pool)
	tmpCache := NewTxCache()
	impl := &BusinessStoreImpl{cache: tmpCache, querier: db.WithTx(tx)}
	var auditEvents []*common.AuditLogEvent

	auditEvents, err = fn(impl)

	if err != nil {
		return auditEvents, err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return auditEvents, err
	}

	tmpCache.Commit(ctx, s.Cache)

	return auditEvents, nil
}

func (s *BusinessStore) Ping(ctx context.Context) error {
	// NOTE: we always use "real" DB connection to check for ping
	return s.defaultImpl.ping(ctx)
}

func (s *BusinessStore) CacheHitRatio() float64 {
	return s.Cache.HitRatio()
}

func (s *BusinessStore) CheckVerifiedPuzzle(ctx context.Context, p puzzle.Puzzle, maxCount uint32) bool {
	if p == nil || p.IsZero() {
		return false
	}

	// purely theoretically there's still a chance of cache collision, but it's so negligible that it's allowed
	// (HashKey() has to match during puzzle.DefaultValidityPeriod on the same server)
	return !s.puzzleCache.CheckCount(ctx, p.HashKey(), maxCount)
}

func (s *BusinessStore) CacheVerifiedPuzzle(ctx context.Context, p puzzle.Puzzle, tnow time.Time) {
	if p == nil || p.IsZero() {
		slog.Log(ctx, common.LevelTrace, "Skipping caching zero puzzle")
		return
	}

	expiration := p.Expiration()
	// this check should have been done before in the pipeline. Here the check only to safeguard storing in cache
	if !tnow.Before(expiration) {
		slog.WarnContext(ctx, "Skipping caching expired puzzle", "now", tnow, "expiration", p.Expiration())
		return
	}

	value := s.puzzleCache.Inc(ctx, p.HashKey(), expiration.Sub(tnow))
	slog.Log(ctx, common.LevelTrace, "Cached verified puzzle", "times", value)
}

func (s *BusinessStore) CheckUserPropertyAccess(ctx context.Context, property *dbgen.Property, userID int32) bool {
	_, level, err := s.cacheOnlyImpl.retrieveOrganizationWithAccess(ctx, userID, property.OrgID.Int32)
	if (err == nil) && level.Valid {
		return (level.AccessLevel == dbgen.AccessLevelMember) || (level.AccessLevel == dbgen.AccessLevelOwner)
	}

	// the reason we use orgUsers() API and not userOrgs() API is that the former is a bit faster
	members, err := s.Impl().RetrieveOrganizationUsers(ctx, property.OrgID.Int32)
	if (err == nil) && (len(members) > 0) {
		for _, user := range members {
			if user.User.ID == userID {
				slog.DebugContext(ctx, "Found user as org member", "level", user.Level, "userID", userID, "orgID", property.OrgID)
				return (user.Level == dbgen.AccessLevelMember) || (level.AccessLevel == dbgen.AccessLevelOwner)
			}
		}
	}

	return false
}
