package db

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidInput   = errors.New("invalid input")
	ErrRecordNotFound = errors.New("record not found")
)

type Store struct {
	db                    *dbgen.Queries
	cache                 common.Cache
	NegativeCacheDuration time.Duration
	PropertyCacheDuration time.Duration
	APIKeyCacheDuration   time.Duration
}

func NewStore(queries *dbgen.Queries, cache common.Cache) *Store {
	// TODO: Adjust caching durations mindfully
	return &Store{
		db:                    queries,
		cache:                 cache,
		NegativeCacheDuration: 1 * time.Minute,
		PropertyCacheDuration: 1 * time.Minute,
	}
}

// Fetches property from DB, backed by cache
func (s *Store) RetrieveProperty(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	eid := UUIDFromSiteKey(sitekey)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	property := &dbgen.Property{}
	if err := s.cache.GetItem(ctx, sitekey, property); err == nil {
		// TODO: Check if we need to update expiration at all times
		_ = s.cache.UpdateExpiration(ctx, sitekey, s.PropertyCacheDuration)
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

	_ = s.cache.SetItem(ctx, sitekey, property, s.PropertyCacheDuration)

	return property, nil
}

func (s *Store) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	eid := UUIDFromSecret(secret)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	apiKey := &dbgen.APIKey{}
	if err := s.cache.GetItem(ctx, secret, apiKey); err == nil {
		_ = s.cache.UpdateExpiration(ctx, secret, s.APIKeyCacheDuration)
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

	_ = s.cache.SetItem(ctx, secret, apiKey, s.APIKeyCacheDuration)

	return apiKey, nil
}
