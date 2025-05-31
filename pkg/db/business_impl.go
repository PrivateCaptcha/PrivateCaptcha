package db

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// NOTE: this is the time during which changes to difficulty will propagate when we have multiple API nodes
	propertyTTL = 30 * time.Minute
	apiKeyTTL   = 30 * time.Minute
)

var (
	errUnsupported  = errors.New("not supported")
	emptyOrgUsers   = []*dbgen.GetOrganizationUsersRow{}
	emptyAPIKeys    = []*dbgen.APIKey{}
	emptyUserOrgs   = []*dbgen.GetUserOrganizationsRow{}
	emptyProperties = []*dbgen.Property{}
	// shortcuts for nullable access levels
	nullAccessLevelNull   = dbgen.NullAccessLevel{Valid: false}
	nullAccessLevelOwner  = dbgen.NullAccessLevel{Valid: true, AccessLevel: dbgen.AccessLevelOwner}
	nullAccessLevelMember = dbgen.NullAccessLevel{Valid: true, AccessLevel: dbgen.AccessLevelMember}
)

func fetchCachedOne[T any](ctx context.Context, cache common.Cache[CacheKey, any], key CacheKey) (*T, error) {
	data, err := cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if t, ok := data.(*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

func fetchCachedMany[T any](ctx context.Context, cache common.Cache[CacheKey, any], key CacheKey) ([]*T, error) {
	data, err := cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if t, ok := data.([]*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

type txCacheArg struct {
	item any
	ttl  time.Duration
}

type TxCache struct {
	set     map[CacheKey]*txCacheArg
	del     map[CacheKey]struct{}
	missing map[CacheKey]time.Duration
}

func NewTxCache() *TxCache {
	return &TxCache{
		set:     make(map[CacheKey]*txCacheArg),
		del:     make(map[CacheKey]struct{}),
		missing: make(map[CacheKey]time.Duration),
	}
}

func (c *TxCache) Get(ctx context.Context, key CacheKey) (any, error) { return nil, errUnsupported }
func (c *TxCache) SetMissing(ctx context.Context, key CacheKey, ttl time.Duration) error {
	c.missing[key] = ttl
	return nil
}
func (c *TxCache) Set(ctx context.Context, key CacheKey, t any, ttl time.Duration) error {
	c.set[key] = &txCacheArg{item: t, ttl: ttl}
	return nil
}
func (c *TxCache) Delete(ctx context.Context, key CacheKey) error {
	c.del[key] = struct{}{}
	return nil
}

func (c *TxCache) Commit(ctx context.Context, cache common.Cache[CacheKey, any]) {
	for key := range c.del {
		if err := cache.Delete(ctx, key); err != nil {
			slog.ErrorContext(ctx, "Failed to delete from cache", "key", key, common.ErrAttr(err))
		}
	}

	for key, value := range c.missing {
		if err := cache.SetMissing(ctx, key, value); err != nil {
			slog.ErrorContext(ctx, "Failed to set missing in cache", "key", key, common.ErrAttr(err))
		}
	}

	for key, value := range c.set {
		if err := cache.Set(ctx, key, value.item, value.ttl); err != nil {
			slog.ErrorContext(ctx, "Failed to set in cache", "key", key, common.ErrAttr(err))
		}
	}
}

type BusinessStoreImpl struct {
	querier dbgen.Querier
	cache   common.Cache[CacheKey, any]
	ttl     time.Duration
}

func (impl *BusinessStoreImpl) RetrieveFromCache(ctx context.Context, key string) ([]byte, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	data, err := impl.querier.GetCachedByKey(ctx, key)
	if err == pgx.ErrNoRows {
		return nil, ErrCacheMiss
	} else if err != nil {
		slog.ErrorContext(ctx, "Failed to read Paddle prices", common.ErrAttr(err))
		return nil, err
	}

	return data, nil
}

func (impl *BusinessStoreImpl) StoreInCache(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	if len(data) == 0 {
		return ErrInvalidInput
	}

	if impl.querier == nil {
		return ErrMaintenance
	}

	return impl.querier.CreateCache(ctx, &dbgen.CreateCacheParams{
		Key:     key,
		Value:   data,
		Column3: ttl,
	})
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

func (impl *BusinessStoreImpl) createNewSubscription(ctx context.Context, params *dbgen.CreateSubscriptionParams) (*dbgen.Subscription, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	subscription, err := impl.querier.CreateSubscription(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create a subscription in DB", common.ErrAttr(err))
		return nil, err
	}

	if subscription != nil {
		cacheKey := subscriptionCacheKey(subscription.ID)
		_ = impl.cache.Set(ctx, cacheKey, subscription, impl.ttl)
	}

	return subscription, nil
}

func (impl *BusinessStoreImpl) createNewUser(ctx context.Context, email, name string, subscriptionID *int32) (*dbgen.User, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	params := &dbgen.CreateUserParams{
		Name:  name,
		Email: email,
	}

	if subscriptionID != nil {
		params.SubscriptionID = Int(*subscriptionID)
	}

	user, err := impl.querier.CreateUser(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create user in DB", "email", email, common.ErrAttr(err))
		return nil, err
	}

	if user != nil {
		slog.DebugContext(ctx, "Created user in DB", "email", email, "id", user.ID)

		// we need to update cache as we just set user as missing when checking for it's existence
		cacheKey := userCacheKey(user.ID)
		_ = impl.cache.Set(ctx, cacheKey, user, impl.ttl)
	}

	return user, nil
}

func (impl *BusinessStoreImpl) CreateNewOrganization(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	org, err := impl.querier.CreateOrganization(ctx, &dbgen.CreateOrganizationParams{
		Name:   name,
		UserID: Int(userID),
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create organization in DB", "name", name, common.ErrAttr(err))
		return nil, err
	}

	if org != nil {
		slog.DebugContext(ctx, "Created organization in DB", "name", name, "id", org.ID)

		cacheKey := orgCacheKey(org.ID)
		_ = impl.cache.Set(ctx, cacheKey, org, impl.ttl)

		// invalidate user orgs in cache as we just created another one
		_ = impl.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))
	}

	return org, nil
}

func (impl *BusinessStoreImpl) SoftDeleteUser(ctx context.Context, userID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	user, err := impl.querier.SoftDeleteUser(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user", "userID", userID, common.ErrAttr(err))
		return err
	} else {
		slog.DebugContext(ctx, "Soft-deleted user", "userID", userID)
	}

	if err := impl.querier.SoftDeleteUserOrganizations(ctx, Int(userID)); err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user organizations", "userID", userID, common.ErrAttr(err))
		return err
	} else {
		slog.DebugContext(ctx, "Soft-deleted user organizations", "userID", userID)
	}

	if err := impl.querier.DeleteUserAPIKeys(ctx, Int(userID)); err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user API keys", "userID", userID, common.ErrAttr(err))
		return err
	} else {
		slog.DebugContext(ctx, "Disabled user API keys", "userID", userID)
	}

	// TODO: Delete user API keys from cache

	// invalidate user caches
	userOrgsCacheKey := userOrgsCacheKey(userID)
	if orgs, err := fetchCachedMany[dbgen.GetUserOrganizationsRow](ctx, impl.cache, userOrgsCacheKey); err == nil {
		for _, org := range orgs {
			_ = impl.cache.Delete(ctx, orgCacheKey(org.Organization.ID))
			_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(org.Organization.ID))
		}
		_ = impl.cache.Delete(ctx, userOrgsCacheKey)
	}

	_ = impl.cache.Delete(ctx, userCacheKey(user.ID))

	return nil
}

func (impl *BusinessStoreImpl) getCachedPropertyBySitekey(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	eid := UUIDFromSiteKey(sitekey)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	cacheKey := PropertyBySitekeyCacheKey(sitekey)

	if property, err := fetchCachedOne[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
		return property, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	} else {
		return nil, err
	}
}

func (impl *BusinessStoreImpl) RetrievePropertiesBySitekey(ctx context.Context, sitekeys map[string]struct{}) ([]*dbgen.Property, error) {
	keys := make([]pgtype.UUID, 0, len(sitekeys))
	keysMap := make(map[string]bool)
	result := make([]*dbgen.Property, 0, len(sitekeys))

	for sitekey := range sitekeys {
		eid := UUIDFromSiteKey(sitekey)
		if !eid.Valid {
			continue
		}

		cacheKey := PropertyBySitekeyCacheKey(sitekey)
		if property, err := fetchCachedOne[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
			result = append(result, property)
			continue
		} else if err == ErrNegativeCacheHit {
			continue
		}

		keys = append(keys, eid)
		keysMap[sitekey] = true
	}

	if len(keys) == 0 {
		if len(result) > 0 {
			slog.DebugContext(ctx, "All properties are cached", "count", len(result))
			return result, nil
		}

		slog.WarnContext(ctx, "No valid sitekeys to fetch from DB")
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return result, ErrMaintenance
	}

	properties, err := impl.querier.GetPropertiesByExternalID(ctx, keys)
	if err != nil && err != pgx.ErrNoRows {
		slog.ErrorContext(ctx, "Failed to retrieve properties by sitekeys", common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched properties from DB by sitekeys", "count", len(properties))

	for _, p := range properties {
		sitekey := UUIDToSiteKey(p.ExternalID)
		cacheKey := PropertyBySitekeyCacheKey(sitekey)
		_ = impl.cache.Set(ctx, cacheKey, p, propertyTTL)
		delete(keysMap, sitekey)
	}

	for missingKey := range keysMap {
		_ = impl.cache.SetMissing(ctx, PropertyBySitekeyCacheKey(missingKey), impl.ttl)
	}

	result = append(result, properties...)

	return result, nil
}

func (impl *BusinessStoreImpl) GetCachedAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	cacheKey := APIKeyCacheKey(secret)

	if apiKey, err := fetchCachedOne[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return apiKey, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	} else {
		return nil, err
	}
}

// Fetches API keyfrom DB, backed by cache
func (impl *BusinessStoreImpl) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	cacheKey := APIKeyCacheKey(secret)

	if apiKey, err := fetchCachedOne[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return apiKey, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	eid := UUIDFromSecret(secret)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	apiKey, err := impl.querier.GetAPIKeyByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve API Key by external ID", "secret", secret, common.ErrAttr(err))

		return nil, err
	}

	if apiKey != nil {
		_ = impl.cache.Set(ctx, cacheKey, apiKey, apiKeyTTL)
	}

	return apiKey, nil
}

func (impl *BusinessStoreImpl) retrieveUser(ctx context.Context, userID int32) (*dbgen.User, error) {
	cacheKey := userCacheKey(userID)
	if user, err := fetchCachedOne[dbgen.User](ctx, impl.cache, cacheKey); err == nil {
		return user, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	user, err := impl.querier.GetUserByID(ctx, userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve user by ID", "id", userID, common.ErrAttr(err))

		return nil, err
	}

	if user != nil {
		_ = impl.cache.Set(ctx, cacheKey, user, impl.ttl)
	}

	return user, nil
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
		cacheKey := userCacheKey(user.ID)
		_ = impl.cache.Set(ctx, cacheKey, user, impl.ttl)
	}

	return user, nil
}

func (impl *BusinessStoreImpl) FindUserBySubscriptionID(ctx context.Context, subscriptionID int32) (*dbgen.User, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	user, err := impl.querier.GetUserBySubscriptionID(ctx, Int(subscriptionID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve user by subscriptionID", "subscriptionID", subscriptionID, common.ErrAttr(err))

		return nil, err
	}

	if user != nil {
		cacheKey := userCacheKey(user.ID)
		_ = impl.cache.Set(ctx, cacheKey, user, impl.ttl)
	}

	return user, nil
}

func (impl *BusinessStoreImpl) RetrieveUserOrganizations(ctx context.Context, userID int32) ([]*dbgen.GetUserOrganizationsRow, error) {
	cacheKey := userOrgsCacheKey(userID)

	if orgs, err := fetchCachedMany[dbgen.GetUserOrganizationsRow](ctx, impl.cache, cacheKey); err == nil {
		return orgs, nil
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	orgs, err := impl.querier.GetUserOrganizations(ctx, Int(userID))
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.Set(ctx, cacheKey, emptyUserOrgs, impl.ttl)
			return emptyUserOrgs, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve orgs by user ID", "userID", userID, common.ErrAttr(err))

		return nil, err
	}

	// NOTE: We sort here instead in SQL to avoid confusing sqlc by ordering the UNION ALL result as a subquery
	sort.Slice(orgs, func(i, j int) bool {
		return orgs[i].Organization.CreatedAt.Time.Before(orgs[j].Organization.CreatedAt.Time)
	})

	if len(orgs) > 0 {
		_ = impl.cache.Set(ctx, cacheKey, orgs, impl.ttl)
	}

	slog.DebugContext(ctx, "Retrieved user organizations", "count", len(orgs))

	// TODO: Also sort by orgs that have any properties in them
	return orgs, nil
}

func (impl *BusinessStoreImpl) retrieveOrganizationWithAccess(ctx context.Context, userID, orgID int32) (*dbgen.Organization, dbgen.NullAccessLevel, error) {
	cacheKey := orgCacheKey(orgID)

	if org, err := fetchCachedOne[dbgen.Organization](ctx, impl.cache, cacheKey); err == nil {
		if org.UserID.Int32 == userID {
			return org, nullAccessLevelOwner, nil
		}
		// NOTE: for security reasons, we want to verify that this user has rights to get this org

		// this value should be in cache if user opens "Members" tab in the org
		if users, err := fetchCachedMany[dbgen.GetOrganizationUsersRow](ctx, impl.cache, orgUsersCacheKey(orgID)); err == nil {
			if hasUser := slices.ContainsFunc(users, func(u *dbgen.GetOrganizationUsersRow) bool { return u.User.ID == userID }); hasUser {
				slog.Log(ctx, common.LevelTrace, "Found cached org from organization users", "orgID", orgID, "userID", userID)
				return org, nullAccessLevelMember, nil
			}
		}
	} else if err == ErrNegativeCacheHit {
		return nil, nullAccessLevelNull, ErrNegativeCacheHit
	}

	// this value should be in cache for "normal" use-cases (e.g. user logs in to the portal)
	if orgs, err := fetchCachedMany[dbgen.GetUserOrganizationsRow](ctx, impl.cache, userOrgsCacheKey(userID)); err == nil {
		if index := slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool { return o.Organization.ID == orgID }); index != -1 {
			slog.Log(ctx, common.LevelTrace, "Found cached org from user organizations", "orgID", orgID, "userID", userID)
			org := &dbgen.Organization{}
			*org = orgs[index].Organization
			_ = impl.cache.Set(ctx, cacheKey, org, impl.ttl)

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
			_ = impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
			return nil, nullAccessLevelNull, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve organization by ID", "orgID", orgID, common.ErrAttr(err))

		return nil, nullAccessLevelNull, err
	}

	// when sqlc will be able to do embed() as a pointer, we can remove this copying
	org := &dbgen.Organization{}
	*org = orgAndAccess.Organization

	_ = impl.cache.Set(ctx, cacheKey, org, impl.ttl)

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
	_ = impl.cache.Set(ctx, key, property, impl.ttl)
	sitekey := UUIDToSiteKey(property.ExternalID)
	_ = impl.cache.Set(ctx, PropertyBySitekeyCacheKey(sitekey), property, propertyTTL)
}

func (impl *BusinessStoreImpl) retrieveOrgProperty(ctx context.Context, orgID, propID int32) (*dbgen.Property, error) {
	cacheKey := propertyByIDCacheKey(propID)

	if prop, err := fetchCachedOne[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
		return prop, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if properties, err := fetchCachedMany[dbgen.Property](ctx, impl.cache, orgPropertiesCacheKey(orgID)); err == nil {
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
			_ = impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by ID", "propID", propID, common.ErrAttr(err))

		return nil, err
	}

	impl.cacheProperty(ctx, property)

	return property, nil
}

func (impl *BusinessStoreImpl) RetrieveSubscription(ctx context.Context, sID int32) (*dbgen.Subscription, error) {
	cacheKey := subscriptionCacheKey(sID)
	if subscription, err := fetchCachedOne[dbgen.Subscription](ctx, impl.cache, cacheKey); err == nil {
		return subscription, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	subscription, err := impl.querier.GetSubscriptionByID(ctx, sID)
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to fetch subscription from DB", "id", sID, common.ErrAttr(err))

		return nil, err
	}

	if subscription != nil {
		_ = impl.cache.Set(ctx, cacheKey, subscription, impl.ttl)
	}

	return subscription, nil
}

func (impl *BusinessStoreImpl) UpdateSubscription(ctx context.Context, params *dbgen.UpdateSubscriptionParams) (*dbgen.Subscription, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	subscription, err := impl.querier.UpdateSubscription(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update subscription in DB", "externalSubscriptionID", params.ExternalSubscriptionID, common.ErrAttr(err))
		return nil, err
	}

	if subscription != nil {
		slog.DebugContext(ctx, "Updated subscription in DB", "id", subscription.ID, "status", subscription.Status)

		cacheKey := subscriptionCacheKey(subscription.ID)
		_ = impl.cache.Set(ctx, cacheKey, subscription, impl.ttl)
	}

	return subscription, nil
}

func (impl *BusinessStoreImpl) FindOrgProperty(ctx context.Context, name string, orgID int32) (*dbgen.Property, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.querier.GetOrgPropertyByName(ctx, &dbgen.GetOrgPropertyByNameParams{
		OrgID: Int(orgID),
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

func (impl *BusinessStoreImpl) FindOrg(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	org, err := impl.querier.FindUserOrgByName(ctx, &dbgen.FindUserOrgByNameParams{
		UserID: Int(userID),
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

func (impl *BusinessStoreImpl) CreateNewProperty(ctx context.Context, params *dbgen.CreatePropertyParams) (*dbgen.Property, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.querier.CreateProperty(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property in DB", "name", params.Name, "org", params.OrgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Created new property", "id", property.ID, "name", params.Name, "org", params.OrgID)

	cacheKey := propertyByIDCacheKey(property.ID)
	_ = impl.cache.Set(ctx, cacheKey, property, impl.ttl)
	sitekey := UUIDToSiteKey(property.ExternalID)
	_ = impl.cache.Set(ctx, PropertyBySitekeyCacheKey(sitekey), property, propertyTTL)
	// invalidate org properties in cache as we just created a new property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(params.OrgID.Int32))

	return property, nil
}

func (impl *BusinessStoreImpl) UpdateProperty(ctx context.Context, params *dbgen.UpdatePropertyParams) (*dbgen.Property, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.querier.UpdateProperty(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update property in DB", "name", params.Name, "propID", params.ID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Updated property", "name", params.Name, "propID", params.ID)

	sitekey := UUIDToSiteKey(property.ExternalID)
	cacheBySitekeyKey := PropertyBySitekeyCacheKey(sitekey)
	_ = impl.cache.Set(ctx, cacheBySitekeyKey, property, propertyTTL)

	cacheByIDKey := propertyByIDCacheKey(property.ID)
	_ = impl.cache.Set(ctx, cacheByIDKey, property, impl.ttl)
	// invalidate org properties in cache as we just created a new property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(property.OrgID.Int32))

	return property, nil
}

func (impl *BusinessStoreImpl) SoftDeleteProperty(ctx context.Context, propID int32, orgID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	property, err := impl.querier.SoftDeleteProperty(ctx, propID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to mark property as deleted in DB", "propID", propID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted property", "propID", propID)

	// update caches
	sitekey := UUIDToSiteKey(property.ExternalID)
	// cache mostly used in API server
	_ = impl.cache.SetMissing(ctx, PropertyBySitekeyCacheKey(sitekey), impl.ttl)
	_ = impl.cache.SetMissing(ctx, propertyByIDCacheKey(propID), impl.ttl)
	// invalidate org properties in cache as we just deleted a property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(orgID))

	return nil
}

func (impl *BusinessStoreImpl) RetrieveOrgProperties(ctx context.Context, orgID int32) ([]*dbgen.Property, error) {
	cacheKey := orgPropertiesCacheKey(orgID)

	if properties, err := fetchCachedMany[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
		return properties, nil
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	properties, err := impl.querier.GetOrgProperties(ctx, Int(orgID))
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.Set(ctx, cacheKey, emptyProperties, impl.ttl)
			return emptyProperties, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve org properties", "org", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.Log(ctx, common.LevelTrace, "Retrieved properties", "count", len(properties))
	if len(properties) > 0 {
		_ = impl.cache.Set(ctx, cacheKey, properties, impl.ttl)
	}

	return properties, err
}

func (impl *BusinessStoreImpl) UpdateOrganization(ctx context.Context, orgID int32, name string) (*dbgen.Organization, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	org, err := impl.querier.UpdateOrganization(ctx, &dbgen.UpdateOrganizationParams{
		Name: name,
		ID:   orgID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update org in DB", "name", name, "orgID", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Updated organization", "name", name, "orgID", orgID)

	cacheKey := orgCacheKey(org.ID)
	_ = impl.cache.Set(ctx, cacheKey, org, impl.ttl)
	// invalidate user orgs in cache as we just updated name
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))

	return org, nil
}

func (impl *BusinessStoreImpl) SoftDeleteOrganization(ctx context.Context, orgID int32, userID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	if err := impl.querier.SoftDeleteUserOrganization(ctx, &dbgen.SoftDeleteUserOrganizationParams{
		ID:     orgID,
		UserID: Int(userID),
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to mark organization as deleted in DB", "orgID", orgID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted organization", "orgID", orgID)

	// update caches
	_ = impl.cache.SetMissing(ctx, orgCacheKey(orgID), impl.ttl)
	// invalidate user orgs in cache as we just deleted one
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(userID))

	return nil
}

// NOTE: by definition this does not include the owner as this relationship is set directly in the 'organizations' table
func (impl *BusinessStoreImpl) RetrieveOrganizationUsers(ctx context.Context, orgID int32) ([]*dbgen.GetOrganizationUsersRow, error) {
	cacheKey := orgUsersCacheKey(orgID)

	if users, err := fetchCachedMany[dbgen.GetOrganizationUsersRow](ctx, impl.cache, cacheKey); err == nil {
		return users, nil
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	users, err := impl.querier.GetOrganizationUsers(ctx, orgID)
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.Set(ctx, cacheKey, emptyOrgUsers, impl.ttl)
			return emptyOrgUsers, nil
		}
		slog.ErrorContext(ctx, "Failed to fetch organization users", "orgID", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched organization users", "orgID", orgID, "count", len(users))

	if len(users) > 0 {
		_ = impl.cache.Set(ctx, cacheKey, users, impl.ttl)
	}

	return users, nil
}

func (impl *BusinessStoreImpl) InviteUserToOrg(ctx context.Context, orgID int32, userID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	_, err := impl.querier.InviteUserToOrg(ctx, &dbgen.InviteUserToOrgParams{
		OrgID:  orgID,
		UserID: userID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to add user to org", "orgID", orgID, "userID", userID, common.ErrAttr(err))
	}

	// invalidate relevant caches
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = impl.cache.Delete(ctx, orgUsersCacheKey(orgID))

	slog.DebugContext(ctx, "Added org membership invite", "orgID", orgID, "userID", userID)

	return nil
}

func (impl *BusinessStoreImpl) JoinOrg(ctx context.Context, orgID int32, userID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.UpdateOrgMembershipLevel(ctx, &dbgen.UpdateOrgMembershipLevelParams{
		OrgID:   orgID,
		UserID:  userID,
		Level:   dbgen.AccessLevelMember,
		Level_2: dbgen.AccessLevelInvited,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to accept org invite", "orgID", orgID, "userID", userID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Accepted org invite", "orgID", orgID, "userID", userID)

	// invalidate relevant caches
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = impl.cache.Delete(ctx, orgUsersCacheKey(orgID))

	return nil
}

func (impl *BusinessStoreImpl) LeaveOrg(ctx context.Context, orgID int32, userID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.UpdateOrgMembershipLevel(ctx, &dbgen.UpdateOrgMembershipLevelParams{
		OrgID:   orgID,
		UserID:  userID,
		Level:   dbgen.AccessLevelInvited,
		Level_2: dbgen.AccessLevelMember,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to leave org", "orgID", orgID, "userID", userID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Left organization", "orgID", orgID, "userID", userID)

	// invalidate relevant caches
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = impl.cache.Delete(ctx, orgUsersCacheKey(orgID))

	return nil
}

func (impl *BusinessStoreImpl) RemoveUserFromOrg(ctx context.Context, orgID int32, userID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.RemoveUserFromOrg(ctx, &dbgen.RemoveUserFromOrgParams{
		OrgID:  orgID,
		UserID: userID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to remove user from org", "orgID", orgID, "userID", userID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Removed user from org", "orgID", orgID, "userID", userID)

	// invalidate relevant caches
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = impl.cache.Delete(ctx, orgUsersCacheKey(orgID))

	return nil
}

func (impl *BusinessStoreImpl) updateUserSubscription(ctx context.Context, userID, subscriptionID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	user, err := impl.querier.UpdateUserSubscription(ctx, &dbgen.UpdateUserSubscriptionParams{
		ID:             userID,
		SubscriptionID: Int(subscriptionID),
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update user subscription", "userID", userID, "subscriptionID", subscriptionID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Updated user subscription", "userID", userID, "subscriptionID", subscriptionID)

	if user != nil {
		_ = impl.cache.Set(ctx, userCacheKey(user.ID), user, impl.ttl)
	}

	return nil
}

func (impl *BusinessStoreImpl) UpdateUser(ctx context.Context, userID int32, name string, newEmail, oldEmail string) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	user, err := impl.querier.UpdateUserData(ctx, &dbgen.UpdateUserDataParams{
		Name:  name,
		Email: newEmail,
		ID:    userID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update user", "userID", userID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Updated user", "userID", userID)

	if user != nil {
		_ = impl.cache.Set(ctx, userCacheKey(user.ID), user, impl.ttl)
	}

	return nil
}

func (impl *BusinessStoreImpl) RetrieveUserAPIKeys(ctx context.Context, userID int32) ([]*dbgen.APIKey, error) {
	cacheKey := userAPIKeysCacheKey(userID)

	if keys, err := fetchCachedMany[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return keys, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	keys, err := impl.querier.GetUserAPIKeys(ctx, Int(userID))
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.Set(ctx, cacheKey, emptyAPIKeys, impl.ttl)
			return emptyAPIKeys, nil
		}
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Retrieved API keys", "count", len(keys))

	if len(keys) > 0 {
		_ = impl.cache.Set(ctx, cacheKey, keys, impl.ttl)
	}

	return keys, err
}

func (impl *BusinessStoreImpl) UpdateAPIKey(ctx context.Context, externalID pgtype.UUID, expiration time.Time, enabled bool) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	key, err := impl.querier.UpdateAPIKey(ctx, &dbgen.UpdateAPIKeyParams{
		ExpiresAt:  Timestampz(expiration),
		Enabled:    Bool(enabled),
		ExternalID: externalID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update API key", "externalID", UUIDToSecret(externalID), common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Updated API key", "externalID", UUIDToSecret(externalID))

	if key != nil {
		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.Set(ctx, cacheKey, key, apiKeyTTL)

		// invalidate keys cache
		_ = impl.cache.Delete(ctx, userAPIKeysCacheKey(key.UserID.Int32))
	}

	return nil
}

func (impl *BusinessStoreImpl) CreateAPIKey(ctx context.Context, userID int32, name string, expiration time.Time, requestsPerSecond float64) (*dbgen.APIKey, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	// current logic is that initial values will be set per plan and adjusted manually in DB if requested by customer
	const minAPIKeyRequestsBurst = 20
	burst := int32(requestsPerSecond * 5)
	if burst < minAPIKeyRequestsBurst {
		burst = minAPIKeyRequestsBurst
	}

	key, err := impl.querier.CreateAPIKey(ctx, &dbgen.CreateAPIKeyParams{
		Name:              name,
		UserID:            Int(userID),
		ExpiresAt:         Timestampz(expiration),
		RequestsPerSecond: requestsPerSecond,
		RequestsBurst:     burst,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create API key", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	if key != nil {
		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.Set(ctx, cacheKey, key, apiKeyTTL)

		// invalidate keys cache
		_ = impl.cache.Delete(ctx, userAPIKeysCacheKey(userID))
	}

	return key, nil
}

func (impl *BusinessStoreImpl) DeleteAPIKey(ctx context.Context, userID, keyID int32) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	key, err := impl.querier.DeleteAPIKey(ctx, &dbgen.DeleteAPIKeyParams{
		ID:     keyID,
		UserID: Int(userID),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			slog.ErrorContext(ctx, "Failed to find API Key", "keyID", keyID, "userID", userID)
			return ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to delete API key", "keyID", keyID, "userID", userID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Deleted API Key", "keyID", keyID, "userID", userID)

	// invalidate keys cache
	if key != nil {
		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.Delete(ctx, cacheKey)

	}

	_ = impl.cache.Delete(ctx, userAPIKeysCacheKey(userID))

	return nil
}

func (impl *BusinessStoreImpl) UpdateUserAPIKeysRateLimits(ctx context.Context, userID int32, requestsPerSecond float64) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.UpdateUserAPIKeysRateLimits(ctx, &dbgen.UpdateUserAPIKeysRateLimitsParams{
		RequestsPerSecond: requestsPerSecond,
		UserID:            Int(userID),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			slog.WarnContext(ctx, "Failed to find user API Keys", "userID", userID)
			return ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to update user API keys rate limit", "userID", userID, "rateLimit", requestsPerSecond,
			common.ErrAttr(err))

		return err
	}

	slog.DebugContext(ctx, "Updated user API keys rate limit", "userID", userID)

	// invalidate keys cache
	_ = impl.cache.Delete(ctx, userAPIKeysCacheKey(userID))

	return nil
}

func (impl *BusinessStoreImpl) RetrieveUsersWithoutSubscription(ctx context.Context, userIDs []int32) ([]*dbgen.User, error) {
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

func (impl *BusinessStoreImpl) RetrieveSubscriptionsByUserIDs(ctx context.Context, userIDs []int32) ([]*dbgen.GetSubscriptionsByUserIDsRow, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	subscriptions, err := impl.querier.GetSubscriptionsByUserIDs(ctx, userIDs)
	if err != nil {
		if err == pgx.ErrNoRows {
			return []*dbgen.GetSubscriptionsByUserIDsRow{}, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve user subscriptions", "userIDs", len(userIDs), common.ErrAttr(err))

		return nil, err
	}

	slog.DebugContext(ctx, "Fetched users subscriptions", "count", len(subscriptions), "userIDs", len(userIDs))

	return subscriptions, err
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

	return err
}

func (impl *BusinessStoreImpl) DeleteDeletedRecords(ctx context.Context, before time.Time) error {
	if impl.querier == nil {
		return ErrMaintenance
	}

	err := impl.querier.DeleteDeletedRecords(ctx, Timestampz(before))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to cleanup deleted records", "before", before, common.ErrAttr(err))
	}

	return err
}

func (impl *BusinessStoreImpl) RetrieveSoftDeletedProperties(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedPropertiesRow, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	properties, err := impl.querier.GetSoftDeletedProperties(ctx, &dbgen.GetSoftDeletedPropertiesParams{
		DeletedAt: Timestampz(before),
		Limit:     int32(limit),
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

func (impl *BusinessStoreImpl) RetrieveSoftDeletedOrganizations(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedOrganizationsRow, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	organizations, err := impl.querier.GetSoftDeletedOrganizations(ctx, &dbgen.GetSoftDeletedOrganizationsParams{
		DeletedAt: Timestampz(before),
		Limit:     int32(limit),
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

func (impl *BusinessStoreImpl) RetrieveSoftDeletedUsers(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedUsersRow, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	users, err := impl.querier.GetSoftDeletedUsers(ctx, &dbgen.GetSoftDeletedUsersParams{
		DeletedAt: Timestampz(before),
		Limit:     int32(limit),
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

func (impl *BusinessStoreImpl) RetrieveNotification(ctx context.Context, id int32) (*dbgen.SystemNotification, error) {
	cacheKey := notificationCacheKey(id)

	if notif, err := fetchCachedOne[dbgen.SystemNotification](ctx, impl.cache, cacheKey); err == nil {
		return notif, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	notification, err := impl.querier.GetNotificationById(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			_ = impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve notification by ID", "notifID", id, common.ErrAttr(err))

		return nil, err
	}

	if notification != nil {
		_ = impl.cache.Set(ctx, cacheKey, notification, impl.ttl)
	}

	return notification, nil
}

func (impl *BusinessStoreImpl) RetrieveUserNotification(ctx context.Context, tnow time.Time, userID int32) (*dbgen.SystemNotification, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	n, err := impl.querier.GetLastActiveNotification(ctx, &dbgen.GetLastActiveNotificationParams{
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
	_ = impl.cache.Set(ctx, cacheKey, n, impl.ttl)

	slog.DebugContext(ctx, "Retrieved system notification", "userID", userID, "notifID", n.ID)

	return n, err
}

func (impl *BusinessStoreImpl) CreateNotification(ctx context.Context, message string, tnow time.Time, duration *time.Duration, userID *int32) (*dbgen.SystemNotification, error) {
	if impl.querier == nil {
		return nil, ErrMaintenance
	}

	arg := &dbgen.CreateNotificationParams{
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

	n, err := impl.querier.CreateNotification(ctx, arg)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create a system notification", common.ErrAttr(err))
		return nil, err
	}

	if n != nil {
		cacheKey := notificationCacheKey(n.ID)
		_ = impl.cache.Set(ctx, cacheKey, n, impl.ttl)
	}

	slog.DebugContext(ctx, "Created system notification", "notifID", n.ID)

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

	count, err := impl.querier.GetUserPropertiesCount(ctx, Int(userID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user properties count", "userID", userID, common.ErrAttr(err))
		return 0, err
	}

	slog.DebugContext(ctx, "Fetched user properties count", "userID", userID, "count", count)

	return count, nil
}

func (s *BusinessStoreImpl) GetCachedPropertyBySitekey(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	if sitekey == TestPropertySitekey {
		return nil, ErrTestProperty
	}

	return s.getCachedPropertyBySitekey(ctx, sitekey)
}

func (s *BusinessStoreImpl) RetrieveUser(ctx context.Context, id int32) (*dbgen.User, error) {
	user, err := s.retrieveUser(ctx, id)
	if err != nil {
		return nil, err
	}

	if user.DeletedAt.Valid {
		slog.WarnContext(ctx, "User is soft-deleted", "userID", id, "deletedAt", user.DeletedAt.Time)
		return user, ErrSoftDeleted
	}

	return user, nil
}

func (s *BusinessStoreImpl) RetrieveUserOrganization(ctx context.Context, userID, orgID int32) (*dbgen.Organization, error) {
	org, level, err := s.retrieveOrganizationWithAccess(ctx, userID, orgID)
	if err != nil {
		return nil, err
	}

	if !level.Valid {
		slog.WarnContext(ctx, "User cannot access this org", "orgID", orgID, "userID", userID)
		return nil, ErrPermissions
	}

	if org.DeletedAt.Valid {
		slog.WarnContext(ctx, "Organization is soft-deleted", "orgID", orgID, "deletedAt", org.DeletedAt.Time)
		return org, ErrSoftDeleted
	}

	return org, nil
}

func (s *BusinessStoreImpl) RetrieveOrgProperty(ctx context.Context, orgID, propID int32) (*dbgen.Property, error) {
	property, err := s.retrieveOrgProperty(ctx, orgID, propID)
	if err != nil {
		return nil, err
	}

	if !property.OrgID.Valid || (property.OrgID.Int32 != orgID) {
		slog.ErrorContext(ctx, "Property org does not match", "propertyOrgID", property.OrgID.Int32, "orgID", orgID)
		return nil, ErrPermissions
	}

	if property.DeletedAt.Valid {
		slog.WarnContext(ctx, "Property is soft-deleted", "propID", propID, "deletedAt", property.DeletedAt.Time)
		return property, ErrSoftDeleted
	}

	return property, nil
}

func (s *BusinessStoreImpl) CreateNewAccount(ctx context.Context, params *dbgen.CreateSubscriptionParams, email, name, orgName string, existingUserID int32) (*dbgen.User, *dbgen.Organization, error) {
	if s.querier == nil {
		return nil, nil, ErrMaintenance
	}

	var subscriptionID *int32

	if params != nil {
		subscription, err := s.createNewSubscription(ctx, params)
		if err != nil {
			return nil, nil, err
		}

		subscriptionID = &subscription.ID

		if existingUser, err := s.FindUserByEmail(ctx, email); err == nil {
			slog.InfoContext(ctx, "User with such email already exists", "userID", existingUser.ID, "subscriptionID", existingUser.SubscriptionID)
			if ((existingUser.ID == existingUserID) || (existingUserID == -1)) && !existingUser.SubscriptionID.Valid {
				if err := s.updateUserSubscription(ctx, existingUser.ID, subscription.ID); err != nil {
					return nil, nil, err
				}

				return existingUser, nil, nil
			} else {
				slog.ErrorContext(ctx, "Cannot update existing user with same email", "existingUserID", existingUser.ID,
					"passthrough", existingUserID, "subscribed", existingUser.SubscriptionID.Valid, "email", email)
				return nil, nil, ErrDuplicateAccount
			}
		}
	}

	user, err := s.createNewUser(ctx, email, name, subscriptionID)
	if err != nil {
		return nil, nil, err
	}

	org, err := s.CreateNewOrganization(ctx, orgName, user.ID)
	if err != nil {
		return nil, nil, err
	}

	return user, org, nil
}
