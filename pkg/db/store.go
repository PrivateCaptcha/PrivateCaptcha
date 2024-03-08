package db

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrRecordNotFound   = errors.New("record not found")
	errInvalidCacheType = errors.New("cache record type does not match")
	markerData          = []byte{'{', '}'}
)

type Store struct {
	db                    *dbgen.Queries
	cache                 common.Cache
	NegativeCacheDuration time.Duration
	PropertyCacheDuration time.Duration
	APIKeyCacheDuration   time.Duration
	PuzzleCacheDuration   time.Duration
	cancelFunc            context.CancelFunc
}

type puzzleCacheMarker struct {
	Data [4]byte
}

func NewStore(queries *dbgen.Queries, cache common.Cache, cleanupInterval time.Duration) *Store {
	// TODO: Adjust caching durations mindfully
	s := &Store{
		db:                    queries,
		cache:                 cache,
		NegativeCacheDuration: 1 * time.Minute,
		PropertyCacheDuration: 1 * time.Minute,
		APIKeyCacheDuration:   1 * time.Minute,
		PuzzleCacheDuration:   1 * time.Minute,
	}

	var ctx context.Context
	ctx, s.cancelFunc = context.WithCancel(context.Background())
	go s.cleanupCache(ctx, cleanupInterval)

	return s
}

func (s *Store) Shutdown() {
	s.cancelFunc()
}

func (s *Store) cleanupCache(ctx context.Context, interval time.Duration) {
	slog.Debug("Store cache cleanup started")
	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
		case <-time.After(interval):
			err := s.db.DeleteExpiredCache(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to delete expired items", common.ErrAttr(err))
				continue
			}
		}
	}
	slog.Debug("Store cache cleanup finished")
}

func fetchCached[T any](ctx context.Context, cache common.Cache, key string, expiration time.Duration) (*T, error) {
	data, err := cache.GetAndExpireItem(ctx, key, expiration)
	if err != nil {
		return nil, err
	}

	if t, ok := data.(*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

// Fetches property from DB, backed by cache
func (s *Store) RetrievePropertyAndOrg(ctx context.Context, sitekey string) (*dbgen.PropertyAndOrgByExternalIDRow, error) {
	eid := UUIDFromSiteKey(sitekey)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	if property, err := fetchCached[dbgen.PropertyAndOrgByExternalIDRow](ctx, s.cache, sitekey, s.PropertyCacheDuration); err == nil {
		return property, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	propertyAndOrg, err := s.db.PropertyAndOrgByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.cache.SetMissing(ctx, sitekey, s.NegativeCacheDuration)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by external ID", "sitekey", sitekey, common.ErrAttr(err))

		return nil, err
	}

	if propertyAndOrg != nil {
		_ = s.cache.SetItem(ctx, sitekey, propertyAndOrg, s.PropertyCacheDuration)
	}

	return propertyAndOrg, nil
}

// Fetches API keyfrom DB, backed by cache
func (s *Store) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	eid := UUIDFromSecret(secret)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	if apiKey, err := fetchCached[dbgen.APIKey](ctx, s.cache, secret, s.APIKeyCacheDuration); err == nil {
		return apiKey, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	apiKey, err := s.db.GetAPIKeyByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.cache.SetMissing(ctx, secret, s.NegativeCacheDuration)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve API Key by external ID", "secret", secret, common.ErrAttr(err))

		return nil, err
	}

	if apiKey != nil {
		_ = s.cache.SetItem(ctx, secret, apiKey, s.APIKeyCacheDuration)
	}

	return apiKey, nil
}

func (s *Store) CheckPuzzleCached(ctx context.Context, p *puzzle.Puzzle) bool {
	key := hex.EncodeToString(p.Nonce[:])

	data, err := s.db.GetCachedByKey(ctx, key)
	if err == pgx.ErrNoRows {
		return false
	} else if err != nil {
		slog.ErrorContext(ctx, "Failed to check if puzzle is cached", common.ErrAttr(err))
		return false
	}

	return bytes.Equal(data[:], markerData[:])
}

func (s *Store) CachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error {
	// this check should have been done before in the pipeline. Here the check only to safeguard storing in Redis
	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Skipping caching expired puzzle", "now", tnow, "expiration", p.Expiration)
		return nil
	}

	key := hex.EncodeToString(p.Nonce[:])
	diff := p.Expiration.Sub(tnow)

	return s.db.CreateCache(ctx, &dbgen.CreateCacheParams{
		Key:     key,
		Value:   markerData[:],
		Column3: diff,
	})
}
