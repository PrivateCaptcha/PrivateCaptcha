package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/maypok86/otter/v2"
	"github.com/maypok86/otter/v2/stats"
)

var (
	ErrNegativeCacheHit    = errors.New("negative hit")
	ErrCacheMiss           = errors.New("cache miss")
	ErrSetMissing          = errors.New("cannot set missing value directly")
	errEmptyCacheKeyPrefix = errors.New("cache key prefix is empty")
)

type memcache[TKey comparable, TValue comparable] struct {
	name    string
	store   *otter.Cache[TKey, TValue]
	counter *stats.Counter
	// TODO: Evaluate storing negative cache separately from the main one
	// with much smaller max size
	missingValue TValue
	missingTTL   time.Duration
}

type pcOtterLogger struct{}

func (pcOtterLogger) Warn(ctx context.Context, msg string, err error) {
	slog.WarnContext(ctx, msg, "source", "otter", common.ErrAttr(err))
}
func (pcOtterLogger) Error(ctx context.Context, msg string, err error) {
	slog.ErrorContext(ctx, msg, "source", "otter", common.ErrAttr(err))
}

type OtterCacheOption[TKey comparable, TValue comparable] func(*otter.Options[TKey, TValue])

func NewMemoryCache[TKey comparable, TValue comparable](name string, maxCacheSize int, missingValue TValue, expiryTTL, refreshTTL, missingTTL time.Duration) (*memcache[TKey, TValue], error) {
	return NewMemoryCacheEx[TKey, TValue](name, maxCacheSize, missingValue, missingTTL,
		func(o *otter.Options[TKey, TValue]) {
			o.ExpiryCalculator = otter.ExpiryAccessing[TKey, TValue](10 * time.Minute)
			o.RefreshCalculator = otter.RefreshWriting[TKey, TValue](5 * time.Minute)
		})
}

func NewMemoryCacheEx[TKey comparable, TValue comparable](
	name string,
	maxCacheSize int,
	missingValue TValue,
	missingTTL time.Duration,
	opts ...OtterCacheOption[TKey, TValue],
) (*memcache[TKey, TValue], error) {

	counter := stats.NewCounter()

	// Defaults
	cfg := &otter.Options[TKey, TValue]{
		MaximumSize:     maxCacheSize,
		InitialCapacity: max(100, maxCacheSize/1000),
		StatsRecorder:   counter,
		Logger:          &pcOtterLogger{},
	}

	// Apply user-supplied overrides
	for _, opt := range opts {
		opt(cfg)
	}

	store, err := otter.New(cfg)
	if err != nil {
		return nil, err
	}

	return &memcache[TKey, TValue]{
		name:         name,
		store:        store,
		counter:      counter,
		missingValue: missingValue,
		missingTTL:   missingTTL,
	}, nil
}

var _ common.Cache[int, any] = (*memcache[int, any])(nil)

func (c *memcache[TKey, TValue]) Missing() TValue {
	return c.missingValue
}

func (c *memcache[TKey, TValue]) HitRatio() float64 {
	return c.counter.Snapshot().HitRatio()
}

func (c *memcache[TKey, TValue]) Get(ctx context.Context, key TKey) (TValue, error) {
	data, found := c.store.GetIfPresent(key)
	if !found {
		slog.Log(ctx, common.LevelTrace, "Item not found in memory cache", "cache", c.name, "key", key)
		var zero TValue
		return zero, ErrCacheMiss
	}

	if data == c.missingValue {
		slog.Log(ctx, common.LevelTrace, "Item set as missing in memory cache", "cache", c.name, "key", key)
		var zero TValue
		return zero, ErrNegativeCacheHit
	}

	slog.Log(ctx, common.LevelTrace, "Found item in memory cache", "cache", c.name, "key", key)

	return data, nil
}

func (c *memcache[TKey, TValue]) GetEx(ctx context.Context, key TKey, loader common.CacheLoader[TKey, TValue]) (TValue, error) {
	data, err := c.store.Get(ctx, key, loader)
	if err != nil {
		if errors.Is(err, otter.ErrNotFound) {
			slog.Log(ctx, common.LevelTrace, "Item not found in memory cache", "cache", c.name, "key", key)

			var zero TValue
			return zero, ErrCacheMiss
		}

		slog.ErrorContext(ctx, "Failed to get item from memory cache", "cache", c.name, "key", key, common.ErrAttr(err))

		return data, err
	}

	if data == c.missingValue {
		// we force-set TTL as it means loader function returned missing value, in contrast to using function SetMission()
		c.store.SetExpiresAfter(key, c.missingTTL)
		slog.Log(ctx, common.LevelTrace, "Item set as missing in memory cache", "cache", c.name, "key", key)
		var zero TValue
		return zero, ErrNegativeCacheHit
	}

	slog.Log(ctx, common.LevelTrace, "Found item in memory cache", "cache", c.name, "key", key)

	return data, nil
}

