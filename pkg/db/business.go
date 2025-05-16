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
)

type BusinessStore struct {
	Pool          *pgxpool.Pool
	defaultImpl   *BusinessStoreImpl
	cacheOnlyImpl *BusinessStoreImpl
	Cache         common.Cache[CacheKey, any]
	// this could have been a bloom/cuckoo filter with expiration, if they existed
	PuzzleCache     common.Cache[uint64, bool]
	MaintenanceMode atomic.Bool
}

type Implementor interface {
	Impl() *BusinessStoreImpl
	WithTx(ctx context.Context, fn func(*BusinessStoreImpl) error) error
	Ping(ctx context.Context) error
	CheckPuzzleCached(ctx context.Context, p *puzzle.Puzzle) bool
	CachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error
}

var _ Implementor = (*BusinessStore)(nil)

func NewBusiness(pool *pgxpool.Pool) *BusinessStore {
	const maxCacheSize = 1_000_000
	var cache common.Cache[CacheKey, any]
	var err error
	cache, err = NewMemoryCache[CacheKey, any](maxCacheSize, nil /*missing value*/)
	if err != nil {
		slog.Error("Failed to create memory cache", common.ErrAttr(err))
		cache = NewStaticCache[CacheKey, any](maxCacheSize, nil /*missing value*/)
	}

	return NewBusinessEx(pool, cache)
}

func NewBusinessEx(pool *pgxpool.Pool, cache common.Cache[CacheKey, any]) *BusinessStore {
	const maxPuzzleCacheSize = 100_000
	var puzzleCache common.Cache[uint64, bool]
	var err error
	puzzleCache, err = NewMemoryCache[uint64, bool](maxPuzzleCacheSize, false /*missing value*/)
	if err != nil {
		slog.Error("Failed to create puzzle memory cache", common.ErrAttr(err))
		puzzleCache = NewStaticCache[uint64, bool](maxPuzzleCacheSize, false /*missing value*/)
	}

	return &BusinessStore{
		Pool:          pool,
		defaultImpl:   &BusinessStoreImpl{cache: cache, querier: dbgen.New(pool), ttl: DefaultCacheTTL},
		cacheOnlyImpl: &BusinessStoreImpl{cache: cache, ttl: DefaultCacheTTL},
		Cache:         cache,
		PuzzleCache:   puzzleCache,
	}
}

func (s *BusinessStore) UpdateConfig(maintenanceMode bool) {
	s.MaintenanceMode.Store(maintenanceMode)
}

func (s *BusinessStore) Impl() *BusinessStoreImpl {
	if s.MaintenanceMode.Load() {
		return s.cacheOnlyImpl
	}

	return s.defaultImpl
}

func (s *BusinessStore) WithTx(ctx context.Context, fn func(*BusinessStoreImpl) error) error {
	if s.MaintenanceMode.Load() {
		return ErrMaintenance
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to rollback transaction", common.ErrAttr(err))
		}
	}()

	db := dbgen.New(s.Pool)
	tmpCache := NewTxCache()
	impl := &BusinessStoreImpl{cache: tmpCache, querier: db.WithTx(tx), ttl: DefaultCacheTTL}

	err = fn(impl)

	if err != nil {
		return err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	tmpCache.Commit(ctx, s.Cache)

	return nil
}

func (s *BusinessStore) Ping(ctx context.Context) error {
	// NOTE: we always use "real" DB connection to check for ping
	return s.defaultImpl.ping(ctx)
}

func (s *BusinessStore) CheckPuzzleCached(ctx context.Context, p *puzzle.Puzzle) bool {
	if p.PuzzleID == 0 {
		return false
	}

	ok, err := s.PuzzleCache.Get(ctx, p.PuzzleID)
	return (err == nil) && ok
}

func (s *BusinessStore) CachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error {
	if p.PuzzleID == 0 {
		slog.Log(ctx, common.LevelTrace, "Skipping caching stub puzzle")
		return nil
	}

	// this check should have been done before in the pipeline. Here the check only to safeguard storing in cache
	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Skipping caching expired puzzle", "now", tnow, "expiration", p.Expiration)
		return nil
	}

	return s.PuzzleCache.Set(ctx, p.PuzzleID, true, p.Expiration.Sub(tnow))
}
