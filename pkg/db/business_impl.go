package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/maypok86/otter/v2"
)

const (
	// NOTE: this is the time during which changes to difficulty will propagate when we have multiple API nodes
	propertyTTL              = 1 * time.Hour
	apiKeyTTL                = 12 * time.Hour
	asyncTaskTTL             = 1 * time.Minute
	OrgPropertiesPageSize    = 30
	orgPropertiesCacheKeyStr = "0" // "0" as in "first page"
)

var (
	errTransactionCache = errors.New("cache is not supported during transaction")
	// shortcuts for nullable access levels
	nullAccessLevelNull   = dbgen.NullAccessLevel{Valid: false}
	nullAccessLevelOwner  = dbgen.NullAccessLevel{Valid: true, AccessLevel: dbgen.AccessLevelOwner}
	nullAccessLevelMember = dbgen.NullAccessLevel{Valid: true, AccessLevel: dbgen.AccessLevelMember}
)

type txCacheArg struct {
	item any
	ttl  time.Duration
}

type TxCache struct {
	set     map[CacheKey]*txCacheArg
	del     map[CacheKey]struct{}
	missing map[CacheKey]struct{}
}

func NewTxCache() *TxCache {
	return &TxCache{
		set:     make(map[CacheKey]*txCacheArg),
		del:     make(map[CacheKey]struct{}),
		missing: make(map[CacheKey]struct{}),
	}
}

var _ common.Cache[CacheKey, any] = (*TxCache)(nil)

func (c *TxCache) HitRatio() float64 { return 0.0 }
func (c *TxCache) Missing() any      { return nil }
func (c *TxCache) Get(ctx context.Context, key CacheKey) (any, error) {
	return nil, errTransactionCache
}
func (c *TxCache) GetEx(ctx context.Context, key CacheKey, loader common.CacheLoader[CacheKey, any]) (any, error) {
	return loader.Load(ctx, key)
}
func (c *TxCache) SetMissing(ctx context.Context, key CacheKey) error {
	c.missing[key] = struct{}{}
	return nil
}
func (c *TxCache) Set(ctx context.Context, key CacheKey, t any) error {
	c.set[key] = &txCacheArg{item: t}
	return nil
}
func (c *TxCache) SetWithTTL(ctx context.Context, key CacheKey, t any, ttl time.Duration) error {
	c.set[key] = &txCacheArg{item: t, ttl: ttl}
	return nil
}

func (c *TxCache) SetTTL(ctx context.Context, key CacheKey, ttl time.Duration) error {
	if item, ok := c.set[key]; ok {
		item.ttl = ttl
		return nil
	}
	return ErrRecordNotFound
}
func (c *TxCache) Delete(ctx context.Context, key CacheKey) bool {
	c.del[key] = struct{}{}
	return true
}

func (c *TxCache) Commit(ctx context.Context, cache common.Cache[CacheKey, any]) {
	for key := range c.del {
		if deleted := cache.Delete(ctx, key); !deleted {
			slog.WarnContext(ctx, "Cache item to delete was not found", "key", key)
		}
	}

	for key := range c.missing {
		if err := cache.SetMissing(ctx, key); err != nil {
			slog.ErrorContext(ctx, "Failed to set missing in cache", "key", key, common.ErrAttr(err))
		}
	}

	for key, value := range c.set {
		var err error
		if value.ttl > 0 {
			err = cache.SetWithTTL(ctx, key, value.item, value.ttl)
		} else {
			err = cache.Set(ctx, key, value.item)
		}
		if err != nil {
			slog.ErrorContext(ctx, "Failed to set in cache", "key", key, common.ErrAttr(err))
		}
	}
}

type BusinessStoreImpl struct {
	querier dbgen.Querier
	cache   common.Cache[CacheKey, any]
}

func (impl *BusinessStoreImpl) RetrieveFromCache(ctx context.Context, key string) ([]byte, error) {
	if len(key) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	data, err := impl.querier.GetCachedByKey(ctx, key)
	if err == pgx.ErrNoRows {
		return nil, ErrCacheMiss
	} else if err != nil {
		slog.ErrorContext(ctx, "Failed to read from cache", "key", key, common.ErrAttr(err))
		return nil, err
	}

	return data, nil
}

func (impl *BusinessStoreImpl) StoreInCache(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	if (len(key) == 0) || (len(data) == 0) || (ttl == 0) {
		return ErrInvalidInput
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.CreateCache(ctx, &dbgen.CreateCacheParams{
		Key:     key,
		Value:   data,
		Column3: ttl,
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to write to cache", "key", key, common.ErrAttr(err))
		return err
	}

	return nil
}

func (impl *BusinessStoreImpl) ping(ctx context.Context) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	v, err := impl.querier.Ping(ctx)
	if err != nil {
		return err
	}
	slog.Log(ctx, common.LevelTrace, "Pinged Postgres", "result", v)
	return nil
}

func (impl *BusinessStoreImpl) DeleteExpiredCache(ctx context.Context) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	return impl.querier.DeleteExpiredCache(ctx)
}

func (impl *BusinessStoreImpl) CreateNewSubscription(ctx context.Context, params *dbgen.CreateSubscriptionParams) (*dbgen.Subscription, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	subscription, err := impl.querier.CreateSubscription(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create a subscription in DB", common.ErrAttr(err))
		return nil, err
	}

	if subscription != nil {
		slog.InfoContext(ctx, "Created new subscription", "subscriptionID", subscription.ID,
			"externalSubscriptionID", subscription.ExternalSubscriptionID.String)

		cacheKey := SubscriptionCacheKey(subscription.ID)
		_ = impl.cache.Set(ctx, cacheKey, subscription)
	}

	return subscription, nil
}

func (impl *BusinessStoreImpl) createNewUser(ctx context.Context, email, name string, subscription *dbgen.Subscription) (*dbgen.User, *common.AuditLogEvent, error) {
	if len(email) == 0 {
		return nil, nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	if len(name) == 0 {
		name = fmt.Sprintf("User_%v", time.Now().UTC().UnixMilli())
	}

	params := &dbgen.CreateUserParams{
		Name:  name,
		Email: email,
	}

	if subscription != nil {
		params.SubscriptionID = Int(subscription.ID)
	}

	user, err := impl.querier.CreateUser(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create user in DB", "email", email, common.ErrAttr(err))
		return nil, nil, err
	}

	var auditEvent *common.AuditLogEvent

	if user != nil {
		slog.InfoContext(ctx, "Created user in DB", "email", email, "id", user.ID)

		// we need to update cache as we just set user as missing when checking for it's existence
		cacheKey := UserCacheKey(user.ID)
		_ = impl.cache.Set(ctx, cacheKey, user)

		auditEvent = newUserAuditLogEvent(user, subscription, common.AuditLogActionCreate)
	}

	return user, auditEvent, nil
}

func (impl *BusinessStoreImpl) CreateNewOrganization(ctx context.Context, name string, userID int32) (*dbgen.Organization, *common.AuditLogEvent, error) {
	if len(name) == 0 {
		return nil, nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	org, err := impl.querier.CreateOrganization(ctx, &dbgen.CreateOrganizationParams{
		Name:   name,
		UserID: Int(userID),
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create organization in DB", "name", name, common.ErrAttr(err))
		return nil, nil, err
	}

	var auditEvent *common.AuditLogEvent

	if org != nil {
		slog.InfoContext(ctx, "Created organization in DB", "name", name, "id", org.ID)

		cacheKey := orgCacheKey(org.ID)
		_ = impl.cache.Set(ctx, cacheKey, org)

		// invalidate user orgs in cache as we just created another one
		_ = impl.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))

		auditEvent = newOrgAuditLogEvent(userID, org, common.AuditLogActionCreate)
	}

	return org, auditEvent, nil
}

func (impl *BusinessStoreImpl) SoftDeleteUser(ctx context.Context, user *dbgen.User) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	user, err := impl.querier.SoftDeleteUser(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user", "userID", user.ID, common.ErrAttr(err))
		return nil, err
	} else {
		slog.InfoContext(ctx, "Soft-deleted user", "userID", user.ID)
	}

	if err := impl.querier.SoftDeleteUserOrganizations(ctx, Int(user.ID)); err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user organizations", "userID", user.ID, common.ErrAttr(err))
		return nil, err
	} else {
		slog.InfoContext(ctx, "Soft-deleted user organizations", "userID", user.ID)
	}

	if err := impl.querier.DeleteUserAPIKeys(ctx, Int(user.ID)); err != nil {
		slog.ErrorContext(ctx, "Failed to delete user API keys", "userID", user.ID, common.ErrAttr(err))
		return nil, err
	} else {
		slog.InfoContext(ctx, "Deleted user API keys", "userID", user.ID)
	}

	// TODO: Delete user API keys from cache

	// invalidate user caches
	userOrgsCacheKey := userOrgsCacheKey(user.ID)
	if orgs, err := FetchCachedArray[dbgen.GetUserOrganizationsRow](ctx, impl.cache, userOrgsCacheKey); err == nil {
		for _, org := range orgs {
			_ = impl.cache.Delete(ctx, orgCacheKey(org.Organization.ID))
			_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(org.Organization.ID, orgPropertiesCacheKeyStr))
		}
		_ = impl.cache.Delete(ctx, userOrgsCacheKey)
	}

	_ = impl.cache.Delete(ctx, UserCacheKey(user.ID))

	auditEvent := newUserAuditLogEvent(user, nil, common.AuditLogActionSoftDelete)

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) doGetSessionbyID(ctx context.Context, sid string) (*session.SessionData, error) {
	sslog := slog.With(common.SessionIDAttr(sid))
	sessionID, _ := sessionIDFunc(sid)
	data, err := impl.RetrieveFromCache(ctx, sessionID)
	if (err == nil) && (len(data) > 0) {
		sslog.DebugContext(ctx, "Found session data cached in DB")
		sd := session.NewSessionData(sid)
		if uerr := sd.UnmarshalBinary(data); uerr != nil {
			sslog.ErrorContext(ctx, "Failed to unmarshal session data from cache", common.ErrAttr(uerr))
			return nil, uerr
		}

		sslog.Log(ctx, common.LevelTrace, "Unmarshaled session data from binary", "fields", sd.Size())

		return sd, nil
	}

	if err == ErrCacheMiss {
		sslog.DebugContext(ctx, "Session data not found cached in DB")
		// this will cause item to be purged from otter cache, should it still be there
		return nil, otter.ErrNotFound
	}

	sslog.ErrorContext(ctx, "Failed to read session data from DB cache", "size", len(data), common.ErrAttr(err))

	return nil, err
}