func (c *memcache[TKey, TValue]) SetMissing(ctx context.Context, key TKey) error {
	c.store.Set(key, c.missingValue)
	c.store.SetExpiresAfter(key, c.missingTTL)

	slog.Log(ctx, common.LevelTrace, "Set item as missing in memory cache", "cache", c.name, "key", key)

	return nil
}

func (c *memcache[TKey, TValue]) Set(ctx context.Context, key TKey, t TValue) error {
	if t == c.missingValue {
		return ErrSetMissing
	}

	c.store.Set(key, t)

	slog.Log(ctx, common.LevelTrace, "Saved item to memory cache", "cache", c.name, "key", key)

	return nil
}

func (c *memcache[TKey, TValue]) SetWithTTL(ctx context.Context, key TKey, t TValue, ttl time.Duration) error {
	if t == c.missingValue {
		return ErrSetMissing
	}

	c.store.Set(key, t)
	c.store.SetExpiresAfter(key, ttl)

	slog.Log(ctx, common.LevelTrace, "Saved item to memory cache", "cache", c.name, "key", key, "ttl", ttl)

	return nil
}

func (c *memcache[TKey, TValue]) SetTTL(ctx context.Context, key TKey, ttl time.Duration) error {
	c.store.SetExpiresAfter(key, ttl)

	slog.Log(ctx, common.LevelTrace, "Set item TTL", "cache", c.name, "key", key, "ttl", ttl)

	return nil
}

func (c *memcache[TKey, TValue]) Delete(ctx context.Context, key TKey) bool {
	_, found := c.store.Invalidate(key)

	slog.Log(ctx, common.LevelTrace, "Deleted item from memory cache", "cache", c.name, "key", key, "found", found)

	return found
}

type CacheKeyPrefix byte

const (
	userCacheKeyPrefix CacheKeyPrefix = iota
	apiKeyCacheKeyPrefix
	orgCacheKeyPrefix
	orgPropertiesCacheKeyPrefix
	propertyByIDCacheKeyPrefix
	propertyBySitekeyCacheKeyPrefix
	userOrgsCacheKeyPrefix
	orgUsersCacheKeyPrefix
	userAPIKeysCacheKeyPrefix
	subscriptionCacheKeyPrefix
	notificationCacheKeyPrefix
	templateCacheKeyPrefix
	sessionCacheKeyPrefix
	userAuditLogsCacheKeyPrefix
	propertyAuditLogsCacheKeyPrefix
	orgAuditLogsCacheKeyPrefix
	userPropertiesCountCachePrefix
	userAccountStatsCachePrefix
	propertyStatsCachePrefix
	asyncTaskCachePrefix
	// Add new fields _above_
	CACHE_KEY_PREFIXES_COUNT
)

var (
	cachePrefixToStrings []string
	cachePrefixMux       sync.Mutex
)

func init() {
	cachePrefixMux.Lock()
	defer cachePrefixMux.Unlock()

	if len(cachePrefixToStrings) < int(CACHE_KEY_PREFIXES_COUNT) {
		cachePrefixToStrings = make([]string, CACHE_KEY_PREFIXES_COUNT)
	}

	cachePrefixToStrings[userCacheKeyPrefix] = "user/"
	cachePrefixToStrings[apiKeyCacheKeyPrefix] = "apikey/"
	cachePrefixToStrings[orgCacheKeyPrefix] = "org/"
	cachePrefixToStrings[orgPropertiesCacheKeyPrefix] = "orgProperties/"
	cachePrefixToStrings[propertyByIDCacheKeyPrefix] = "propID/"
	cachePrefixToStrings[propertyBySitekeyCacheKeyPrefix] = "propSitekey/"
	cachePrefixToStrings[userOrgsCacheKeyPrefix] = "userOrgs/"
	cachePrefixToStrings[orgUsersCacheKeyPrefix] = "orgUsers/"
	cachePrefixToStrings[userAPIKeysCacheKeyPrefix] = "userApiKeys/"
	cachePrefixToStrings[subscriptionCacheKeyPrefix] = "subscr/"
	cachePrefixToStrings[notificationCacheKeyPrefix] = "notif/"
	cachePrefixToStrings[templateCacheKeyPrefix] = "template/"
	cachePrefixToStrings[sessionCacheKeyPrefix] = "session/"
	cachePrefixToStrings[userAuditLogsCacheKeyPrefix] = "userAuditLogs/"
	cachePrefixToStrings[propertyAuditLogsCacheKeyPrefix] = "propAuditLogs/"
	cachePrefixToStrings[orgAuditLogsCacheKeyPrefix] = "orgAuditLogs/"
	cachePrefixToStrings[userPropertiesCountCachePrefix] = "userPropertiesCount/"
	cachePrefixToStrings[userAccountStatsCachePrefix] = "userAccountStats/"
	cachePrefixToStrings[propertyStatsCachePrefix] = "propertyStats/"
	cachePrefixToStrings[asyncTaskCachePrefix] = "asyncTask/"

	for i, v := range cachePrefixToStrings {
		if len(v) == 0 {
			panic(fmt.Sprintf("found unconfigured value for key: %v", i))
		}
	}
}

