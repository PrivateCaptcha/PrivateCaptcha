package db

import (
	"context"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/maypok86/otter/v2"
)

const (
	SitekeyLen         = 32
	APIKeyPrefix       = "pc_"
	SecretLen          = len(APIKeyPrefix) + SitekeyLen
	sessionCachePrefix = "session/"
)

var (
	invalidUUID = pgtype.UUID{Valid: false}
)

func IsInternalSubscription(source dbgen.SubscriptionSource) bool {
	switch source {
	case dbgen.SubscriptionSourceExternal:
		return false
	default:
		return true
	}
}

func Text(text string) pgtype.Text {
	return pgtype.Text{
		String: text,
		Valid:  true,
	}
}

func Int(i int32) pgtype.Int4 {
	return pgtype.Int4{Int32: i, Valid: true}
}

func Int8(i int64) pgtype.Int8 {
	return pgtype.Int8{Int64: i, Valid: true}
}

func Int2(i int16) pgtype.Int2 {
	return pgtype.Int2{Int16: i, Valid: true}
}

func Bool(b bool) pgtype.Bool {
	return pgtype.Bool{
		Bool:  b,
		Valid: true,
	}
}

func Timestampz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{Valid: false}
	}

	return pgtype.Timestamptz{
		Time:             t,
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}
}

func Date(t time.Time) pgtype.Date {
	return pgtype.Date{
		Time:             t,
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}
}

func UUIDToSiteKey(uuid pgtype.UUID) string {
	if !uuid.Valid {
		return ""
	}

	return hex.EncodeToString(uuid.Bytes[:])
}

func UUIDFromSiteKey(s string) pgtype.UUID {
	if len(s) != SitekeyLen {
		return invalidUUID
	}

	var result pgtype.UUID

	byteArray, err := hex.DecodeString(s)

	if (err == nil) && (len(byteArray) == len(result.Bytes)) {
		copy(result.Bytes[:], byteArray)
		result.Valid = true
		return result
	}

	return invalidUUID
}

func CanBeValidSitekey(sitekey string) bool {
	if len(sitekey) != SitekeyLen {
		return false
	}

	for _, c := range sitekey {
		//nolint:staticcheck
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}

	return true
}

func UUIDToSecret(uuid pgtype.UUID) string {
	if !uuid.Valid {
		return ""
	}

	return APIKeyPrefix + hex.EncodeToString(uuid.Bytes[:])
}

func UUIDToString(uuid pgtype.UUID) string {
	if !uuid.Valid {
		return ""
	}

	return hex.EncodeToString(uuid.Bytes[:])
}

func UUIDFromSecret(s string) pgtype.UUID {
	if !strings.HasPrefix(s, APIKeyPrefix) {
		return invalidUUID
	}

	s = strings.TrimPrefix(s, APIKeyPrefix)

	if len(s) != SitekeyLen {
		return invalidUUID
	}

	var result pgtype.UUID

	byteArray, err := hex.DecodeString(s)

	if (err == nil) && (len(byteArray) == len(result.Bytes)) {
		copy(result.Bytes[:], byteArray)
		result.Valid = true
		return result
	}

	return invalidUUID
}

func UUIDFromString(s string) pgtype.UUID {
	if len(s) != hex.EncodedLen(len(invalidUUID.Bytes)) {
		return invalidUUID
	}

	for _, c := range s {
		//nolint:staticcheck
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return invalidUUID
		}
	}

	var result pgtype.UUID

	byteArray, err := hex.DecodeString(s)

	if (err == nil) && (len(byteArray) == len(result.Bytes)) {
		copy(result.Bytes[:], byteArray)
		result.Valid = true
		return result
	}

	return invalidUUID
}

