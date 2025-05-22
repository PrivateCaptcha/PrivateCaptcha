package common

import (
	"context"
	"net/http"
	"time"
)

type Cache[TKey comparable, TValue any] interface {
	Get(ctx context.Context, key TKey) (TValue, error)
	SetMissing(ctx context.Context, key TKey, ttl time.Duration) error
	Set(ctx context.Context, key TKey, t TValue, ttl time.Duration) error
	Delete(ctx context.Context, key TKey) error
}

type SessionStore interface {
	Init(ctx context.Context, session *Session) error
	Read(ctx context.Context, sid string) (*Session, error)
	Update(session *Session) error
	Destroy(ctx context.Context, sid string) error
	GC(ctx context.Context, d time.Duration)
}

type ConfigItem interface {
	Key() ConfigKey
	Value() string
}

type ConfigStore interface {
	Get(key ConfigKey) ConfigItem
	Update(ctx context.Context)
}

type TimeSeriesStore interface {
	Ping(ctx context.Context) error
	WriteAccessLogBatch(ctx context.Context, records []*AccessRecord) error
	WriteVerifyLogBatch(ctx context.Context, records []*VerifyRecord) error
	ReadPropertyStats(ctx context.Context, r *BackfillRequest, from time.Time) ([]*TimeCount, error)
	ReadAccountStats(ctx context.Context, userID int32, from time.Time) ([]*TimeCount, error)
	RetrievePropertyStats(ctx context.Context, orgID, propertyID int32, period TimePeriod) ([]*TimePeriodStat, error)
	DeletePropertiesData(ctx context.Context, propertyIDs []int32) error
	DeleteOrganizationsData(ctx context.Context, orgIDs []int32) error
	DeleteUsersData(ctx context.Context, userIDs []int32) error
}

type Metrics interface {
	Handler(h http.Handler) http.Handler
	HandlerFunc(handlerIDFunc func() string) func(http.Handler) http.Handler
	ObservePuzzleCreated(userID int32)
	ObservePuzzleVerified(userID int32, result string, isStub bool)
	ObserveHealth(postgres, clickhouse bool)
}