func (impl *BusinessStoreImpl) DeleteUserSession(ctx context.Context, sid string) error {
	if found := impl.cache.Delete(ctx, SessionCacheKey(sid)); !found {
		slog.WarnContext(ctx, "User session was not found in memory cache to delete")
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	sessionID, _ := sessionIDFunc(sid)
	err := impl.querier.DeleteCachedByKey(ctx, sessionID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete cached session from DB", common.ErrAttr(err))
	}

	return err
}

func (impl *BusinessStoreImpl) CacheUserSession(ctx context.Context, data *session.SessionData) error {
	if data == nil {
		return ErrInvalidInput
	}

	return impl.cache.Set(ctx, SessionCacheKey(data.ID()), data)
}

func (impl *BusinessStoreImpl) RetrieveUserSession(ctx context.Context, sid string, skipCache bool) (*session.SessionData, error) {
	if len(sid) == 0 {
		return nil, ErrInvalidInput
	}

	if skipCache {
		// we do not re-cache it yet and let external changes to be merged first
		return impl.doGetSessionbyID(ctx, sid)
	}

	reader := &StoreOneReader[string, session.SessionData]{
		CacheKey: SessionCacheKey(sid),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryFunc = impl.doGetSessionbyID
		reader.QueryKeyFunc = QueryKeyString
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) StoreUserSessions(ctx context.Context, batch map[string]uint, persistKey session.SessionKey, ttl time.Duration) error {
	reader := &StoreBulkReader[string, string, session.SessionData]{
		ArgFunc:      nil, // we shouldn't be using it as we read from cache only
		Cache:        impl.cache,
		CacheKeyFunc: SessionCacheKey,
		QueryKeyFunc: sessionIDFunc,
		QueryFunc:    nil, // explicitly set - we are only interested in cache
	}

	// NOTE: it does have the side-effect of extending session expiration in our cache once again (which _is_ a "bug"),
	// but its impact is not large enough to bother
	cached, _, err := reader.Read(ctx, batch)
	if (err != nil) && (err != ErrMaintenance) {
		slog.Log(ctx, common.LevelTrace, "Failed to read cached sessions", common.ErrAttr(err))
		return err
	}

	if len(cached) == 0 {
		slog.DebugContext(ctx, "No sessions to save")
		return nil
	}

	slog.DebugContext(ctx, "Read sessions chunk to save", "count", len(cached))

	keys := make([]string, 0, len(batch))
	values := make([][]byte, 0, len(batch))
	intervals := make([]time.Duration, 0, len(batch))

	for _, sd := range cached {
		if !sd.Has(persistKey) {
			slog.Log(ctx, common.LevelTrace, "Skipping persisting session without persist key", common.SessionIDAttr(sd.ID()))
			continue
		}

		data, err := sd.MarshalBinary()
		if err != nil {
			slog.ErrorContext(ctx, "Failed to marshal session", common.SessionIDAttr(sd.ID()), common.ErrAttr(err))
			continue
		}

		slog.Log(ctx, common.LevelTrace, "Marshaled session data to binary", common.SessionIDAttr(sd.ID()), "fields", sd.Size())

		sidKey, _ := sessionIDFunc(sd.ID())
		keys = append(keys, sidKey)
		values = append(values, data)
		intervals = append(intervals, ttl)
	}

	if len(keys) == 0 {
		slog.WarnContext(ctx, "No persistent sessions to save")
		return nil
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	err = impl.querier.CreateCacheMany(ctx, &dbgen.CreateCacheManyParams{
		Keys:      keys,
		Values:    values,
		Intervals: intervals,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to cache sessions", "count", len(keys), common.ErrAttr(err))
	}

	slog.DebugContext(ctx, "Saved persisted sessions to DB", "count", len(keys))

	return err
}

func (impl *BusinessStoreImpl) RetrievePropertyBySitekey(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	reader := &StoreOneReader[pgtype.UUID, dbgen.Property]{
		CacheKey: PropertyBySitekeyCacheKey(sitekey),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryFunc = impl.querier.GetPropertyByExternalID
		reader.QueryKeyFunc = queryKeySitekeyUUID
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) RetrievePropertiesBySitekey(ctx context.Context, sitekeys map[string]uint, minMissingCount uint) ([]*dbgen.Property, error) {
	reader := &StoreBulkReader[string, pgtype.UUID, dbgen.Property]{
		ArgFunc:         propertySitekeyFunc,
		Cache:           impl.cache,
		CacheKeyFunc:    PropertyBySitekeyCacheKey,
		QueryKeyFunc:    stringKeySitekeyUUID,
		MinMissingCount: minMissingCount,
	}

	if impl.querier != nil {
		reader.QueryFunc = impl.querier.GetPropertiesByExternalID
	}

	cached, items, err := reader.Read(ctx, sitekeys)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		sitekey := UUIDToSiteKey(item.ExternalID)
		cacheKey := PropertyBySitekeyCacheKey(sitekey)
		_ = impl.cache.SetWithTTL(ctx, cacheKey, item, propertyTTL)
	}

	result := cached
	result = append(result, items...)
	return result, nil
}

// this is pretty much a copy paste of RetrievePropertiesBySitekey
func (impl *BusinessStoreImpl) RetrievePropertiesByID(ctx context.Context, batch map[int32]uint) ([]*dbgen.Property, error) {
	reader := &StoreBulkReader[int32, int32, dbgen.Property]{
		ArgFunc:      propertyIDFunc,
		Cache:        impl.cache,
		CacheKeyFunc: propertyByIDCacheKey,
		QueryKeyFunc: IdentityKeyFunc[int32],
	}

	if impl.querier != nil {
		reader.QueryFunc = impl.querier.GetPropertiesByID
	}

	cached, items, err := reader.Read(ctx, batch)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		sitekey := UUIDToSiteKey(item.ExternalID)
		cacheKey := PropertyBySitekeyCacheKey(sitekey)
		_ = impl.cache.SetWithTTL(ctx, cacheKey, item, propertyTTL)
	}

	result := cached
	result = append(result, items...)
	return result, nil
}

func (impl *BusinessStoreImpl) GetCachedAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	cacheKey := APIKeyCacheKey(secret)

	if apiKey, err := FetchCachedOne[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return apiKey, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	} else {
		return nil, err
	}
}

func (impl *BusinessStoreImpl) FindUserAPIKeyByName(ctx context.Context, user *dbgen.User, name string) (*dbgen.APIKey, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	key, err := impl.querier.GetUserAPIKeyByName(ctx, &dbgen.GetUserAPIKeyByNameParams{
		UserID: Int(user.ID),
		Name:   name,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve API key by name", "userID", user.ID, "name", name, common.ErrAttr(err))

		return nil, err
	}

	if key != nil {
		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.SetWithTTL(ctx, cacheKey, key, apiKeyTTL)
	}

	return key, nil
}

// Fetches API key from DB, backed by cache
func (impl *BusinessStoreImpl) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	reader := &StoreOneReader[pgtype.UUID, dbgen.APIKey]{
		CacheKey: APIKeyCacheKey(secret),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryFunc = impl.querier.GetAPIKeyByExternalID
		reader.QueryKeyFunc = queryKeySecretUUID
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) retrieveUser(ctx context.Context, userID int32) (*dbgen.User, error) {
	reader := &StoreOneReader[int32, dbgen.User]{
		CacheKey: UserCacheKey(userID),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = QueryKeyInt
		reader.QueryFunc = impl.querier.GetUserByID
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) FindUserByEmail(ctx context.Context, email string) (*dbgen.User, error) {
	if len(email) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	user, err := impl.querier.GetUserByEmail(ctx, email)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve user by email", "email", email, common.ErrAttr(err))

		return nil, err
	}

	if user != nil {
		cacheKey := UserCacheKey(user.ID)
		_ = impl.cache.Set(ctx, cacheKey, user)
	}

	return user, nil
}

func (impl *BusinessStoreImpl) RetrieveUserOrganizations(ctx context.Context, userID int32) ([]*dbgen.GetUserOrganizationsRow, error) {
	reader := &StoreArrayReader[pgtype.Int4, dbgen.GetUserOrganizationsRow]{
		CacheKey: userOrgsCacheKey(userID),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = QueryKeyPgInt
		reader.QueryFunc = impl.querier.GetUserOrganizations
	}

	orgs, err := reader.Read(ctx)
	if err != nil {
		return nil, err
	}

	// NOTE: We sort here instead in SQL to avoid confusing sqlc by ordering the UNION ALL result as a subquery
	sort.Slice(orgs, func(i, j int) bool {
		return orgs[i].Organization.CreatedAt.Time.Before(orgs[j].Organization.CreatedAt.Time)
	})

	slog.DebugContext(ctx, "Retrieved user organizations", "count", len(orgs))

	// TODO: Also sort by orgs that have any properties in them
	return orgs, nil
}

func (impl *BusinessStoreImpl) retrieveOrganizationWithAccess(ctx context.Context, userID, orgID int32) (*dbgen.Organization, dbgen.NullAccessLevel, error) {
	cacheKey := orgCacheKey(orgID)

	if org, err := FetchCachedOne[dbgen.Organization](ctx, impl.cache, cacheKey); err == nil {
		if org.UserID.Int32 == userID {
			return org, nullAccessLevelOwner, nil
		}
		// NOTE: for security reasons, we want to verify that this user has rights to get this org

		// this value should be in cache if user opens "Members" tab in the org
		if users, err := FetchCachedArray[dbgen.GetOrganizationUsersRow](ctx, impl.cache, orgUsersCacheKey(orgID)); err == nil {
			if hasUser := slices.ContainsFunc(users, func(u *dbgen.GetOrganizationUsersRow) bool { return u.User.ID == userID }); hasUser {
				slog.Log(ctx, common.LevelTrace, "Found cached org from organization users", "orgID", orgID, "userID", userID)
				return org, nullAccessLevelMember, nil
			}
		}
	} else if err == ErrNegativeCacheHit {
		return nil, nullAccessLevelNull, ErrNegativeCacheHit
	}

	// this value should be in cache for "normal" use-cases (e.g. user logs in to the portal)
	if orgs, err := FetchCachedArray[dbgen.GetUserOrganizationsRow](ctx, impl.cache, userOrgsCacheKey(userID)); err == nil {
		if index := slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool { return o.Organization.ID == orgID }); index != -1 {
			slog.Log(ctx, common.LevelTrace, "Found cached org from user organizations", "orgID", orgID, "userID", userID)
			org := &dbgen.Organization{}
			*org = orgs[index].Organization
			_ = impl.cache.Set(ctx, cacheKey, org)

			return org, dbgen.NullAccessLevel{Valid: true, AccessLevel: orgs[index].Level}, nil
		}
	}

	if impl.querier == nil {
		return nil, nullAccessLevelNull, ErrMaintenance
	}

	// NOTE: we don't return the whole row from org_users in query and instead we only get the level back
	// left join and embed() do not work together in sqlc (https://github.com/sqlc-dev/sqlc/issues/2348)
	orgAndAccess, err := impl.querier.GetOrganizationWithAccess(ctx, &dbgen.GetOrganizationWithAccessParams{
		ID:     orgID,
		UserID: userID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.SetMissing(ctx, cacheKey)
			return nil, nullAccessLevelNull, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve organization by ID", "orgID", orgID, common.ErrAttr(err))

		return nil, nullAccessLevelNull, err
	}

	// when sqlc will be able to do embed() as a pointer, we can remove this copying
	org := &dbgen.Organization{}
	*org = orgAndAccess.Organization

	_ = impl.cache.Set(ctx, cacheKey, org)

	if org.UserID.Int32 == userID {
		return org, nullAccessLevelOwner, nil
	}

	return org, orgAndAccess.Level, nil
}

func (impl *BusinessStoreImpl) cacheProperty(ctx context.Context, property *dbgen.Property) {
	if property == nil {
		return
	}

	key := propertyByIDCacheKey(property.ID)
	_ = impl.cache.Set(ctx, key, property)
	sitekey := UUIDToSiteKey(property.ExternalID)
	_ = impl.cache.SetWithTTL(ctx, PropertyBySitekeyCacheKey(sitekey), property, propertyTTL)
}

func (impl *BusinessStoreImpl) deleteCachedProperty(ctx context.Context, property *dbgen.Property) {
	if property == nil {
		return
	}

	// update caches
	sitekey := UUIDToSiteKey(property.ExternalID)
	// cache mostly used in API server
	_ = impl.cache.SetMissing(ctx, PropertyBySitekeyCacheKey(sitekey))
	_ = impl.cache.SetMissing(ctx, propertyByIDCacheKey(property.ID))
	// invalidate org properties in cache as we just deleted a property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(property.OrgID.Int32, orgPropertiesCacheKeyStr))
	_ = impl.cache.Delete(ctx, orgPropertiesCountCacheKey(property.OrgID.Int32))
	_ = impl.cache.Delete(ctx, userPropertiesCountCacheKey(property.CreatorID.Int32))
	_ = impl.cache.Delete(ctx, userPropertiesCountCacheKey(property.OrgOwnerID.Int32))
}

func (impl *BusinessStoreImpl) GetCachedOrgProperties(ctx context.Context, orgID int32) ([]*dbgen.Property, error) {
	return FetchCachedArray[dbgen.Property](ctx, impl.cache, orgPropertiesCacheKey(orgID, orgPropertiesCacheKeyStr))
}

func (impl *BusinessStoreImpl) retrieveOrgProperty(ctx context.Context, orgID, propID int32) (*dbgen.Property, error) {
	cacheKey := propertyByIDCacheKey(propID)

	if prop, err := FetchCachedOne[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
		return prop, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if properties, err := FetchCachedArray[dbgen.Property](ctx, impl.cache, orgPropertiesCacheKey(orgID, orgPropertiesCacheKeyStr)); err == nil {
		if index := slices.IndexFunc(properties, func(p *dbgen.Property) bool { return p.ID == propID }); index != -1 {
			property := properties[index]
			impl.cacheProperty(ctx, property)
			return property, nil
		}
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.querier.GetPropertyByID(ctx, propID)
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.SetMissing(ctx, cacheKey)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by ID", "propID", propID, common.ErrAttr(err))

		return nil, err
	}

	impl.cacheProperty(ctx, property)

	return property, nil
}

func (impl *BusinessStoreImpl) RetrieveSubscription(ctx context.Context, sID int32) (*dbgen.Subscription, error) {
	reader := &StoreOneReader[int32, dbgen.Subscription]{
		CacheKey: SubscriptionCacheKey(sID),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = QueryKeyInt
		reader.QueryFunc = impl.querier.GetSubscriptionByID
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) FindOrgProperty(ctx context.Context, name string, org *dbgen.Organization) (*dbgen.Property, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.querier.GetOrgPropertyByName(ctx, &dbgen.GetOrgPropertyByNameParams{
		OrgID: Int(org.ID),
		Name:  name,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by name", "name", name, common.ErrAttr(err))

		return nil, err
	}

	return property, nil
}

func (impl *BusinessStoreImpl) FindOrg(ctx context.Context, name string, user *dbgen.User) (*dbgen.Organization, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	org, err := impl.querier.FindUserOrgByName(ctx, &dbgen.FindUserOrgByNameParams{
		UserID: Int(user.ID),
		Name:   name,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve org by name", "name", name, common.ErrAttr(err))

		return nil, err
	}

	return org, nil
}

func (impl *BusinessStoreImpl) CreateNewProperty(ctx context.Context, params *dbgen.CreatePropertyParams, org *dbgen.Organization) (*dbgen.Property, *common.AuditLogEvent, error) {
	if (params == nil) || (len(params.Domain) == 0) || (len(params.Name) == 0) {
		return nil, nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	params.OrgID = Int(org.ID)
	params.OrgOwnerID = org.UserID

	property, err := impl.querier.CreateProperty(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property in DB", "name", params.Name, "org", params.OrgID, common.ErrAttr(err))
		return nil, nil, err
	}

	slog.InfoContext(ctx, "Created new property", "id", property.ID, "name", params.Name, "org", params.OrgID)

	impl.cacheProperty(ctx, property)
	// invalidate org properties in cache as we just created a new property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(params.OrgID.Int32, orgPropertiesCacheKeyStr))
	_ = impl.cache.Delete(ctx, userPropertiesCountCacheKey(property.CreatorID.Int32))
	_ = impl.cache.Delete(ctx, userPropertiesCountCacheKey(property.OrgOwnerID.Int32))
	_ = impl.cache.Delete(ctx, orgPropertiesCountCacheKey(property.OrgID.Int32))

	auditEvent := newCreatePropertyAuditLogEvent(property, org)

	return property, auditEvent, nil
}

func (impl *BusinessStoreImpl) UpdateProperty(ctx context.Context, oldProperty *dbgen.Property, org *dbgen.Organization, params *dbgen.UpdatePropertyParams) (*dbgen.Property, *common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	updatedProperty, err := impl.querier.UpdateProperty(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update property in DB", "name", params.Name, "propID", params.ID, common.ErrAttr(err))
		return nil, nil, err
	}

	slog.InfoContext(ctx, "Updated property", "name", params.Name, "propID", params.ID)

	impl.cacheProperty(ctx, updatedProperty)
	// invalidate org properties in cache as we just created a new property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(updatedProperty.OrgID.Int32, orgPropertiesCacheKeyStr))
	_ = impl.cache.Delete(ctx, propertyAuditLogsCacheKey(updatedProperty.ID))

	auditEvent := newUpdatePropertyAuditLogEvent(oldProperty, updatedProperty, org)

	return updatedProperty, auditEvent, nil
}

func (impl *BusinessStoreImpl) SoftDeleteProperty(ctx context.Context, prop *dbgen.Property, org *dbgen.Organization, user *dbgen.User) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.querier.SoftDeleteProperty(ctx, prop.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to mark property as deleted in DB", "propID", prop.ID, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Soft-deleted property", "propID", prop.ID)

	impl.deleteCachedProperty(ctx, property)
	auditEvent := newDeletePropertyAuditLogEvent(prop, org, user)

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) SoftDeleteProperties(ctx context.Context, ids []int32, user *dbgen.User) (map[int32]struct{}, []*common.AuditLogEvent, error) {
	if len(ids) == 0 {
		return map[int32]struct{}{}, []*common.AuditLogEvent{}, nil
	}

	if user == nil {
		return nil, nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	properties, err := impl.querier.SoftDeleteProperties(ctx, &dbgen.SoftDeletePropertiesParams{
		Column1:   ids,
		CreatorID: Int(user.ID),
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to mark properties as deleted in DB", "count", len(ids), common.ErrAttr(err))
		return nil, nil, err
	}

	slog.InfoContext(ctx, "Soft-deleted properties", "count", len(properties))

	auditEvents := make([]*common.AuditLogEvent, 0, len(properties))
	deletedIDs := make(map[int32]struct{})

	for _, property := range properties {
		impl.deleteCachedProperty(ctx, property)
		auditEvents = append(auditEvents, newDeletePropertyAuditLogEvent(property, nil /*org*/, user))
		deletedIDs[property.ID] = struct{}{}
	}

	return deletedIDs, auditEvents, nil
}

func (impl *BusinessStoreImpl) RetrieveOrgProperties(ctx context.Context, org *dbgen.Organization, offset, limit int) ([]*dbgen.Property, bool, error) {
	if (offset < 0) || (limit <= 0) {
		return nil, false, ErrInvalidInput
	}

	params := &dbgen.GetOrgPropertiesParams{
		OrgID:  Int(org.ID),
		Offset: int32(offset),
		Limit:  OrgPropertiesPageSize + 1,
	}

	if offset == 0 {
		reader := &StoreArrayReader[*dbgen.GetOrgPropertiesParams, dbgen.Property]{
			CacheKey: orgPropertiesCacheKey(org.ID, orgPropertiesCacheKeyStr),
			Cache:    impl.cache,
		}

		if impl.querier != nil {
			reader.QueryKeyFunc = func(ck CacheKey) (*dbgen.GetOrgPropertiesParams, error) { return params, nil }
			reader.QueryFunc = impl.querier.GetOrgProperties
		}

		properties, err := reader.Read(ctx)
		if err != nil {
			return nil, false, err
		}

		finalProperties := properties[:min(len(properties), limit, OrgPropertiesPageSize)]

		return finalProperties, len(properties) > len(finalProperties), nil
	}

	if impl.querier == nil {
		return nil, false, ErrMaintenance
	}

	actualLimit := min(OrgPropertiesPageSize, limit)
	params.Limit = int32(actualLimit) + 1

	properties, err := impl.querier.GetOrgProperties(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org properties", "offset", offset, "limit", actualLimit, "orgID", org.ID, common.ErrAttr(err))
		return nil, false, err
	}

	slog.DebugContext(ctx, "Retrieved org properties", "offset", offset, "limit", actualLimit, "orgID", org.ID, "count", len(properties))

	return properties[:min(len(properties), actualLimit)], len(properties) == int(params.Limit), nil
}

func (impl *BusinessStoreImpl) UpdateOrganization(ctx context.Context, user *dbgen.User, org *dbgen.Organization, name string) (*dbgen.Organization, *common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	oldName := org.Name

	org, err := impl.querier.UpdateOrganization(ctx, &dbgen.UpdateOrganizationParams{
		Name: name,
		ID:   org.ID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update org in DB", "name", name, "orgID", org.ID, common.ErrAttr(err))
		return nil, nil, err
	}

	slog.InfoContext(ctx, "Updated organization", "name", name, "orgID", org.ID)

	cacheKey := orgCacheKey(org.ID)
	_ = impl.cache.Set(ctx, cacheKey, org)
	// invalidate user orgs in cache as we just updated name
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))

	auditEvent := newUpdateOrgAuditLogEvent(user, org, oldName)

	return org, auditEvent, nil
}

func (impl *BusinessStoreImpl) SoftDeleteOrganization(ctx context.Context, org *dbgen.Organization, user *dbgen.User) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	if err := impl.querier.SoftDeleteUserOrganization(ctx, &dbgen.SoftDeleteUserOrganizationParams{
		ID:     org.ID,
		UserID: Int(user.ID),
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to mark organization as deleted in DB", "orgID", org.ID, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Soft-deleted organization", "orgID", org.ID)

	// update caches
	_ = impl.cache.SetMissing(ctx, orgCacheKey(org.ID))
	_ = impl.cache.Delete(ctx, orgPropertiesCountCacheKey(org.ID))
	// invalidate user orgs in cache as we just deleted one
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(user.ID))
	_ = impl.cache.Delete(ctx, userPropertiesCountCacheKey(user.ID))

	auditEvent := newOrgAuditLogEvent(user.ID, org, common.AuditLogActionSoftDelete)

	return auditEvent, nil
}

// NOTE: by definition this does not include the owner as this relationship is set directly in the 'organizations' table
func (impl *BusinessStoreImpl) RetrieveOrganizationUsers(ctx context.Context, orgID int32) ([]*dbgen.GetOrganizationUsersRow, error) {
	reader := &StoreArrayReader[int32, dbgen.GetOrganizationUsersRow]{
		CacheKey: orgUsersCacheKey(orgID),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = QueryKeyInt
		reader.QueryFunc = impl.querier.GetOrganizationUsers
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) InviteUserToOrg(ctx context.Context, user *dbgen.User, org *dbgen.Organization, inviteUser *dbgen.User) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	_, err := impl.querier.InviteUserToOrg(ctx, &dbgen.InviteUserToOrgParams{
		OrgID:  org.ID,
		UserID: inviteUser.ID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to invite user to org", "orgID", org.ID, "userID", inviteUser.ID, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Added org membership invite", "orgID", org.ID, "userID", inviteUser.ID)

	// invalidate relevant caches
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(inviteUser.ID))
	_ = impl.cache.Delete(ctx, orgUsersCacheKey(org.ID))

	auditEvent := newOrgInviteAuditLogEvent(user, org, inviteUser)

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) JoinOrg(ctx context.Context, orgID int32, user *dbgen.User) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	err := impl.querier.UpdateOrgMembershipLevel(ctx, &dbgen.UpdateOrgMembershipLevelParams{
		OrgID:   orgID,
		UserID:  user.ID,
		Level:   dbgen.AccessLevelMember,
		Level_2: dbgen.AccessLevelInvited,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to accept org invite", "orgID", orgID, "userID", user.ID, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Accepted org invite", "orgID", orgID, "userID", user.ID)

	// invalidate relevant caches
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(user.ID))
	_ = impl.cache.Delete(ctx, orgUsersCacheKey(orgID))

	var orgName string
	if org, err := FetchCachedOne[dbgen.Organization](ctx, impl.cache, orgCacheKey(orgID)); err == nil {
		orgName = org.Name
	}

	auditEvent := newOrgMemberAuditLogEvent(orgID, orgName, user, common.AuditLogActionUpdate, string(dbgen.AccessLevelMember))

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) LeaveOrg(ctx context.Context, orgID int32, user *dbgen.User) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	err := impl.querier.UpdateOrgMembershipLevel(ctx, &dbgen.UpdateOrgMembershipLevelParams{
		OrgID:   orgID,
		UserID:  user.ID,
		Level:   dbgen.AccessLevelInvited,
		Level_2: dbgen.AccessLevelMember,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to leave org", "orgID", orgID, "userID", user.ID, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Left organization", "orgID", orgID, "userID", user.ID)

	// invalidate relevant caches
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(user.ID))
	_ = impl.cache.Delete(ctx, orgUsersCacheKey(orgID))

	var orgName string
	if org, err := FetchCachedOne[dbgen.Organization](ctx, impl.cache, orgCacheKey(orgID)); err == nil {
		orgName = org.Name
	}

	auditEvent := newOrgMemberAuditLogEvent(orgID, orgName, user, common.AuditLogActionDelete, "")

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) RemoveUserFromOrg(ctx context.Context, user *dbgen.User, org *dbgen.Organization, userID int32) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	err := impl.querier.RemoveUserFromOrg(ctx, &dbgen.RemoveUserFromOrgParams{
		OrgID:  org.ID,
		UserID: userID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to remove user from org", "orgID", org.ID, "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Removed user from org", "orgID", org.ID, "userID", userID)

	// invalidate relevant caches
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = impl.cache.Delete(ctx, orgUsersCacheKey(org.ID))

	userEmail := ""
	if cachedUser, err := FetchCachedOne[dbgen.User](ctx, impl.cache, UserCacheKey(userID)); err == nil {
		userEmail = cachedUser.Email
	}

	auditEvent := newOrgMemberDeleteAuditLogEvent(user, org, userID, userEmail)

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) UpdateUserSubscription(ctx context.Context, user *dbgen.User, subscription *dbgen.Subscription) (*dbgen.User, *common.AuditLogEvent, error) {
	if subscription == nil {
		return nil, nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	var oldSubscription *dbgen.Subscription
	if user.SubscriptionID.Valid {
		oldSubscription, _ = FetchCachedOne[dbgen.Subscription](ctx, impl.cache, SubscriptionCacheKey(user.SubscriptionID.Int32))
	}

	user, err := impl.querier.UpdateUserSubscription(ctx, &dbgen.UpdateUserSubscriptionParams{
		ID:             user.ID,
		SubscriptionID: Int(subscription.ID),
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update user subscription", "userID", user.ID, "subscriptionID", subscription.ID, common.ErrAttr(err))
		return nil, nil, err
	}

	var auditEvent *common.AuditLogEvent

	if user != nil {
		slog.InfoContext(ctx, "Updated user subscription", "userID", user.ID, "subscriptionID", subscription.ID)
		_ = impl.cache.Set(ctx, UserCacheKey(user.ID), user)

		auditEvent = newUpdateUserSubscriptionEvent(user, oldSubscription, subscription)
	}

	return user, auditEvent, nil
}

func (impl *BusinessStoreImpl) UpdateUser(ctx context.Context, user *dbgen.User, name string, newEmail, oldEmail string) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	params := &dbgen.UpdateUserDataParams{
		Name:  name,
		Email: newEmail,
		ID:    user.ID,
	}

	updatedUser, err := impl.querier.UpdateUserData(ctx, params)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update user", "userID", user.ID, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Updated user", "userID", updatedUser.ID)

	var auditEvent *common.AuditLogEvent

	if user != nil {
		_ = impl.cache.Set(ctx, UserCacheKey(updatedUser.ID), updatedUser)

		auditEvent = newUpdateUserAuditLogEvent(user, updatedUser)
	}

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) RetrieveUserAPIKeys(ctx context.Context, userID int32) ([]*dbgen.APIKey, error) {
	reader := &StoreArrayReader[pgtype.Int4, dbgen.APIKey]{
		CacheKey: UserAPIKeysCacheKey(userID),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = QueryKeyPgInt
		reader.QueryFunc = impl.querier.GetUserAPIKeys
	}

	keys, err := reader.Read(ctx)
	if err == nil {
		// recache individual keys
		for _, key := range keys {
			secret := UUIDToSecret(key.ExternalID)
			cacheKey := APIKeyCacheKey(secret)
			_ = impl.cache.SetWithTTL(ctx, cacheKey, key, apiKeyTTL)
		}
	}

	return keys, err
}

func (impl *BusinessStoreImpl) UpdateAPIKey(ctx context.Context, user *dbgen.User, oldKey *dbgen.APIKey, expiration time.Time, enabled bool) (*common.AuditLogEvent, error) {
	if expiration.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	updatedKey, err := impl.querier.UpdateAPIKey(ctx, &dbgen.UpdateAPIKeyParams{
		ExpiresAt:  Timestampz(expiration),
		Enabled:    Bool(enabled),
		ExternalID: oldKey.ExternalID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update API key", "externalID", UUIDToSecret(oldKey.ExternalID), common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Updated API key", "externalID", UUIDToSecret(oldKey.ExternalID))

	var auditEvent *common.AuditLogEvent

	if updatedKey != nil {
		secret := UUIDToSecret(updatedKey.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.SetWithTTL(ctx, cacheKey, updatedKey, apiKeyTTL)

		// invalidate keys cache
		_ = impl.cache.Delete(ctx, UserAPIKeysCacheKey(updatedKey.UserID.Int32))

		auditEvent = newUpdateAPIKeyAuditLogEvent(user, oldKey, updatedKey)
	}

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) CreateAPIKey(ctx context.Context, user *dbgen.User, params *dbgen.CreateAPIKeyParams) (*dbgen.APIKey, *common.AuditLogEvent, error) {
	if len(params.Name) == 0 {
		return nil, nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	params.UserID = Int(user.ID)

	key, err := impl.querier.CreateAPIKey(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create API key", "userID", user.ID, common.ErrAttr(err))
		return nil, nil, err
	}

	var auditEvent *common.AuditLogEvent

	if key != nil {
		slog.InfoContext(ctx, "Created API key", "userID", user.ID, "keyID", key.ID)

		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.SetWithTTL(ctx, cacheKey, key, apiKeyTTL)

		// invalidate keys cache
		_ = impl.cache.Delete(ctx, UserAPIKeysCacheKey(user.ID))

		auditEvent = newAPIKeyAuditLogEvent(user, key, common.AuditLogActionCreate)
	}

	return key, auditEvent, nil
}

func (impl *BusinessStoreImpl) RotateAPIKey(ctx context.Context, user *dbgen.User, keyID int32) (*dbgen.APIKey, *common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	var oldKey *dbgen.APIKey

	// to rotate we would want to drop old key from cache immediately (if we don't, not a big deal actually)
	// the reason we ONLY check in cache is because rotation is only available from when user opens settings
	// which means to show them the keys we should put them all in cache first when reading
	userKeysCacheKey := UserAPIKeysCacheKey(user.ID)
	if keys, err := FetchCachedArray[dbgen.APIKey](ctx, impl.cache, userKeysCacheKey); err == nil {
		if index := slices.IndexFunc(keys, func(key *dbgen.APIKey) bool { return key.ID == keyID }); index != -1 {
			oldKey = keys[index]
			secret := UUIDToSecret(oldKey.ExternalID)
			cacheKey := APIKeyCacheKey(secret)
			_ = impl.cache.Delete(ctx, cacheKey)
		} else {
			slog.WarnContext(ctx, "Old key not found in cached user keys", "userID", user.ID, "keyID", keyID)
		}
	}

	key, err := impl.querier.RotateAPIKey(ctx, &dbgen.RotateAPIKeyParams{
		ID:     keyID,
		UserID: Int(user.ID),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			slog.ErrorContext(ctx, "Failed to find API Key", "keyID", keyID, "userID", user.ID)
			return nil, nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to rotate API key", "keyID", keyID, "userID", user.ID, common.ErrAttr(err))
		return nil, nil, err
	}

	slog.InfoContext(ctx, "Rotated API Key", "keyID", keyID, "userID", user.ID)

	var auditEvent *common.AuditLogEvent

	if key != nil {
		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.SetWithTTL(ctx, cacheKey, key, apiKeyTTL)

		auditEvent = newUpdateAPIKeyAuditLogEvent(user, oldKey, key)
	}

	_ = impl.cache.Delete(ctx, userKeysCacheKey)

	return key, auditEvent, nil
}

func (impl *BusinessStoreImpl) DeleteAPIKey(ctx context.Context, user *dbgen.User, keyID int32) (*common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	key, err := impl.querier.DeleteAPIKey(ctx, &dbgen.DeleteAPIKeyParams{
		ID:     keyID,
		UserID: Int(user.ID),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			slog.ErrorContext(ctx, "Failed to find API Key", "keyID", keyID, "userID", user.ID)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to delete API key", "keyID", keyID, "userID", user.ID, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Deleted API Key", "keyID", keyID, "userID", user.ID)

	// invalidate keys cache
	if key != nil {
		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.Delete(ctx, cacheKey)
	}

	_ = impl.cache.Delete(ctx, UserAPIKeysCacheKey(user.ID))

	auditEvent := newAPIKeyAuditLogEvent(user, key, common.AuditLogActionDelete)

	return auditEvent, nil
}

func (impl *BusinessStoreImpl) RetrieveUsersWithoutSubscription(ctx context.Context, userIDs []int32) ([]*dbgen.User, error) {
	if len(userIDs) == 0 {
		return []*dbgen.User{}, nil
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	users, err := impl.querier.GetUsersWithoutSubscription(ctx, userIDs)
	if err != nil {
		if err == pgx.ErrNoRows {
			return []*dbgen.User{}, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve users without subscriptions", "userIDs", len(userIDs), common.ErrAttr(err))

		return nil, err
	}

	slog.DebugContext(ctx, "Fetched users without subscriptions", "count", len(users), "userIDs", len(userIDs))

	return users, err
}

func (impl *BusinessStoreImpl) RetrieveLock(ctx context.Context, name string) (*dbgen.Lock, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	result, err := impl.querier.GetLock(ctx, name)
	if err != nil {
		if err == pgx.ErrNoRows {
			// slog.WarnContext(ctx, "Lock is still taken", "name", name)
			return nil, ErrRecordNotFound
		}
		slog.ErrorContext(ctx, "Failed to get lock", "name", name, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Retrieved a lock", "name", name)

	return result, nil
}

func (impl *BusinessStoreImpl) AcquireLock(ctx context.Context, name string, data []byte, expiration time.Time) (*dbgen.Lock, error) {
	if (len(name) == 0) || expiration.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	lock, err := impl.querier.InsertLock(ctx, &dbgen.InsertLockParams{
		Name:      name,
		Data:      data,
		ExpiresAt: Timestampz(expiration),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			// slog.WarnContext(ctx, "Lock is still taken", "name", name)
			return nil, ErrLocked
		}
		slog.ErrorContext(ctx, "Failed to acquire a lock", "name", name, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Acquired a lock", "name", name, "expires_at", lock.ExpiresAt.Time)

	return lock, nil
}

func (impl *BusinessStoreImpl) ReleaseLock(ctx context.Context, name string) error {
	if impl.querier == nil {
		return ErrMaintenance
	}
	err := impl.querier.DeleteLock(ctx, name)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to release a lock", "name", name, common.ErrAttr(err))
	}

	slog.DebugContext(ctx, "Released a lock", "name", name)

	return err
}

func (impl *BusinessStoreImpl) DeleteDeletedRecords(ctx context.Context, before time.Time) error {
	if before.IsZero() {
		return ErrInvalidInput
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.DeleteDeletedRecords(ctx, Timestampz(before))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to cleanup deleted records", "before", before, common.ErrAttr(err))
	}

	return err
}

func (impl *BusinessStoreImpl) RetrieveSoftDeletedProperties(ctx context.Context, before time.Time, limit int32) ([]*dbgen.GetSoftDeletedPropertiesRow, error) {
	if before.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	properties, err := impl.querier.GetSoftDeletedProperties(ctx, &dbgen.GetSoftDeletedPropertiesParams{
		DeletedAt: Timestampz(before),
		Limit:     limit,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve soft deleted properties", "before", before, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched soft-deleted properties", "count", len(properties), "before", before)

	return properties, nil
}

func (impl *BusinessStoreImpl) DeleteProperties(ctx context.Context, ids []int32) error {
	if len(ids) == 0 {
		slog.WarnContext(ctx, "No properties to delete")
		return nil
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.DeleteProperties(ctx, ids)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete properties", "count", len(ids), common.ErrAttr(err))
	}

	return err
}

func (impl *BusinessStoreImpl) RetrieveSoftDeletedOrganizations(ctx context.Context, before time.Time, limit int32) ([]*dbgen.GetSoftDeletedOrganizationsRow, error) {
	if before.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	organizations, err := impl.querier.GetSoftDeletedOrganizations(ctx, &dbgen.GetSoftDeletedOrganizationsParams{
		DeletedAt: Timestampz(before),
		Limit:     limit,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve soft deleted organizations", "before", before, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched soft-deleted organizations", "count", len(organizations), "before", before)

	return organizations, nil
}

func (impl *BusinessStoreImpl) DeleteOrganizations(ctx context.Context, ids []int32) error {
	if len(ids) == 0 {
		slog.WarnContext(ctx, "No organizations to delete")
		return nil
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.DeleteOrganizations(ctx, ids)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete organizations", "count", len(ids), common.ErrAttr(err))
	}

	return err
}

func (impl *BusinessStoreImpl) RetrieveSoftDeletedUsers(ctx context.Context, before time.Time, limit int32) ([]*dbgen.GetSoftDeletedUsersRow, error) {
	if before.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	users, err := impl.querier.GetSoftDeletedUsers(ctx, &dbgen.GetSoftDeletedUsersParams{
		DeletedAt: Timestampz(before),
		Limit:     limit,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve soft deleted users", "before", before, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched soft-deleted users", "count", len(users), "before", before)

	return users, nil
}

func (impl *BusinessStoreImpl) DeleteUsers(ctx context.Context, ids []int32) error {
	if len(ids) == 0 {
		slog.WarnContext(ctx, "No users to delete")
		return nil
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.DeleteUsers(ctx, ids)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete users", "count", len(ids), common.ErrAttr(err))
	}

	return err
}

func (impl *BusinessStoreImpl) RetrieveSystemNotification(ctx context.Context, id int32) (*dbgen.SystemNotification, error) {
	reader := &StoreOneReader[int32, dbgen.SystemNotification]{
		CacheKey: notificationCacheKey(id),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = QueryKeyInt
		reader.QueryFunc = impl.querier.GetSystemNotificationById
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) RetrieveSystemUserNotification(ctx context.Context, tnow time.Time, userID int32) (*dbgen.SystemNotification, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	n, err := impl.querier.GetLastActiveSystemNotification(ctx, &dbgen.GetLastActiveSystemNotificationParams{
		Column1: Timestampz(tnow),
		UserID:  Int(userID),
	})

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
		}
		slog.ErrorContext(ctx, "Failed to retrieve system notification", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	cacheKey := notificationCacheKey(n.ID)
	_ = impl.cache.Set(ctx, cacheKey, n)

	slog.DebugContext(ctx, "Retrieved system notification", "userID", userID, "notifID", n.ID)

	return n, err
}

func (impl *BusinessStoreImpl) CreateSystemNotification(ctx context.Context, message string, tnow time.Time, duration *time.Duration, userID *int32) (*dbgen.SystemNotification, error) {
	if (len(message) == 0) || tnow.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	arg := &dbgen.CreateSystemNotificationParams{
		Message:   message,
		StartDate: Timestampz(tnow),
		EndDate:   pgtype.Timestamptz{Valid: false},
		UserID:    pgtype.Int4{Valid: false},
	}

	if duration != nil {
		arg.EndDate = Timestampz(tnow.Add(*duration))
	}

	if userID != nil {
		arg.UserID = Int(*userID)
	}

	n, err := impl.querier.CreateSystemNotification(ctx, arg)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create a system notification", common.ErrAttr(err))
		return nil, err
	}

	if n != nil {
		cacheKey := notificationCacheKey(n.ID)
		_ = impl.cache.Set(ctx, cacheKey, n)
	}

	slog.InfoContext(ctx, "Created system notification", "notifID", n.ID)

	return n, err
}

func (impl *BusinessStoreImpl) RetrieveProperties(ctx context.Context, limit int) ([]*dbgen.Property, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	properties, err := impl.querier.GetProperties(ctx, int32(limit))

	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties", common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched properties", "count", len(properties))

	return properties, nil
}

func (impl *BusinessStoreImpl) RetrieveUserPropertiesCount(ctx context.Context, userID int32) (int64, error) {
	if impl.querier == nil {
		return 0, ErrMaintenance
	}

	cacheKey := userPropertiesCountCacheKey(userID)
	if count, err := FetchCachedOne[int64](ctx, impl.cache, cacheKey); err == nil {
		return *count, nil
	}

	count, err := impl.querier.GetUserPropertiesCount(ctx, Int(userID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user properties count", "userID", userID, common.ErrAttr(err))
		return 0, err
	}

	slog.DebugContext(ctx, "Fetched user properties count", "userID", userID, "count", count)

	const propertiesCountTTL = 5 * time.Minute
	c := new(int64)
	*c = count
	_ = impl.cache.SetWithTTL(ctx, cacheKey, c, propertiesCountTTL)

	return count, nil
}

func (impl *BusinessStoreImpl) GetCachedPropertyBySitekey(ctx context.Context, sitekey string, refreshFunc func(string)) (*dbgen.Property, error) {
	if sitekey == TestPropertySitekey {
		return nil, ErrTestProperty
	}

	// this check is important to keep as we depend on it at least in Sitekey() middleware
	if !CanBeValidSitekey(sitekey) {
		return nil, ErrInvalidInput
	}

	reader := &cachedPropertyReader{
		sitekey:     sitekey,
		cache:       impl.cache,
		refreshFunc: refreshFunc,
	}

	// we should NOT check for soft-deleted state because soft-deleted properties are deleted from cache in the first place
	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) RetrieveUser(ctx context.Context, id int32) (*dbgen.User, error) {
	user, err := impl.retrieveUser(ctx, id)
	if err != nil {
		return nil, err
	}

	if user.DeletedAt.Valid {
		slog.WarnContext(ctx, "User is soft-deleted", "userID", id, "deletedAt", user.DeletedAt.Time)
		return user, ErrSoftDeleted
	}

	return user, nil
}

func (impl *BusinessStoreImpl) RetrieveUserOrganization(ctx context.Context, user *dbgen.User, orgID int32) (*dbgen.Organization, error) {
	org, level, err := impl.retrieveOrganizationWithAccess(ctx, user.ID, orgID)
	if err != nil {
		return nil, err
	}

	if !level.Valid {
		slog.WarnContext(ctx, "User cannot access this org", "orgID", orgID, "userID", user.ID)
		return nil, ErrPermissions
	}

	if org.DeletedAt.Valid {
		slog.WarnContext(ctx, "Organization is soft-deleted", "orgID", orgID, "deletedAt", org.DeletedAt.Time)
		return org, ErrSoftDeleted
	}

	return org, nil
}

func (impl *BusinessStoreImpl) RetrieveOrgProperty(ctx context.Context, org *dbgen.Organization, propID int32) (*dbgen.Property, error) {
	property, err := impl.retrieveOrgProperty(ctx, org.ID, propID)
	if err != nil {
		return nil, err
	}

	if !property.OrgID.Valid || (property.OrgID.Int32 != org.ID) {
		slog.ErrorContext(ctx, "Property org does not match", "propertyOrgID", property.OrgID.Int32, "orgID", org.ID)
		return nil, ErrPermissions
	}

	if property.DeletedAt.Valid {
		slog.WarnContext(ctx, "Property is soft-deleted", "propID", propID, "deletedAt", property.DeletedAt.Time)
		return property, ErrSoftDeleted
	}

	return property, nil
}

func (impl *BusinessStoreImpl) CreateNewAccount(ctx context.Context, params *dbgen.CreateSubscriptionParams, email, name, orgName string, expectedUserID int32) (*dbgen.User, *dbgen.Organization, []*common.AuditLogEvent, error) {
	if (len(email) == 0) || (len(orgName) == 0) {
		return nil, nil, nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, nil, nil, ErrMaintenance
	}

	var subscription *dbgen.Subscription
	var err error
	var auditEvents []*common.AuditLogEvent

	if params != nil {
		subscription, err = impl.CreateNewSubscription(ctx, params)
		if err != nil {
			return nil, nil, nil, err
		}

		if existingUser, err := impl.FindUserByEmail(ctx, email); err == nil {
			slog.InfoContext(ctx, "User with such email already exists", "userID", existingUser.ID, "subscriptionID", existingUser.SubscriptionID, "expectedUserID", expectedUserID)

			if (existingUser.ID == expectedUserID) || (expectedUserID == -1) {
				if existingUser.SubscriptionID.Valid {
					if existingSubscription, err := impl.RetrieveSubscription(ctx, existingUser.SubscriptionID.Int32); (err == nil) && !IsInternalSubscription(existingSubscription.Source) {
						slog.ErrorContext(ctx, "Existing user already has external subscription",
							"existingUserID", existingUser.ID, "subscriptionID", existingSubscription.ID)
						return nil, nil, nil, ErrDuplicateAccount
					} else if err != nil {
						return nil, nil, nil, err
					}
				}

				updatedUser, auditEvent, err := impl.UpdateUserSubscription(ctx, existingUser, subscription)
				if err != nil {
					return nil, nil, nil, err
				}

				if auditEvent != nil {
					auditEvents = append(auditEvents, auditEvent)
				}

				return updatedUser, nil, auditEvents, nil
			}

			slog.ErrorContext(ctx, "Cannot update existing user with same email", "existingUserID", existingUser.ID,
				"expectedUserID", expectedUserID, "subscribed", existingUser.SubscriptionID.Valid, "email", email)

			return nil, nil, nil, ErrDuplicateAccount
		}
	}

	user, userAuditEvent, err := impl.createNewUser(ctx, email, name, subscription)
	if err != nil {
		return nil, nil, nil, err
	}

	if userAuditEvent != nil {
		auditEvents = append(auditEvents, userAuditEvent)
	}

	org, orgAuditEvent, err := impl.CreateNewOrganization(ctx, orgName, user.ID)
	if err != nil {
		return nil, nil, nil, err
	}

	if orgAuditEvent != nil {
		auditEvents = append(auditEvents, orgAuditEvent)
	}

	return user, org, auditEvents, nil
}

func (impl *BusinessStoreImpl) CreateNotificationTemplate(ctx context.Context, name, tplHTML, tplText, hash string) (*dbgen.NotificationTemplate, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	t, err := impl.querier.CreateNotificationTemplate(ctx, &dbgen.CreateNotificationTemplateParams{
		Name:        name,
		ContentHtml: tplHTML,
		ContentText: tplText,
		ExternalID:  hash,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create notification template", "name", name, "hash", hash, common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Upserted notification template", "name", name, "hash", hash)

	return t, nil
}

func (impl *BusinessStoreImpl) RetrieveNotificationTemplate(ctx context.Context, templateHash string) (*dbgen.NotificationTemplate, error) {
	reader := &StoreOneReader[string, dbgen.NotificationTemplate]{
		CacheKey: templateCacheKey(templateHash),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = QueryKeyString
		reader.QueryFunc = impl.querier.GetNotificationTemplateByHash
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) CreateUserNotification(ctx context.Context, n *common.ScheduledNotification) (*dbgen.UserNotification, error) {
	if (n == nil) || (len(n.TemplateHash) == 0) || (len(n.ReferenceID) == 0) {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	payload, err := json.Marshal(n.Data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize payload for notification", common.ErrAttr(err))
		return nil, err
	}

	// NOTE: we don't add template to DB (again) because it should have been done with RegisterEmailTemplatesJob on startup
	params := &dbgen.CreateUserNotificationParams{
		UserID:      Int(n.UserID),
		ReferenceID: n.ReferenceID,
		TemplateID:  Text(n.TemplateHash),
		Subject:     n.Subject,
		Payload:     payload,
		ScheduledAt: Timestampz(n.DateTime),
		Persistent:  n.Persistent,
	}

	switch n.Condition {
	case common.EmptyNotificationCondition:
		params.RequiresSubscription = pgtype.Bool{Valid: false}
	case common.NotificationWithSubscription:
		params.RequiresSubscription = Bool(true)
	case common.NotificationWithoutSubscription:
		params.RequiresSubscription = Bool(false)
	}

	rlog := slog.With("userID", params.UserID.Int32, "refID", params.ReferenceID)

	notif, err := impl.querier.CreateUserNotification(ctx, params)
	if err != nil {
		// warning and not error (as usual) because constraints are part of deduplication logic and we let caller decide
		// (and also - it's not such an important error in the end)
		rlog.WarnContext(ctx, "Failed to create user notification", common.ErrAttr(err))
		return nil, err
	}

	rlog.InfoContext(ctx, "Created user notification", "notifID", notif.ID)

	return notif, nil
}

func (impl *BusinessStoreImpl) RetrievePendingUserNotifications(ctx context.Context, since time.Time, maxCount, maxAttempts int) ([]*dbgen.GetPendingUserNotificationsRow, error) {
	if (maxCount <= 0) || since.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	result, err := impl.querier.GetPendingUserNotifications(ctx, &dbgen.GetPendingUserNotificationsParams{
		ScheduledAt:        Timestampz(since),
		Limit:              int32(maxCount),
		ProcessingAttempts: int32(maxAttempts),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			slog.DebugContext(ctx, "No pending notifications found", "since", since)
			return []*dbgen.GetPendingUserNotificationsRow{}, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve pending user notifications", common.ErrAttr(err))

		return nil, err
	}

	slog.DebugContext(ctx, "Retrieved pending user notifications", "count", len(result), "since", since)

	return result, nil
}

func (impl *BusinessStoreImpl) MarkUserNotificationsAttempted(ctx context.Context, ids []int32) error {
	if len(ids) == 0 {
		return nil
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.UpdateAttemptedUserNotifications(ctx, ids); err != nil {
		slog.ErrorContext(ctx, "Failed to update attempted user notifications", "count", len(ids), common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Updated attempted user notifications", "count", len(ids))

	return nil
}

func (impl *BusinessStoreImpl) MarkUserNotificationsProcessed(ctx context.Context, ids []int32, t time.Time) error {
	if (len(ids) == 0) || t.IsZero() {
		return nil
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.UpdateProcessedUserNotifications(ctx, &dbgen.UpdateProcessedUserNotificationsParams{
		ProcessedAt: Timestampz(t),
		Column2:     ids,
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to update processed user notifications", "count", len(ids), common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Updated processed user notifications", "count", len(ids), "processed_at", t)

	return nil
}

func (impl *BusinessStoreImpl) DeleteUnusedNotificationTemplates(ctx context.Context, processedBefore, updatedBefore time.Time) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.DeleteUnusedNotificationTemplates(ctx, &dbgen.DeleteUnusedNotificationTemplatesParams{
		ProcessedAt: Timestampz(processedBefore),
		UpdatedAt:   Timestampz(updatedBefore),
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to delete unused notification templates", common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Deleted unused notification templates", "delivered_before", processedBefore, "updated_before", updatedBefore)

	return nil
}

func (impl *BusinessStoreImpl) DeleteSentUserNotifications(ctx context.Context, before time.Time) error {
	if before.IsZero() {
		return ErrInvalidInput
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.DeleteProcessedUserNotifications(ctx, Timestampz(before)); err != nil {
		slog.ErrorContext(ctx, "Failed to delete sent user notifications", common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Deleted sent user notifications", "before", before)

	return nil
}

func (impl *BusinessStoreImpl) DeleteUnsentUserNotifications(ctx context.Context, before time.Time) error {
	if before.IsZero() {
		return ErrInvalidInput
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.DeleteUnprocessedUserNotifications(ctx, Timestampz(before)); err != nil {
		slog.ErrorContext(ctx, "Failed to delete UNsent user notifications", common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Deleted UNsent user notifications", "before", before)

	return nil
}

func (impl *BusinessStoreImpl) DeletePendingUserNotification(ctx context.Context, user *dbgen.User, referenceID string) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.DeletePendingUserNotification(ctx, &dbgen.DeletePendingUserNotificationParams{
		UserID:      Int(user.ID),
		ReferenceID: referenceID,
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to delete pending user notification", "userID", user.ID, "refID", referenceID, common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Deleted pending user notification", "userID", user.ID, "refID", referenceID)

	return nil
}

func (impl *BusinessStoreImpl) RetrieveTrialUsers(ctx context.Context, from, to time.Time, status string, maxUsers int32, internal bool) ([]*dbgen.User, error) {
	if from.IsZero() || to.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	params := &dbgen.GetTrialUsersParams{
		TrialEndsAt:   Timestampz(from),
		TrialEndsAt_2: Timestampz(to),
		Status:        status,
		Limit:         maxUsers,
	}

	if internal {
		params.Source = dbgen.SubscriptionSourceInternal
	} else {
		params.Source = dbgen.SubscriptionSourceExternal
	}

	users, err := impl.querier.GetTrialUsers(ctx, params)
	if err != nil {
		if err == pgx.ErrNoRows {
			return []*dbgen.User{}, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve trial users", "from", from, "to", to, "status", status, common.ErrAttr(err))

		return nil, err
	}

	slog.DebugContext(ctx, "Fetched trial users", "count", len(users), "from", from, "to", to, "status", status)

	return users, nil
}

func (impl *BusinessStoreImpl) ExpireInternalTrials(ctx context.Context, from, to time.Time, activeStatus, expiredStatus string) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.UpdateInternalSubscriptions(ctx, &dbgen.UpdateInternalSubscriptionsParams{
		TrialEndsAt:   Timestampz(from),
		TrialEndsAt_2: Timestampz(to),
		Status:        expiredStatus,
		Status_2:      activeStatus,
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to expire internal trials", "from", from, "to", to, common.ErrAttr(err))
		return err
	}

	// NOTE: we don't update caches in this case

	slog.InfoContext(ctx, "Expired internal trials", "from", from, "to", to)

	return nil
}

func (impl *BusinessStoreImpl) MoveProperty(ctx context.Context, user *dbgen.User, property *dbgen.Property, org *dbgen.GetUserOrganizationsRow) (*dbgen.Property, *common.AuditLogEvent, error) {
	if impl.querier == nil {
		return nil, nil, ErrMaintenance
	}

	if property.OrgID.Int32 == org.Organization.ID {
		slog.WarnContext(ctx, "Property is already in the destination org", "propID", property.ID, "orgID", property.OrgID.Int32)
		return nil, nil, ErrInvalidInput
	}

	oldOrgID := property.OrgID.Int32

	updatedProperty, err := impl.querier.MoveProperty(ctx, &dbgen.MovePropertyParams{
		ID:         property.ID,
		OrgID:      Int(org.Organization.ID),
		OrgOwnerID: org.Organization.UserID,
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to move property to another org", "propID", property.ID, "oldOrgID", property.OrgID.Int32, "newOrgID", org.Organization.ID, common.ErrAttr(err))
		return nil, nil, err
	}

	slog.InfoContext(ctx, "Moved property to another org", "propID", property.ID, "oldOrgID", property.OrgID.Int32, "newOrgID", org.Organization.ID)

	impl.cacheProperty(ctx, updatedProperty)

	auditEvent := newMovePropertyAuditLogEvent(user, updatedProperty, oldOrgID, updatedProperty.OrgID.Int32)

	return updatedProperty, auditEvent, nil
}

func (impl *BusinessStoreImpl) DeleteOldAuditLogs(ctx context.Context, before time.Time) error {
	if before.IsZero() {
		return ErrInvalidInput
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.DeleteOldAuditLogs(ctx, Timestampz(before)); err != nil {
		slog.ErrorContext(ctx, "Failed to deleted old audit logs", common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Deleted old audit logs", "before", before)

	return nil
}

func (impl *BusinessStoreImpl) GetCachedAuditLogs(ctx context.Context, user *dbgen.User, limit int, after time.Time, cachedAfter time.Time) ([]*dbgen.GetUserAuditLogsRow, error) {
	if (limit <= 0) || after.IsZero() || cachedAfter.IsZero() {
		return nil, ErrInvalidInput
	}

	if after.Before(cachedAfter) {
		slog.ErrorContext(ctx, "Audit logs cutoff date should be always after cache key", "after", after, "cachedAfter", cachedAfter)
		return nil, ErrInvalidInput
	}

	cacheKey := userAuditLogsCacheKey(user.ID, cachedAfter.Format(time.DateOnly))
	if logs, err := FetchCachedArray[dbgen.GetUserAuditLogsRow](ctx, impl.cache, cacheKey); err == nil {
		if after.Equal(cachedAfter) {
			return logs, nil
		}

		result := make([]*dbgen.GetUserAuditLogsRow, 0, len(logs))
		for _, log := range logs {
			if log.AuditLog.CreatedAt.Valid && !log.AuditLog.CreatedAt.Time.Before(after) {
				result = append(result, log)

				if len(result) >= limit {
					break
				}
			}
		}
		return result, nil
	} else {
		return nil, err
	}
}

func (impl *BusinessStoreImpl) RetrieveUserAuditLogs(ctx context.Context, user *dbgen.User, limit int, after time.Time) ([]*dbgen.GetUserAuditLogsRow, error) {
	if (limit <= 0) || after.IsZero() {
		return nil, ErrInvalidInput
	}

	reader := &StoreArrayReader[*dbgen.GetUserAuditLogsParams, dbgen.GetUserAuditLogsRow]{
		CacheKey: userAuditLogsCacheKey(user.ID, after.Format(time.DateOnly)),
		Cache:    impl.cache,
		TTL:      5 * time.Minute,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = func(ck CacheKey) (*dbgen.GetUserAuditLogsParams, error) {
			return &dbgen.GetUserAuditLogsParams{
				UserID:    Int(user.ID),
				Offset:    0,
				Limit:     int32(limit),
				CreatedAt: Timestampz(after),
			}, nil
		}
		reader.QueryFunc = impl.querier.GetUserAuditLogs
	}

	return reader.Read(ctx)
}

func (impl *BusinessStoreImpl) RetrievePropertyAuditLogs(ctx context.Context, property *dbgen.Property, limit int) ([]*dbgen.GetPropertyAuditLogsRow, error) {
	if limit <= 0 {
		return nil, ErrInvalidInput
	}

	reader := &StoreArrayReader[*dbgen.GetPropertyAuditLogsParams, dbgen.GetPropertyAuditLogsRow]{
		CacheKey: propertyAuditLogsCacheKey(property.ID),
		Cache:    impl.cache,
		TTL:      5 * time.Minute,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = func(ck CacheKey) (*dbgen.GetPropertyAuditLogsParams, error) {
			return &dbgen.GetPropertyAuditLogsParams{
				EntityID:  Int8(int64(property.ID)),
				CreatedAt: property.CreatedAt,
				Offset:    0,
				Limit:     int32(limit),
			}, nil
		}
		reader.QueryFunc = impl.querier.GetPropertyAuditLogs
	}

	logs, err := reader.Read(ctx)
	if err != nil {
		return nil, err
	}

	// extra slice is due to possible caching in portal where for /auditlogs we can fetch and cache multiple entries
	// but later for /property/auditlogs we will show only up to {limit}
	return logs[0:min(len(logs), limit)], nil
}

func (impl *BusinessStoreImpl) RetrieveOrganizationAuditLogs(ctx context.Context, org *dbgen.Organization, limit int) ([]*dbgen.GetOrgAuditLogsRow, error) {
	if limit <= 0 {
		return nil, ErrInvalidInput
	}

	reader := &StoreArrayReader[*dbgen.GetOrgAuditLogsParams, dbgen.GetOrgAuditLogsRow]{
		CacheKey: orgAuditLogsCacheKey(org.ID),
		Cache:    impl.cache,
		TTL:      5 * time.Minute,
	}

	if impl.querier != nil {
		reader.QueryKeyFunc = func(ck CacheKey) (*dbgen.GetOrgAuditLogsParams, error) {
			return &dbgen.GetOrgAuditLogsParams{
				EntityID:  Int8(int64(org.ID)),
				CreatedAt: org.CreatedAt,
				Offset:    0,
				Limit:     int32(limit),
			}, nil
		}
		reader.QueryFunc = impl.querier.GetOrgAuditLogs
	}

	logs, err := reader.Read(ctx)
	if err != nil {
		return nil, err
	}

	// extra slice is due to possible caching in portal where for /auditlogs we can fetch and cache multiple entries
	// but later for /org/auditlogs we will show only up to {limit}
	return logs[0:min(len(logs), limit)], nil
}

func (impl *BusinessStoreImpl) ValidateOrgName(ctx context.Context, name string, user *dbgen.User) common.StatusCode {
	const maxOrgNameLength = 255

	if (len(name) == 0) || (len(name) > maxOrgNameLength) {
		slog.WarnContext(ctx, "Name length is invalid", "length", len(name))

		if len(name) == 0 {
			return common.StatusOrgNameEmptyError
		} else {
			return common.StatusOrgNameTooLongError
		}
	}

	const allowedPunctuation = "'-_&.:()[]"

	for i, r := range name {
		switch {
		case unicode.IsLetter(r):
			continue
		case unicode.IsDigit(r):
			continue
		case unicode.IsSpace(r):
			continue
		case strings.ContainsRune(allowedPunctuation, r):
			continue
		default:
			slog.WarnContext(ctx, "Name contains invalid characters", "position", i, "rune", r)
			return common.StatusOrgNameInvalidSymbolsError
		}
	}

	if _, err := impl.FindOrg(ctx, name, user); err != ErrRecordNotFound {
		slog.WarnContext(ctx, "Org already exists", "name", name, common.ErrAttr(err))
		return common.StatusOrgNameDuplicateError
	}

	return common.StatusOK
}

func (impl *BusinessStoreImpl) ValidatePropertyName(ctx context.Context, name string, org *dbgen.Organization) common.StatusCode {
	const maxPropertyNameLength = 255
	if (len(name) == 0) || (len(name) > maxPropertyNameLength) {
		slog.WarnContext(ctx, "Name length is invalid", "length", len(name))

		if len(name) == 0 {
			return common.StatusPropertyNameEmptyError
		} else {
			return common.StatusPropertyNameTooLongError
		}
	}

	const allowedPunctuation = "'-_.:()[]"

	for i, r := range name {
		switch {
		case unicode.IsLetter(r):
			continue
		case unicode.IsDigit(r):
			continue
		case unicode.IsSpace(r):
			continue
		case strings.ContainsRune(allowedPunctuation, r):
			continue
		default:
			slog.WarnContext(ctx, "Name contains invalid characters", "position", i, "rune", r)
			return common.StatusPropertyNameInvalidSymbolsError
		}
	}

	if org != nil {
		if _, err := impl.FindOrgProperty(ctx, name, org); err != ErrRecordNotFound {
			slog.WarnContext(ctx, "Property already exists", "name", name, common.ErrAttr(err))
			return common.StatusPropertyNameDuplicateError
		}
	}

	return common.StatusOK
}

func (impl *BusinessStoreImpl) CreateNewAsyncTask(ctx context.Context, data interface{}, handler string, user *dbgen.User, scheduledAt time.Time, referenceID string) (*dbgen.AsyncTask, error) {
	if (data == nil) && (user == nil) {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	payload, err := json.Marshal(data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize payload for async request", common.ErrAttr(err))
		return nil, err
	}

	if scheduledAt.IsZero() {
		scheduledAt = time.Now().UTC()
	}

	params := &dbgen.CreateAsyncTaskParams{
		Input:       payload,
		Handler:     handler,
		ReferenceID: referenceID,
		ScheduledAt: Timestampz(scheduledAt),
	}
	if user != nil {
		params.UserID = Int(user.ID)
	}

	tnow := time.Now().UTC()
	uuid, err := impl.querier.CreateAsyncTask(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create async request", common.ErrAttr(err))
		return nil, err
	}

	taskIDStr := UUIDToString(uuid)

	slog.DebugContext(ctx, "Created async task", "taskID", taskIDStr)

	// yes, we "fake" the response in order not to make Postgres to send (potentially large) inputs copy back
	task := &dbgen.AsyncTask{
		ID:                 uuid,
		Handler:            handler,
		Input:              payload,
		Output:             nil,
		UserID:             params.UserID,
		ReferenceID:        referenceID,
		ProcessingAttempts: 0,
		CreatedAt:          Timestampz(tnow),
		ScheduledAt:        params.ScheduledAt,
		ProcessedAt:        pgtype.Timestamptz{},
	}

	cacheKey := asyncTaskCacheKey(taskIDStr)
	_ = impl.cache.SetWithTTL(ctx, cacheKey, task, asyncTaskTTL)

	return task, nil
}

func (impl *BusinessStoreImpl) RetrieveAsyncTask(ctx context.Context, uuid pgtype.UUID, user *dbgen.User) (*dbgen.AsyncTask, error) {
	reader := &StoreOneReader[pgtype.UUID, dbgen.AsyncTask]{
		CacheKey: asyncTaskCacheKey(UUIDToString(uuid)),
		Cache:    impl.cache,
	}

	if impl.querier != nil {
		reader.QueryFunc = impl.querier.GetAsyncTask
		reader.QueryKeyFunc = queryKeyStringUUID
	}

	task, err := reader.Read(ctx)
	if err != nil {
		return nil, err
	}

	if task.UserID.Valid && (user != nil) && (task.UserID.Int32 != user.ID) {
		return nil, ErrPermissions
	}

	return task, nil
}

func (impl *BusinessStoreImpl) RetrievePendingAsyncTasks(ctx context.Context, count int, before time.Time, maxProcessingAttempts int) ([]*dbgen.GetPendingAsyncTasksRow, error) {
	if count <= 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	tasks, err := impl.querier.GetPendingAsyncTasks(ctx, &dbgen.GetPendingAsyncTasksParams{
		ScheduledAt:        Timestampz(before),
		ProcessingAttempts: int32(maxProcessingAttempts),
		Limit:              int32(count),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return []*dbgen.GetPendingAsyncTasksRow{}, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve async tasks", "before", before, "count", count, "attemps", maxProcessingAttempts, common.ErrAttr(err))

		return nil, err
	}

	slog.DebugContext(ctx, "Fetched pending async tasks", "count", len(tasks))

	return tasks, nil
}

func (impl *BusinessStoreImpl) DeleteOldAsyncTasks(ctx context.Context, before time.Time) error {
	if before.IsZero() {
		return ErrInvalidInput
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.DeleteOldAsyncTasks(ctx, Timestampz(before)); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old async request", common.ErrAttr(err))
		return err
	}

	return nil
}

func (impl *BusinessStoreImpl) UpdateAsyncTask(ctx context.Context, uuid pgtype.UUID, output []byte, processedAt time.Time) error {
	if !uuid.Valid {
		return ErrInvalidInput
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.UpdateAsyncTask(ctx, &dbgen.UpdateAsyncTaskParams{
		ID:          uuid,
		Output:      output,
		ProcessedAt: Timestampz(processedAt), // if processedAt.IsZero(), we set to NULL
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to update async task", "id", UUIDToString(uuid), common.ErrAttr(err))
		return err
	}

	cacheKey := asyncTaskCacheKey(UUIDToString(uuid))
	impl.cache.Delete(ctx, cacheKey)

	return nil
}

func (impl *BusinessStoreImpl) RetrieveOrgOwnerWithSubscription(ctx context.Context, org *dbgen.Organization, activeUser *dbgen.User) (owner *dbgen.User, subscr *dbgen.Subscription, err error) {
	isUserOrgOwner := org.UserID.Valid && (org.UserID.Int32 == activeUser.ID)

	if isUserOrgOwner {
		owner = activeUser
		if activeUser.SubscriptionID.Valid {
			subscr, err = impl.RetrieveSubscription(ctx, activeUser.SubscriptionID.Int32)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to retrieve active user subscription", "userID", activeUser.ID, common.ErrAttr(err))
				return nil, nil, err
			}
		}
	} else {
		slog.DebugContext(ctx, "Active user is not org owner", "userID", activeUser.ID, "orgUserID", org.UserID.Int32)

		orgUser, err := impl.RetrieveUser(ctx, org.UserID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve org's owner user by ID", "id", org.UserID.Int32, common.ErrAttr(err))
			return nil, nil, err
		}

		owner = orgUser
		subscr = nil

		if orgUser.SubscriptionID.Valid {
			subscr, err = impl.RetrieveSubscription(ctx, orgUser.SubscriptionID.Int32)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to retrieve org owner's subscription", "userID", org.UserID.Int32, common.ErrAttr(err))
				return nil, nil, err
			}
		}
	}

	return
}

func (impl *BusinessStoreImpl) RetrieveOrgPropertiesCount(ctx context.Context, orgID int32) (int64, error) {
	if impl.querier == nil {
		return 0, ErrMaintenance
	}

	cacheKey := orgPropertiesCountCacheKey(orgID)
	if count, err := FetchCachedOne[int64](ctx, impl.cache, cacheKey); err == nil {
		return *count, nil
	}

	count, err := impl.querier.GetOrgPropertiesCount(ctx, Int(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org properties count", "orgID", orgID, common.ErrAttr(err))
		return 0, err
	}

	slog.DebugContext(ctx, "Fetched org properties count", "orgID", orgID, "count", count)

	const propertiesCountTTL = 5 * time.Minute
	c := new(int64)
	*c = count
	_ = impl.cache.SetWithTTL(ctx, cacheKey, c, propertiesCountTTL)

	return count, nil
}
