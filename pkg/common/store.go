package common

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
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
	GetWithRefresh(ctx context.Context, key TKey) (TValue, bool, error)
	SetMissing(ctx context.Context, key TKey) error
	SetIfAbsent(ctx context.Context, key TKey, t TValue) error
	Set(ctx context.Context, key TKey, t TValue) error
	SetEx(ctx context.Context, key TKey, t TValue, ttl, refresh time.Duration) error
	SetTTL(ctx context.Context, key TKey, ttl time.Duration) error
	SetRefresh(ctx context.Context, key TKey, refresh time.Duration) error
	Delete(ctx context.Context, key TKey) bool
	SaveTo(ctx context.Context, w io.Writer, maxItems int) error
	LoadFrom(ctx context.Context, r io.Reader, ttl time.Duration) error
	Missing() TValue
	HitRatio() float64
	Clear()
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
	DropCache(ctx context.Context, tag string) error
	WriteAccessLogBatch(ctx context.Context, records []*AccessRecord) error
	WriteVerifyLogBatch(ctx context.Context, records []*VerifyRecord) error
	WriteFormSubmitBatch(ctx context.Context, records []*FormSubmitRecord) error
	RetrievePropertyStatsSince(ctx context.Context, r *BackfillRequest, from time.Time) ([]*TimeCount, error)
	RetrieveAccountStats(ctx context.Context, userID int32, from time.Time) ([]*OrgTimeCount, error)
	// Account report methods return a non-nil entry for every distinct positive user ID.
	RetrieveWeeklyAccountReportStats(ctx context.Context, userIDs []int32, from, mid, to time.Time) (map[int32]*UserReportAccountStats, error)
	RetrieveMonthlyAccountReportStats(ctx context.Context, userIDs []int32, from, mid, to time.Time) (map[int32]*UserReportAccountStats, error)
	RetrieveWeeklyPropertiesReportStats(ctx context.Context, userID int32, from, mid, to time.Time, options UserReportOptions) (*UserReportStats, error)
	RetrieveMonthlyPropertiesReportStats(ctx context.Context, userID int32, from, mid, to time.Time, options UserReportOptions) (*UserReportStats, error)
	RetrieveWeeklyFormsReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*UserFormsReportStats, error)
	RetrieveMonthlyFormsReportStats(ctx context.Context, userID int32, from, mid, to time.Time) (*UserFormsReportStats, error)
	RetrieveOrgStatsByPeriod(ctx context.Context, orgID int32, period TimePeriod, topPropertiesLimit int) (*OrgTimePeriodStats, error)
	RetrievePropertyStatsByPeriod(ctx context.Context, orgID, propertyID int32, period TimePeriod) ([]*TimePeriodStat, error)
	RetrieveFormStatsByPeriod(ctx context.Context, orgID, formID int32, period TimePeriod) ([]*FormSubmitStat, error)
	RetrieveFailingForms(ctx context.Context, threshold, limit int) ([]*FailingFormCandidate, error)
	RetrievePropertyRuleStatsByPeriod(ctx context.Context, userID, orgID, propertyID int32, period TimePeriod) ([]*TimeCount, error)
	RetrieveRecentTopProperties(ctx context.Context, limit int) (map[int32]uint, error)
	DeleteFormsData(ctx context.Context, formIDs []int32) error
	DeletePropertiesData(ctx context.Context, propertyIDs []int32) error
	DeleteOrganizationsData(ctx context.Context, orgIDs []int32) error
	DeleteUsersData(ctx context.Context, userIDs []int32) error
}

type PlatformMetrics interface {
	ObserveHealth(postgres, clickhouse bool)
	ObserveCacheHitRatio(ratio float64)
	ObserveQueryDuration(duration float64)
	ObserveQueryError()
}

type MetricEventType string

const (
	PuzzleEventType        MetricEventType = "puzzle"
	VerifyEventType        MetricEventType = "verify"
	UserLimitEventType     MetricEventType = "user_limit"
	SessionEventType       MetricEventType = "session"
	SitekeyEventType       MetricEventType = "sitekey"
	APIKeyEventType        MetricEventType = "apikey"
	PropertyRulesEventType MetricEventType = "property_rules"
	FormEventType          MetricEventType = "form"
	FormLogEventType       MetricEventType = "form_log"
)

type BaseMetrics interface {
	ObserveEventDropped(eventType MetricEventType)
	ObservePanic()
}

type APIMetrics interface {
	BaseMetrics
	APIHandler(h http.Handler) http.Handler
	APIHandlerIDFunc(handlerIDFunc func() string) func(http.Handler) http.Handler
	ObservePuzzleCreated(userID int32)
	ObservePuzzleVerified(userID int32, result string, isStub bool)
	ObserveApiError(handlerID string, method string, code int)
}

type PortalMetrics interface {
	BaseMetrics
	PortalHandler(h http.Handler) http.Handler
	PortalHandlerIDFunc(handlerIDFunc func() string) func(http.Handler) http.Handler
	// this method is used for our error page redirects that are not captured by usual monitoring middleware
	// as we don't actually return an HTTP error out
	ObserveHttpError(handlerID string, method string, code int)
}

type AuditLog interface {
	RecordEvent(ctx context.Context, event *AuditLogEvent, source AuditLogSource)
	RecordEvents(ctx context.Context, events []*AuditLogEvent, source AuditLogSource)
}

type EmailVerifier interface {
	VerifyEmail(ctx context.Context, email string) error
}

type FormURLVerifier interface {
	VerifyURL(ctx context.Context, rawURL string) error
	VerifyResolvedAddress(ctx context.Context, host string, ip netip.Addr) error
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type LicenseService interface {
	IsRegistered() bool
}
