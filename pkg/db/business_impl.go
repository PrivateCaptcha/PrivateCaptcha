package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	paddlePricesKey = "paddle_prices"
	propertyTTL     = 1 * time.Hour
	apiKeyTTL       = 30 * time.Minute
)

var (
	errUnsupported = errors.New("not supported")
)

func fetchCachedOne[T any](ctx context.Context, cache common.Cache[string, any], key string) (*T, error) {
	data, err := cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if t, ok := data.(*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

func fetchCachedMany[T any](ctx context.Context, cache common.Cache[string, any], key string) ([]*T, error) {
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

type txCache struct {
	set     map[string]*txCacheArg
	del     map[string]struct{}
	missing map[string]time.Duration
}

func NewTxCache() *txCache {
	return &txCache{
		set:     make(map[string]*txCacheArg),
		del:     make(map[string]struct{}),
		missing: make(map[string]time.Duration),
	}
}

func (c *txCache) Get(ctx context.Context, key string) (any, error) { return nil, errUnsupported }
func (c *txCache) SetMissing(ctx context.Context, key string, ttl time.Duration) error {
	c.missing[key] = ttl
	return nil
}
func (c *txCache) Set(ctx context.Context, key string, t any, ttl time.Duration) error {
	c.set[key] = &txCacheArg{item: t, ttl: ttl}
	return nil
}
func (c *txCache) Delete(ctx context.Context, key string) error {
	c.del[key] = struct{}{}
	return nil
}

func (c *txCache) Commit(ctx context.Context, cache common.Cache[string, any]) {
	for key := range c.del {
		cache.Delete(ctx, key)
	}

	for key, value := range c.missing {
		cache.SetMissing(ctx, key, value)
	}

	for key, value := range c.set {
		cache.Set(ctx, key, value.item, value.ttl)
	}
}

type businessStoreImpl struct {
	queries *dbgen.Queries
	cache   common.Cache[string, any]
	ttl     time.Duration
}

func (impl *businessStoreImpl) ping(ctx context.Context) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	v, err := impl.queries.Ping(ctx)
	if err != nil {
		return err
	}
	slog.Log(ctx, common.LevelTrace, "Pinged Postgres", "result", v)
	return nil
}

func (impl *businessStoreImpl) deleteExpiredCache(ctx context.Context) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	return impl.queries.DeleteExpiredCache(ctx)
}

func (impl *businessStoreImpl) createNewSubscription(ctx context.Context, params *dbgen.CreateSubscriptionParams) (*dbgen.Subscription, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	subscription, err := impl.queries.CreateSubscription(ctx, params)
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

func (impl *businessStoreImpl) createNewUser(ctx context.Context, email, name string, subscriptionID *int32) (*dbgen.User, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	params := &dbgen.CreateUserParams{
		Name:  name,
		Email: email,
	}

	if subscriptionID != nil {
		params.SubscriptionID = Int(*subscriptionID)
	}

	user, err := impl.queries.CreateUser(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create user in DB", "email", email, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Created user in DB", "email", email, "id", user.ID)

	if user != nil {
		// we need to update cache as we just set user as missing when checking for it's existence
		cacheKey := userCacheKey(user.ID)
		_ = impl.cache.Set(ctx, cacheKey, user, impl.ttl)
	}

	return user, nil
}

func (impl *businessStoreImpl) createNewOrganization(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	org, err := impl.queries.CreateOrganization(ctx, &dbgen.CreateOrganizationParams{
		Name:   name,
		UserID: Int(userID),
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create organization in DB", "name", name, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Created organization in DB", "name", name, "id", org.ID)

	if org != nil {
		cacheKey := orgCacheKey(org.ID)
		_ = impl.cache.Set(ctx, cacheKey, org, impl.ttl)

		// invalidate user orgs in cache as we just created another one
		_ = impl.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))
	}

	return org, nil
}

func (impl *businessStoreImpl) softDeleteUser(ctx context.Context, userID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	user, err := impl.queries.SoftDeleteUser(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user", "userID", userID, common.ErrAttr(err))
		return err
	} else {
		slog.DebugContext(ctx, "Soft-deleted user", "userID", userID)
	}

	if err := impl.queries.SoftDeleteUserOrganizations(ctx, Int(userID)); err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user organizations", "userID", userID, common.ErrAttr(err))
		return err
	} else {
		slog.DebugContext(ctx, "Soft-deleted user organizations", "userID", userID)
	}

	if err := impl.queries.DeleteUserAPIKeys(ctx, Int(userID)); err != nil {
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

func (impl *businessStoreImpl) getCachedPropertyBySitekey(ctx context.Context, sitekey string) (*dbgen.Property, error) {
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

func (impl *businessStoreImpl) retrievePropertiesBySitekey(ctx context.Context, sitekeys map[string]struct{}) ([]*dbgen.Property, error) {
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

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	properties, err := impl.queries.GetPropertiesByExternalID(ctx, keys)
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

func (impl *businessStoreImpl) getCachedAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	cacheKey := APIKeyCacheKey(secret)

	if apiKey, err := fetchCachedOne[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return apiKey, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	} else {
		return nil, err
	}
}

func (impl *businessStoreImpl) retrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	cacheKey := APIKeyCacheKey(secret)

	if apiKey, err := fetchCachedOne[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return apiKey, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	eid := UUIDFromSecret(secret)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	apiKey, err := impl.queries.GetAPIKeyByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
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

func (impl *businessStoreImpl) checkPuzzleCached(ctx context.Context, puzzleID string) bool {
	if impl.queries == nil {
		return false
	}

	key := puzzleCacheKey(puzzleID)

	data, err := impl.queries.GetCachedByKey(ctx, key)
	if err == pgx.ErrNoRows {
		return false
	} else if err != nil {
		slog.ErrorContext(ctx, "Failed to check if puzzle is cached", common.ErrAttr(err))
		return false
	}

	return bytes.Equal(data[:], markerData[:])
}

func (impl *businessStoreImpl) cachePaddlePrices(ctx context.Context, prices map[string]int) error {
	if len(prices) == 0 {
		return ErrInvalidInput
	}

	if impl.queries == nil {
		return ErrMaintenance
	}

	data, err := json.Marshal(prices)
	if err != nil {
		return err
	}

	return impl.queries.CreateCache(ctx, &dbgen.CreateCacheParams{
		Key:     paddlePricesKey,
		Value:   data,
		Column3: 24 * time.Hour,
	})
}

func (impl *businessStoreImpl) retrievePaddlePrices(ctx context.Context) (map[string]int, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	data, err := impl.queries.GetCachedByKey(ctx, paddlePricesKey)
	if err == pgx.ErrNoRows {
		return nil, ErrCacheMiss
	} else if err != nil {
		slog.ErrorContext(ctx, "Failed to read Paddle prices", common.ErrAttr(err))
		return nil, err
	}

	var prices map[string]int
	err = json.Unmarshal(data, &prices)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal Paddle prices", common.ErrAttr(err))
		return nil, err
	}

	return prices, nil
}

func (impl *businessStoreImpl) cachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	puzzleID := p.PuzzleIDString()
	key := puzzleCacheKey(puzzleID)
	diff := p.Expiration.Sub(tnow)

	err := impl.queries.CreateCache(ctx, &dbgen.CreateCacheParams{
		Key:     key,
		Value:   markerData[:],
		Column3: diff,
	})

	slog.Log(ctx, common.LevelTrace, "Cached puzzle", "puzzleID", puzzleID, common.ErrAttr(err))

	return err
}

func (impl *businessStoreImpl) retrieveUser(ctx context.Context, userID int32) (*dbgen.User, error) {
	cacheKey := userCacheKey(userID)
	if user, err := fetchCachedOne[dbgen.User](ctx, impl.cache, cacheKey); err == nil {
		return user, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	user, err := impl.queries.GetUserByID(ctx, userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
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

func (impl *businessStoreImpl) findUserByEmail(ctx context.Context, email string) (*dbgen.User, error) {
	if len(email) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	user, err := impl.queries.GetUserByEmail(ctx, email)
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

func (impl *businessStoreImpl) findUserBySubscriptionID(ctx context.Context, subscriptionID int32) (*dbgen.User, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	user, err := impl.queries.GetUserBySubscriptionID(ctx, Int(subscriptionID))
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

func (impl *businessStoreImpl) retrieveUserOrganizations(ctx context.Context, userID int32) ([]*dbgen.GetUserOrganizationsRow, error) {
	cacheKey := userOrgsCacheKey(userID)

	if orgs, err := fetchCachedMany[dbgen.GetUserOrganizationsRow](ctx, impl.cache, cacheKey); err == nil {
		return orgs, nil
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	orgs, err := impl.queries.GetUserOrganizations(ctx, Int(userID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
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

func (impl *businessStoreImpl) retrieveOrganization(ctx context.Context, orgID int32) (*dbgen.Organization, error) {
	cacheKey := orgCacheKey(orgID)

	if org, err := fetchCachedOne[dbgen.Organization](ctx, impl.cache, cacheKey); err == nil {
		return org, nil
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	org, err := impl.queries.GetOrganizationByID(ctx, orgID)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve organization by ID", "orgID", orgID, common.ErrAttr(err))

		return nil, err
	}

	if org != nil {
		_ = impl.cache.Set(ctx, cacheKey, org, impl.ttl)
	}

	return org, nil
}

func (impl *businessStoreImpl) retrieveProperty(ctx context.Context, propID int32) (*dbgen.Property, error) {
	cacheKey := propertyByIDCacheKey(propID)

	if prop, err := fetchCachedOne[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
		return prop, nil
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.queries.GetPropertyByID(ctx, propID)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by ID", "propID", propID, common.ErrAttr(err))

		return nil, err
	}

	if property != nil {
		_ = impl.cache.Set(ctx, cacheKey, property, impl.ttl)
		sitekey := UUIDToSiteKey(property.ExternalID)
		_ = impl.cache.Set(ctx, PropertyBySitekeyCacheKey(sitekey), property, propertyTTL)
	}

	return property, nil
}

func (impl *businessStoreImpl) retrieveSubscription(ctx context.Context, sID int32) (*dbgen.Subscription, error) {
	cacheKey := subscriptionCacheKey(sID)
	if subscription, err := fetchCachedOne[dbgen.Subscription](ctx, impl.cache, cacheKey); err == nil {
		return subscription, nil
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	subscription, err := impl.queries.GetSubscriptionByID(ctx, sID)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
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

func (impl *businessStoreImpl) updateSubscription(ctx context.Context, params *dbgen.UpdateSubscriptionParams) (*dbgen.Subscription, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	subscription, err := impl.queries.UpdateSubscription(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update subscription in DB", "paddleSubscriptionID", params.PaddleSubscriptionID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Updated subscription in DB", "id", subscription.ID, "status", subscription.Status)

	if subscription != nil {
		cacheKey := subscriptionCacheKey(subscription.ID)
		_ = impl.cache.Set(ctx, cacheKey, subscription, impl.ttl)
	}

	return subscription, nil
}

func (impl *businessStoreImpl) findOrgProperty(ctx context.Context, name string, orgID int32) (*dbgen.Property, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.queries.GetOrgPropertyByName(ctx, &dbgen.GetOrgPropertyByNameParams{
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

func (impl *businessStoreImpl) findOrg(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	org, err := impl.queries.FindUserOrgByName(ctx, &dbgen.FindUserOrgByNameParams{
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

func (impl *businessStoreImpl) createNewProperty(ctx context.Context, params *dbgen.CreatePropertyParams) (*dbgen.Property, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.queries.CreateProperty(ctx, params)
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

func (impl *businessStoreImpl) updateProperty(ctx context.Context, propID int32, name string, level dbgen.DifficultyLevel, growth dbgen.DifficultyGrowth) (*dbgen.Property, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	property, err := impl.queries.UpdateProperty(ctx, &dbgen.UpdatePropertyParams{
		Name:   name,
		Level:  level,
		Growth: growth,
		ID:     propID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update property in DB", "name", name, "propID", propID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Updated property", "name", name, "propID", propID)

	cacheKey := propertyByIDCacheKey(property.ID)
	_ = impl.cache.Set(ctx, cacheKey, property, impl.ttl)
	// invalidate org properties in cache as we just created a new property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(property.OrgID.Int32))

	return property, nil
}

func (impl *businessStoreImpl) softDeleteProperty(ctx context.Context, propID int32, orgID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	property, err := impl.queries.SoftDeleteProperty(ctx, propID)
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

func (impl *businessStoreImpl) retrieveOrgProperties(ctx context.Context, orgID int32) ([]*dbgen.Property, error) {
	cacheKey := orgPropertiesCacheKey(orgID)

	if properties, err := fetchCachedMany[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
		return properties, nil
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	properties, err := impl.queries.GetOrgProperties(ctx, Int(orgID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return []*dbgen.Property{}, nil
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

func (impl *businessStoreImpl) updateOrganization(ctx context.Context, orgID int32, name string) (*dbgen.Organization, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	org, err := impl.queries.UpdateOrganization(ctx, &dbgen.UpdateOrganizationParams{
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

func (impl *businessStoreImpl) softDeleteOrganization(ctx context.Context, orgID int32, userID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	if err := impl.queries.SoftDeleteOrganization(ctx, orgID); err != nil {
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
func (impl *businessStoreImpl) retrieveOrganizationUsers(ctx context.Context, orgID int32) ([]*dbgen.GetOrganizationUsersRow, error) {
	cacheKey := orgUsersCacheKey(orgID)

	if users, err := fetchCachedMany[dbgen.GetOrganizationUsersRow](ctx, impl.cache, cacheKey); err == nil {
		return users, nil
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	users, err := impl.queries.GetOrganizationUsers(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch organization users", "orgID", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched organization users", "orgID", orgID, "count", len(users))

	if len(users) > 0 {
		_ = impl.cache.Set(ctx, cacheKey, users, impl.ttl)
	}

	return users, nil
}

func (impl *businessStoreImpl) inviteUserToOrg(ctx context.Context, orgID int32, userID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	_, err := impl.queries.InviteUserToOrg(ctx, &dbgen.InviteUserToOrgParams{
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

func (impl *businessStoreImpl) joinOrg(ctx context.Context, orgID int32, userID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	err := impl.queries.UpdateOrgMembershipLevel(ctx, &dbgen.UpdateOrgMembershipLevelParams{
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

func (impl *businessStoreImpl) leaveOrg(ctx context.Context, orgID int32, userID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	err := impl.queries.UpdateOrgMembershipLevel(ctx, &dbgen.UpdateOrgMembershipLevelParams{
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

func (impl *businessStoreImpl) removeUserFromOrg(ctx context.Context, orgID int32, userID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	err := impl.queries.RemoveUserFromOrg(ctx, &dbgen.RemoveUserFromOrgParams{
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

func (impl *businessStoreImpl) updateUserSubscription(ctx context.Context, userID, subscriptionID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	user, err := impl.queries.UpdateUserSubscription(ctx, &dbgen.UpdateUserSubscriptionParams{
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

func (impl *businessStoreImpl) updateUser(ctx context.Context, userID int32, name string, newEmail, oldEmail string) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	user, err := impl.queries.UpdateUserData(ctx, &dbgen.UpdateUserDataParams{
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

func (impl *businessStoreImpl) retrieveUserAPIKeys(ctx context.Context, userID int32) ([]*dbgen.APIKey, error) {
	cacheKey := userAPIKeysCacheKey(userID)

	if keys, err := fetchCachedMany[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return keys, nil
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	keys, err := impl.queries.GetUserAPIKeys(ctx, Int(userID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Retrieved API keys", "count", len(keys))

	if len(keys) > 0 {
		_ = impl.cache.Set(ctx, cacheKey, keys, impl.ttl)
	}

	return keys, err
}

func (impl *businessStoreImpl) updateAPIKey(ctx context.Context, externalID pgtype.UUID, expiration time.Time, enabled bool) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	key, err := impl.queries.UpdateAPIKey(ctx, &dbgen.UpdateAPIKeyParams{
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

func (impl *businessStoreImpl) createAPIKey(ctx context.Context, userID int32, name string, expiration time.Time, requestsPerSecond float64) (*dbgen.APIKey, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	// current logic is that initial values will be set per plan and adjusted manually in DB if requested by customer
	const apiKeyRequestsBurst = 10
	burst := int32(requestsPerSecond * 2)
	if burst < apiKeyRequestsBurst {
		burst = apiKeyRequestsBurst
	}

	key, err := impl.queries.CreateAPIKey(ctx, &dbgen.CreateAPIKeyParams{
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

func (impl *businessStoreImpl) deleteAPIKey(ctx context.Context, userID, keyID int32) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	key, err := impl.queries.DeleteAPIKey(ctx, &dbgen.DeleteAPIKeyParams{
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

func (impl *businessStoreImpl) updateUserAPIKeysRateLimits(ctx context.Context, userID int32, requestsPerSecond float64) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	err := impl.queries.UpdateUserAPIKeysRateLimits(ctx, &dbgen.UpdateUserAPIKeysRateLimitsParams{
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

func (impl *businessStoreImpl) createSupportTicket(ctx context.Context, category dbgen.SupportCategory, message string, userID int32) (*dbgen.Support, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	ticket, err := impl.queries.CreateSupportTicket(ctx, &dbgen.CreateSupportTicketParams{
		Category: category,
		Message:  Text(message),
		UserID:   Int(userID),
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create support ticket", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Created support ticket in DB", "ticketID", ticket.ID)

	return ticket, nil
}

func (impl *businessStoreImpl) retrieveUsersWithoutSubscription(ctx context.Context, userIDs []int32) ([]*dbgen.User, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	users, err := impl.queries.GetUsersWithoutSubscription(ctx, userIDs)
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

func (impl *businessStoreImpl) retrieveSubscriptionsByUserIDs(ctx context.Context, userIDs []int32) ([]*dbgen.GetSubscriptionsByUserIDsRow, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	subscriptions, err := impl.queries.GetSubscriptionsByUserIDs(ctx, userIDs)
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

// NOTE: This function has a side-effect that updates PaddleProductID field in the violations array, if found valid
func (impl *businessStoreImpl) addUsageLimitsViolations(ctx context.Context, violations []*common.UserTimeCount) error {
	if len(violations) == 0 {
		return nil
	}

	if impl.queries == nil {
		return ErrMaintenance
	}

	slog.DebugContext(ctx, "About to insert usage limit violations", "count", len(violations))

	userIDs := make([]int32, 0, len(violations))
	for _, v := range violations {
		userIDs = append(userIDs, int32(v.UserID))
	}

	subscriptions, err := impl.retrieveSubscriptionsByUserIDs(ctx, userIDs)
	if err != nil {
		return err
	}
	if len(subscriptions) == 0 {
		slog.ErrorContext(ctx, "Fetched no subscriptions by userIDs", "userIDs", len(userIDs))
		return ErrRecordNotFound
	}

	userIDs = nil

	userProducts := make(map[int32]string)
	for _, s := range subscriptions {
		userProducts[s.UserID] = s.Subscription.PaddleProductID
	}

	params := &dbgen.AddUsageLimitViolationsParams{
		UserIds:  make([]int32, 0, len(violations)),
		Products: make([]string, 0, len(violations)),
		Limits:   make([]int64, 0, len(violations)),
		Counts:   make([]int64, 0, len(violations)),
		Dates:    make([]pgtype.Date, 0, len(violations)),
	}

	for _, v := range violations {
		product, ok := userProducts[int32(v.UserID)]
		if !ok {
			slog.WarnContext(ctx, "Did not find user subscription product", "userID", v.UserID)
			continue
		}

		// This is an important side-effect of this function
		v.PaddleProductID = product

		params.UserIds = append(params.UserIds, int32(v.UserID))
		params.Products = append(params.Products, product)
		params.Limits = append(params.Limits, int64(v.Limit))
		params.Counts = append(params.Counts, int64(v.Count))
		params.Dates = append(params.Dates, Date(v.Timestamp))
	}

	userProducts = map[int32]string{}

	err = impl.queries.AddUsageLimitViolations(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to add usage limits violations", "count", len(violations), common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Added usage limits violations", "count", len(violations))

	return nil
}

func (impl *businessStoreImpl) retrieveUsersWithConsecutiveViolations(ctx context.Context) ([]*dbgen.GetUsersWithConsecutiveViolationsRow, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	rows, err := impl.queries.GetUsersWithConsecutiveViolations(ctx)
	if err != nil {
		if err == pgx.ErrNoRows {
			return []*dbgen.GetUsersWithConsecutiveViolationsRow{}, nil
		}

		slog.ErrorContext(ctx, "Failed to query users with consecutive limits violations", common.ErrAttr(err))
		return nil, err
	}

	return rows, nil
}

func (impl *businessStoreImpl) retrieveUsersWithLargeViolations(ctx context.Context, from time.Time, rate float64) ([]*dbgen.GetUsersWithLargeViolationsRow, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	rows, err := impl.queries.GetUsersWithLargeViolations(ctx, &dbgen.GetUsersWithLargeViolationsParams{
		Column1: rate,
		Column2: Date(from),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return []*dbgen.GetUsersWithLargeViolationsRow{}, nil
		}

		slog.ErrorContext(ctx, "Failed to query users with large limits violations", common.ErrAttr(err))
		return nil, err
	}

	return rows, nil
}

func (impl *businessStoreImpl) acquireLock(ctx context.Context, name string, data []byte, expiration time.Time) (*dbgen.Lock, error) {
	if (len(name) == 0) || expiration.IsZero() {
		return nil, ErrInvalidInput
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	lock, err := impl.queries.InsertLock(ctx, &dbgen.InsertLockParams{
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

func (impl *businessStoreImpl) releaseLock(ctx context.Context, name string) error {
	if impl.queries == nil {
		return ErrMaintenance
	}
	err := impl.queries.DeleteLock(ctx, name)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to release a lock", "name", name, common.ErrAttr(err))
	}

	return err
}

func (impl *businessStoreImpl) deleteDeletedRecords(ctx context.Context, before time.Time) error {
	if impl.queries == nil {
		return ErrMaintenance
	}

	err := impl.queries.DeleteDeletedRecords(ctx, Timestampz(before))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to cleanup deleted records", "before", before, common.ErrAttr(err))
	}

	return err
}

func (impl *businessStoreImpl) retrieveSoftDeletedProperties(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedPropertiesRow, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	properties, err := impl.queries.GetSoftDeletedProperties(ctx, &dbgen.GetSoftDeletedPropertiesParams{
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

func (impl *businessStoreImpl) deleteProperties(ctx context.Context, ids []int32) error {
	if len(ids) == 0 {
		slog.WarnContext(ctx, "No properties to delete")
		return nil
	}

	if impl.queries == nil {
		return ErrMaintenance
	}

	err := impl.queries.DeleteProperties(ctx, ids)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete properties", "count", len(ids), common.ErrAttr(err))
	}

	return err
}

func (impl *businessStoreImpl) retrieveSoftDeletedOrganizations(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedOrganizationsRow, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	organizations, err := impl.queries.GetSoftDeletedOrganizations(ctx, &dbgen.GetSoftDeletedOrganizationsParams{
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

func (impl *businessStoreImpl) deleteOrganizations(ctx context.Context, ids []int32) error {
	if len(ids) == 0 {
		slog.WarnContext(ctx, "No organizations to delete")
		return nil
	}

	if impl.queries == nil {
		return ErrMaintenance
	}

	err := impl.queries.DeleteOrganizations(ctx, ids)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete organizations", "count", len(ids), common.ErrAttr(err))
	}

	return err
}

func (impl *businessStoreImpl) retrieveSoftDeletedUsers(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedUsersRow, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	users, err := impl.queries.GetSoftDeletedUsers(ctx, &dbgen.GetSoftDeletedUsersParams{
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

func (impl *businessStoreImpl) deleteUsers(ctx context.Context, ids []int32) error {
	if len(ids) == 0 {
		slog.WarnContext(ctx, "No users to delete")
		return nil
	}

	if impl.queries == nil {
		return ErrMaintenance
	}

	err := impl.queries.DeleteUsers(ctx, ids)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete users", "count", len(ids), common.ErrAttr(err))
	}

	return err
}

func (impl *businessStoreImpl) retrieveNotification(ctx context.Context, id int32) (*dbgen.SystemNotification, error) {
	cacheKey := notificationCacheKey(id)

	if notif, err := fetchCachedOne[dbgen.SystemNotification](ctx, impl.cache, cacheKey); err == nil {
		return notif, nil
	}

	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	notification, err := impl.queries.GetNotificationById(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey, impl.ttl)
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

func (impl *businessStoreImpl) retrieveUserNotification(ctx context.Context, tnow time.Time, userID int32) (*dbgen.SystemNotification, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	n, err := impl.queries.GetLastActiveNotification(ctx, &dbgen.GetLastActiveNotificationParams{
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

func (impl *businessStoreImpl) createNotification(ctx context.Context, message string, tnow time.Time, duration *time.Duration, userID *int32) (*dbgen.SystemNotification, error) {
	if impl.queries == nil {
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

	n, err := impl.queries.CreateNotification(ctx, arg)

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

func (impl *businessStoreImpl) retrieveProperties(ctx context.Context, limit int) ([]*dbgen.Property, error) {
	if impl.queries == nil {
		return nil, ErrMaintenance
	}

	properties, err := impl.queries.GetProperties(ctx, int32(limit))

	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties", common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched properties", "count", len(properties))

	return properties, nil
}
