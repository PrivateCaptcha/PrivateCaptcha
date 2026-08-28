package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type cleanupQuerier struct {
	*db.QuerierStub
	cacheErr   error
	sessionErr error
}

func (q *cleanupQuerier) DeleteExpiredCache(context.Context) (int64, error) {
	return 0, q.cacheErr
}

func (q *cleanupQuerier) DeleteExpiredSessions(context.Context) (int64, error) {
	return 1, q.sessionErr
}

func TestCleanupDBCacheJobAttemptsBothCleanupsAfterFailure(t *testing.T) {
	cacheErr := errors.New("cache cleanup failed")
	sessionErr := errors.New("session cleanup failed")
	querier := &cleanupQuerier{QuerierStub: &db.QuerierStub{}, cacheErr: cacheErr, sessionErr: sessionErr}
	store := db.NewBusinessWithQuerier(nil, querier, db.NewStaticCache[db.CacheKey, any](10, &db.CacheMissingValue{}))
	job := &CleanupDBCacheJob{Store: store}
	err := job.RunOnce(t.Context(), job.NewParams())
	for _, expected := range []error{cacheErr, sessionErr} {
		if !errors.Is(err, expected) {
			t.Fatalf("cleanup error %v does not include %v", err, expected)
		}
	}
}
