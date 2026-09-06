package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type cleanupDBQuerier struct {
	*db.QuerierStub
	cacheErr    error
	sessionsErr error
}

func (q *cleanupDBQuerier) DeleteExpiredCache(context.Context) (int64, error) {
	return 0, q.cacheErr
}

func (q *cleanupDBQuerier) DeleteExpiredSessions(context.Context) (int64, error) {
	return 0, q.sessionsErr
}

func TestCleanupDBCacheJobRunsBothCleanups(t *testing.T) {
	cacheErr := errors.New("cache cleanup failed")
	sessionsErr := errors.New("session cleanup failed")

	for _, tc := range []struct {
		name        string
		cacheErr    error
		sessionsErr error
		wantErrors  []error
	}{
		{name: "Success"},
		{name: "CacheFailure", cacheErr: cacheErr, wantErrors: []error{cacheErr}},
		{name: "SessionFailure", sessionsErr: sessionsErr, wantErrors: []error{sessionsErr}},
		{name: "BothFail", cacheErr: cacheErr, sessionsErr: sessionsErr, wantErrors: []error{cacheErr, sessionsErr}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			querier := &cleanupDBQuerier{
				QuerierStub: &db.QuerierStub{},
				cacheErr:    tc.cacheErr,
				sessionsErr: tc.sessionsErr,
			}
			store := db.NewBusinessWithQuerier(nil, querier, db.NewStaticCache[db.CacheKey, any](1, &db.CacheMissingValue{}))
			job := &CleanupDBCacheJob{Store: store}

			err := job.RunOnce(t.Context(), job.NewParams())
			if len(tc.wantErrors) == 0 && err != nil {
				t.Fatalf("RunOnce() error = %v, want nil", err)
			}
			for _, wantErr := range tc.wantErrors {
				if !errors.Is(err, wantErr) {
					t.Errorf("RunOnce() error = %v, want it to contain %v", err, wantErr)
				}
			}
		})
	}
}
