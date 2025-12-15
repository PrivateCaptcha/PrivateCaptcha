package common

import (
	"context"
	"net/http"
	"time"
)

type IdentifierHasher interface {
	Encrypt(id int) string
	Encrypt64(id int64) string
	Decrypt(id string) (int, error)
	Decrypt64(id string) (int64, error)
}

// this is an exact copy of otter's Loader
type CacheLoader[K comparable, V any] interface {
	Load(ctx context.Context, key K) (V, error)
	Reload(ctx context.Context, key K, oldValue V) (V, error)
}

type Cache[TKey comparable, TValue any] interface {
	Get(ctx context.Context, key TKey) (TValue, error)
	GetEx(ctx context.Context, key TKey, loader CacheLoader[TKey, TValue]) (TValue, error)
	SetMissing(ctx context.Context, key TKey) error
	Set(ctx context.Context, key TKey, t TValue) error
	SetWithTTL(ctx context.Context, key TKey, t TValue, ttl time.Duration) error
	SetTTL(ctx context.Context, key TKey, ttl time.Duration) error
	Delete(ctx context.Context, key TKey) bool
	Missing() TValue
	HitRatio() float64
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
	RetrievePropertyStatsSince(ctx context.Context, r *BackfillRequest, from time.Time) ([]*TimeCount, error)
	RetrieveAccountStats(ctx context.Context, userID int32, from time.Time) ([]*TimeCount, error)
	RetrievePropertyStatsByPeriod(ctx context.Context, orgID, propertyID int32, period TimePeriod) ([]*TimePeriodStat, error)
	RetrieveRecentTopProperties(ctx context.Context, limit int) (map[int32]uint, error)
	DeletePropertiesData(ctx context.Context, propertyIDs []int32) error
	DeleteOrganizationsData(ctx context.Context, orgIDs []int32) error
	DeleteUsersData(ctx context.Context, userIDs []int32) error
}

type PlatformMetrics interface {
	ObserveHealth(postgres, clickhouse bool)
	ObserveCacheHitRatio(ratio float64)
}

type HTTPMetrics interface {
	Handler(h http.Handler) http.Handler
	HandlerIDFunc(handlerIDFunc func() string) func(http.Handler) http.Handler
}

type APIMetrics interface {
	HTTPMetrics
	ObservePuzzleCreated(userID int32)
	ObservePuzzleVerified(userID int32, result string, isStub bool)
	ObserveApiError(handlerID string, method string, code int)
}

type PortalMetrics interface {
	HTTPMetrics
	// this method is used for our error page redirects that are not captured by usual monitoring middleware
	// as we don't actually return an HTTP error out
	ObserveHttpError(handlerID string, method string, code int)
}

type AuditLog interface {
	RecordEvent(ctx context.Context, event *AuditLogEvent, source AuditLogSource)
	RecordEvents(ctx context.Context, events []*AuditLogEvent, source AuditLogSource)
}