func FetchCachedOne[T any](ctx context.Context, cache common.Cache[CacheKey, any], key CacheKey) (*T, error) {
	data, err := cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if t, ok := data.(*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

func FetchCachedArray[T any](ctx context.Context, cache common.Cache[CacheKey, any], key CacheKey) ([]*T, error) {
	data, err := cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if t, ok := data.([]*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

func QueryKeyInt(ck CacheKey) (int32, error) {
	return ck.IntValue, nil
}

func QueryKeyString(ck CacheKey) (string, error) {
	return ck.StrValue, nil
}

func queryKeySecretUUID(key CacheKey) (pgtype.UUID, error) {
	result := UUIDFromSecret(key.StrValue)
	if !result.Valid {
		return result, ErrInvalidInput
	}

	return result, nil
}

func queryKeySitekeyUUID(key CacheKey) (pgtype.UUID, error) {
	result := UUIDFromSiteKey(key.StrValue)
	if !result.Valid {
		return result, ErrInvalidInput
	}

	return result, nil
}

func queryKeyStringUUID(key CacheKey) (pgtype.UUID, error) {
	result := UUIDFromString(key.StrValue)
	if !result.Valid {
		return result, ErrInvalidInput
	}

	return result, nil
}

func stringKeySitekeyUUID(key string) (pgtype.UUID, error) {
	result := UUIDFromSiteKey(key)
	if !result.Valid {
		return result, ErrInvalidInput
	}

	return result, nil
}

func sessionIDFunc(sid string) (string, error) {
	return sessionCachePrefix + sid, nil
}

func IdentityKeyFunc[TKey any](key TKey) (TKey, error) {
	return key, nil
}

func propertySitekeyFunc(p *dbgen.Property) string {
	return UUIDToSiteKey(p.ExternalID)
}

func propertyIDFunc(p *dbgen.Property) int32 {
	return p.ID
}

func QueryKeyPgInt(key CacheKey) (pgtype.Int4, error) {
	return Int(key.IntValue), nil
}

type StoreOneReader[TKey any, T any] struct {
	CacheKey     CacheKey
	QueryFunc    func(context.Context, TKey) (*T, error)
	QueryKeyFunc func(CacheKey) (TKey, error)
	Cache        common.Cache[CacheKey, any]
	TTL          time.Duration
	readFlag     int32
}

func (sf *StoreOneReader[TKey, T]) Reload(ctx context.Context, key CacheKey, old any) (any, error) {
	return sf.Load(ctx, key)
}

func (sf *StoreOneReader[TKey, T]) Load(ctx context.Context, key CacheKey) (any, error) {
	if sf.QueryFunc == nil {
		// in case of otter's refreshing, this should cause silent failure and eligibility for new refresh until item is expired
		// old item should be returned meanwhile
		return nil, ErrMaintenance
	}

	queryKey, err := sf.QueryKeyFunc(key)
	if err != nil {
		return nil, err
	}

	t, err := sf.QueryFunc(ctx, queryKey)
	if err != nil {
		if err == pgx.ErrNoRows {
			// this will cause cache to store this missing value and ultimately return ErrNegativeCacheHit
			// we do not return otter.ErrNotFound (as per docs), because in such case item will be purged from cache
			return sf.Cache.Missing(), nil
		}

		if err != otter.ErrNotFound {
			slog.ErrorContext(ctx, "Failed to query value from DB", "cacheKey", key, common.ErrAttr(err))
		}

		return nil, err
	}

	slog.Log(ctx, common.LevelTrace, "Retrieved entity from DB", "cacheKey", key)
	atomic.StoreInt32(&sf.readFlag, 1)

	return t, nil
}

func (sf *StoreOneReader[TKey, T]) Read(ctx context.Context) (*T, error) {
	// GetEx should not return errTransactionCache
	data, err := sf.Cache.GetEx(ctx, sf.CacheKey, sf)
	if err != nil {
		return nil, err
	}

	if t, ok := data.(*T); ok {
		slog.Log(ctx, common.LevelTrace, "Read object through cache", "cacheKey", sf.CacheKey)

		if (sf.TTL > 0) && (atomic.LoadInt32(&sf.readFlag) == 1) {
			_ = sf.Cache.SetTTL(ctx, sf.CacheKey, sf.TTL)
		}

		return t, nil
	}

	return nil, errInvalidCacheType
}

type StoreArrayReader[TKey any, T any] struct {
	CacheKey     CacheKey
	QueryFunc    func(context.Context, TKey) ([]*T, error)
	QueryKeyFunc func(CacheKey) (TKey, error)
	Cache        common.Cache[CacheKey, any]
	TTL          time.Duration
	readFlag     int32
}

func (sf *StoreArrayReader[TKey, T]) Reload(ctx context.Context, key CacheKey, old any) (any, error) {
	return sf.Load(ctx, key)
}

func (sf *StoreArrayReader[TKey, T]) Load(ctx context.Context, key CacheKey) (any, error) {
	if sf.QueryFunc == nil {
		// in case of otter's refreshing, this should cause silent failure and eligibility for new refresh until item is expired
		// old item should be returned meanwhile
		return nil, ErrMaintenance
	}

	queryKey, err := sf.QueryKeyFunc(key)
	if err != nil {
		return nil, err
	}

	t, err := sf.QueryFunc(ctx, queryKey)
	if err != nil {
		if err == pgx.ErrNoRows {
			// unlike in case of one, we want to store empty array here and not "missing" value
			// because "no rows" is a valid result for "WHERE" query
			return []*T{}, nil
		}

		if err != otter.ErrNotFound {
			slog.ErrorContext(ctx, "Failed to query entities from DB", "cacheKey", key, common.ErrAttr(err))
		}

		return nil, err
	}

	slog.Log(ctx, common.LevelTrace, "Retrieved entities from DB", "cacheKey", key, "count", len(t))
	atomic.StoreInt32(&sf.readFlag, 1)

	return t, nil
}

func (sf *StoreArrayReader[TKey, T]) Read(ctx context.Context) ([]*T, error) {
	// GetEx should not return errTransactionCache
	data, err := sf.Cache.GetEx(ctx, sf.CacheKey, sf)
	if err != nil {
		return nil, err
	}

	if t, ok := data.([]*T); ok {
		slog.Log(ctx, common.LevelTrace, "Read array through cache", "cacheKey", sf.CacheKey, "count", len(t))

		if (sf.TTL > 0) && (atomic.LoadInt32(&sf.readFlag) == 1) {
			_ = sf.Cache.SetTTL(ctx, sf.CacheKey, sf.TTL)
		}

		return t, nil
	}

	return nil, errInvalidCacheType
}

// this struct exists only to check if otter attempted loading OR refreshing the value
type cachedPropertyReader struct {
	sitekey     string
	cache       common.Cache[CacheKey, any]
	refreshFunc func(string)
}

// refreshing means that value is cached, however it has to be reloaded (which is what we are trying to detect)
func (sf *cachedPropertyReader) Reload(ctx context.Context, _ CacheKey, old any) (any, error) {
	if sf.refreshFunc != nil {
		sf.refreshFunc(sf.sitekey)
	}

	// we keep old value, but (hopefully) trigger a reload using refreshFunc
	return old, nil
}

// loading means value was not in cache - so we return otter.ErrNotFound anyways
func (sf *cachedPropertyReader) Load(ctx context.Context, _ CacheKey) (any, error) {
	return nil, otter.ErrNotFound
}

func (sf *cachedPropertyReader) Read(ctx context.Context) (*dbgen.Property, error) {
	cacheKey := PropertyBySitekeyCacheKey(sf.sitekey)

	data, err := sf.cache.GetEx(ctx, cacheKey, sf)
	if err != nil {
		return nil, err
	}

	if t, ok := data.(*dbgen.Property); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

// TODO: Refactor this to use otter.Cache BulkGet() API
type StoreBulkReader[TArg comparable, TKey any, T any] struct {
	ArgFunc         func(*T) TArg
	QueryFunc       func(context.Context, []TKey) ([]*T, error)
	QueryKeyFunc    func(TArg) (TKey, error)
	Cache           common.Cache[CacheKey, any]
	CacheKeyFunc    func(TArg) CacheKey
	MinMissingCount uint
}

// returns cached and fetched items separately
func (br *StoreBulkReader[TArg, TKey, T]) Read(ctx context.Context, args map[TArg]uint) ([]*T, []*T, error) {
	if len(args) == 0 {
		return []*T{}, []*T{}, nil
	}

	queryKeys := make([]TKey, 0, len(args))
	argsMap := make(map[TArg]struct{})
	cached := make([]*T, 0, len(args))
	anyInputError := false

	for arg := range args {
		cacheKey := br.CacheKeyFunc(arg)
		if t, err := FetchCachedOne[T](ctx, br.Cache, cacheKey); err == nil {
			cached = append(cached, t)
			continue
		} else if err == ErrNegativeCacheHit {
			continue
		}

		if key, err := br.QueryKeyFunc(arg); err == nil {
			queryKeys = append(queryKeys, key)
			argsMap[arg] = struct{}{}
		} else {
			slog.ErrorContext(ctx, "Failed to create query key", "arg", arg, common.ErrAttr(err))
			anyInputError = true
		}
	}

	if len(queryKeys) == 0 {
		if len(cached) > 0 {
			slog.DebugContext(ctx, "All items are cached", "count", len(cached))
			return cached, []*T{}, nil
		}

		slog.WarnContext(ctx, "No valid keys to fetch from DB")
		if anyInputError {
			return nil, nil, ErrInvalidInput
		}
		return nil, nil, ErrNegativeCacheHit
	}

	if br.QueryFunc == nil {
		return cached, []*T{}, ErrMaintenance
	}

	items, err := br.QueryFunc(ctx, queryKeys)
	if err != nil && err != pgx.ErrNoRows {
		slog.ErrorContext(ctx, "Failed to query items", "keys", len(queryKeys), common.ErrAttr(err))
		return cached, nil, err
	}

	slog.DebugContext(ctx, "Fetched items from DB", "count", len(items))

	for _, item := range items {
		arg := br.ArgFunc(item)
		delete(argsMap, arg)
	}

	for missingKey := range argsMap {
		// TODO: Switch to a probabilistic logic via an interface for negative caching
		if count, ok := args[missingKey]; ok && (count >= br.MinMissingCount) {
			cacheKey := br.CacheKeyFunc(missingKey)
			_ = br.Cache.SetMissing(ctx, cacheKey)
		}
	}

	return cached, items, nil
}
