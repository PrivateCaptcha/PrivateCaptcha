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
	ErrInvalidInput   = errors.New("invalid input")
	ErrRecordNotFound = errors.New("record not found")
	markerData        = [4]byte{0xCC, 0xCC, 0xCC, 0xCC}
)

type Store struct {
	db                    *dbgen.Queries
	cache                 common.Cache
	NegativeCacheDuration time.Duration
	PropertyCacheDuration time.Duration
	APIKeyCacheDuration   time.Duration
	PuzzleCacheDuration   time.Duration
}

type puzzleCacheMarker struct {
	Data [4]byte
}

func NewStore(queries *dbgen.Queries, cache common.Cache) *Store {
	// TODO: Adjust caching durations mindfully
	return &Store{
		db:                    queries,
		cache:                 cache,
		NegativeCacheDuration: 1 * time.Minute,
		PropertyCacheDuration: 1 * time.Minute,
		APIKeyCacheDuration:   1 * time.Minute,
		PuzzleCacheDuration:   1 * time.Minute,
	}
}

// Fetches property from DB, backed by cache
func (s *Store) RetrieveProperty(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	eid := UUIDFromSiteKey(sitekey)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	property := &dbgen.Property{}
	if err := s.cache.GetAndExpireItem(ctx, sitekey, s.PropertyCacheDuration, property); err == nil {
		return property, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	property, err := s.db.GetPropertyByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.cache.SetMissing(ctx, sitekey, s.NegativeCacheDuration)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by external ID", "sitekey", sitekey, common.ErrAttr(err))

		return nil, err
	}

	if property != nil {
		_ = s.cache.SetItem(ctx, sitekey, property, s.PropertyCacheDuration)
	}

	return property, nil
}

// Fetches API keyfrom DB, backed by cache
func (s *Store) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	eid := UUIDFromSecret(secret)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	apiKey := &dbgen.APIKey{}
	if err := s.cache.GetAndExpireItem(ctx, secret, s.APIKeyCacheDuration, apiKey); err == nil {
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
	marker := new(puzzleCacheMarker)
	key := hex.EncodeToString(p.Nonce[:])
	err := s.cache.GetAndExpireItem(ctx, key, s.PuzzleCacheDuration, marker)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to check if puzzle is cached", common.ErrAttr(err))
		return false
	}

	return bytes.Equal(marker.Data[:], markerData[:])
}

func (s *Store) CachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error {
	// this check should have been done before in the pipeline. Here the check only to safeguard storing in Redis
	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Skipping caching expired puzzle", "now", tnow, "expiration", p.Expiration)
		return nil
	}

	marker := &puzzleCacheMarker{
		Data: markerData,
	}
	key := hex.EncodeToString(p.Nonce[:])
	diff := p.Expiration.Sub(tnow)
	return s.cache.SetItem(ctx, key, marker, diff)
}