func RegisterCachePrefixString(prefix CacheKeyPrefix, s string) error {
	if len(s) == 0 {
		return errEmptyCacheKeyPrefix
	}

	cachePrefixMux.Lock()
	defer cachePrefixMux.Unlock()

	if int(prefix) >= len(cachePrefixToStrings) {
		newSlice := make([]string, int(prefix)+1)
		copy(newSlice, cachePrefixToStrings)
		cachePrefixToStrings = newSlice
	}

	if cachePrefixToStrings[prefix] != "" {
		return fmt.Errorf("cache: duplicate registration for prefix %v", prefix)
	}

	cachePrefixToStrings[prefix] = s
	return nil
}

// it's a "union" type which is better than doing string concatenation as before
type CacheKey struct {
	Prefix   CacheKeyPrefix
	IntValue int32
	StrValue string
}

func (ck CacheKey) String() string {
	var prefix string
	if int(ck.Prefix) < len(cachePrefixToStrings) {
		prefix = cachePrefixToStrings[ck.Prefix]
	}

	if len(ck.StrValue) != 0 {
		return prefix + ck.StrValue
	}

	return prefix + strconv.Itoa(int(ck.IntValue))
}

func (ck CacheKey) LogValue() slog.Value {
	return slog.StringValue(ck.String())
}

func Int32CacheKey(prefix CacheKeyPrefix, value int32) CacheKey {
	return CacheKey{
		Prefix:   prefix,
		IntValue: value,
		StrValue: "",
	}
}

func StringCacheKey(prefix CacheKeyPrefix, value string) CacheKey {
	return CacheKey{
		Prefix:   prefix,
		IntValue: 0,
		StrValue: value,
	}
}

func SessionCacheKey(id string) CacheKey { return StringCacheKey(sessionCacheKeyPrefix, id) }
func UserCacheKey(id int32) CacheKey     { return Int32CacheKey(userCacheKeyPrefix, id) }
func APIKeyCacheKey(str string) CacheKey { return StringCacheKey(apiKeyCacheKeyPrefix, str) }
func orgCacheKey(orgID int32) CacheKey   { return Int32CacheKey(orgCacheKeyPrefix, orgID) }
func orgPropertiesCacheKey(orgID int32) CacheKey {
	return Int32CacheKey(orgPropertiesCacheKeyPrefix, orgID)
}
func propertyByIDCacheKey(propID int32) CacheKey {
	return Int32CacheKey(propertyByIDCacheKeyPrefix, propID)
}
func PropertyBySitekeyCacheKey(sitekey string) CacheKey {
	return StringCacheKey(propertyBySitekeyCacheKeyPrefix, sitekey)
}
func userOrgsCacheKey(userID int32) CacheKey { return Int32CacheKey(userOrgsCacheKeyPrefix, userID) }
func orgUsersCacheKey(orgID int32) CacheKey  { return Int32CacheKey(orgUsersCacheKeyPrefix, orgID) }
func UserAPIKeysCacheKey(userID int32) CacheKey {
	return Int32CacheKey(userAPIKeysCacheKeyPrefix, userID)
}
func SubscriptionCacheKey(sID int32) CacheKey { return Int32CacheKey(subscriptionCacheKeyPrefix, sID) }
func notificationCacheKey(ID int32) CacheKey  { return Int32CacheKey(notificationCacheKeyPrefix, ID) }
func templateCacheKey(str string) CacheKey    { return StringCacheKey(templateCacheKeyPrefix, str) }
func userAuditLogsCacheKey(userID int32, key string) CacheKey {
	return CacheKey{Prefix: userAuditLogsCacheKeyPrefix, IntValue: userID, StrValue: key}
}
func propertyAuditLogsCacheKey(propID int32) CacheKey {
	return CacheKey{Prefix: propertyAuditLogsCacheKeyPrefix, IntValue: propID}
}
func orgAuditLogsCacheKey(orgID int32) CacheKey {
	return CacheKey{Prefix: orgAuditLogsCacheKeyPrefix, IntValue: orgID}
}
func userPropertiesCountCacheKey(userID int32) CacheKey {
	return Int32CacheKey(userPropertiesCountCachePrefix, userID)
}
func userAccountStatsCacheKey(userID int32, key string) CacheKey {
	return CacheKey{Prefix: userAccountStatsCachePrefix, IntValue: userID, StrValue: key}
}
func propertyStatsCacheKey(propertyID int32, key string) CacheKey {
	return CacheKey{Prefix: propertyStatsCachePrefix, IntValue: propertyID, StrValue: key}
}
func asyncTaskCacheKey(key string) CacheKey {
	return StringCacheKey(asyncTaskCachePrefix, key)
}
