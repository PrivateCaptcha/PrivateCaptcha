package db

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func fetchCachedOne[T any](ctx context.Context, cache common.Cache, key string) (*T, error) {
	data, err := cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if t, ok := data.(*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

func fetchCachedMany[T any](ctx context.Context, cache common.Cache, key string) ([]*T, error) {
	data, err := cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if t, ok := data.([]*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

type businessStoreImpl struct {
	queries *dbgen.Queries
	cache   common.Cache
}

func (impl *businessStoreImpl) deleteExpiredCache(ctx context.Context) error {
	return impl.queries.DeleteExpiredCache(ctx)
}

func (impl *businessStoreImpl) createNewSubscription(ctx context.Context, params *dbgen.CreateSubscriptionParams) (*dbgen.Subscription, error) {
	subscription, err := impl.queries.CreateSubscription(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create a subscription in DB", common.ErrAttr(err))
		return nil, err
	}

	if subscription != nil {
		cacheKey := subscriptionCacheKey(subscription.ID)
		_ = impl.cache.Set(ctx, cacheKey, subscription)
	}

	return subscription, nil
}

func (impl *businessStoreImpl) createNewUser(ctx context.Context, email, name string, subscriptionID *int32) (*dbgen.User, error) {
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
		cacheKey := emailCacheKey(email)
		_ = impl.cache.Set(ctx, cacheKey, user)
	}

	return user, nil
}

func (impl *businessStoreImpl) createNewOrganization(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
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
		_ = impl.cache.Set(ctx, cacheKey, org)

		// invalidate user orgs in cache as we just created another one
		_ = impl.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))
	}

	return org, nil
}

func (impl *businessStoreImpl) softDeleteUser(ctx context.Context, userID int32, email string) error {
	if err := impl.queries.SoftDeleteUser(ctx, userID); err != nil {
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

	if err := impl.queries.SoftDeleteUserAPIKeys(ctx, Int(userID)); err != nil {
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

	_ = impl.cache.Delete(ctx, emailCacheKey(email))

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

func (impl *businessStoreImpl) retrievePropertiesBySitekey(ctx context.Context, sitekeys []string) ([]*dbgen.Property, error) {
	keys := make([]pgtype.UUID, 0, len(sitekeys))
	keysMap := make(map[string]bool)
	result := make([]*dbgen.Property, 0, len(sitekeys))

	for _, sitekey := range sitekeys {
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

	properties, err := impl.queries.GetPropertiesByExternalID(ctx, keys)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties by sitekeys", common.ErrAttr(err))
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
		}

		return nil, err
	}

	slog.DebugContext(ctx, "Fetched properties from DB by sitekeys", "count", len(properties))

	for _, p := range properties {
		sitekey := UUIDToSiteKey(p.ExternalID)
		cacheKey := PropertyBySitekeyCacheKey(sitekey)
		_ = impl.cache.Set(ctx, cacheKey, p)
		delete(keysMap, sitekey)
	}

	for missingKey := range keysMap {
		_ = impl.cache.SetMissing(ctx, PropertyBySitekeyCacheKey(missingKey))
	}

	result = append(result, properties...)

	return result, nil
}

func (impl *businessStoreImpl) retrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	eid := UUIDFromSecret(secret)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	cacheKey := APIKeyCacheKey(secret)

	if apiKey, err := fetchCachedOne[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return apiKey, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	apiKey, err := impl.queries.GetAPIKeyByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve API Key by external ID", "secret", secret, common.ErrAttr(err))

		return nil, err
	}

	if apiKey != nil {
		_ = impl.cache.Set(ctx, cacheKey, apiKey)

		if apiKey.DeletedAt.Valid {
			return apiKey, ErrSoftDeleted
		}
	}

	return apiKey, nil
}

func (impl *businessStoreImpl) checkPuzzleCached(ctx context.Context, puzzleID string) bool {
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

func (impl *businessStoreImpl) cachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error {
	key := puzzleCacheKey(p.PuzzleIDString())
	diff := p.Expiration.Sub(tnow)

	return impl.queries.CreateCache(ctx, &dbgen.CreateCacheParams{
		Key:     key,
		Value:   markerData[:],
		Column3: diff,
	})
}

func (impl *businessStoreImpl) findUser(ctx context.Context, email string) (*dbgen.User, error) {
	if len(email) == 0 {
		return nil, ErrInvalidInput
	}

	cacheKey := emailCacheKey(email)
	if user, err := fetchCachedOne[dbgen.User](ctx, impl.cache, cacheKey); err == nil {
		return user, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	user, err := impl.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve user by email", "email", email, common.ErrAttr(err))

		return nil, err
	}

	if user != nil {
		_ = impl.cache.Set(ctx, cacheKey, user)
	}

	return user, nil
}

func (impl *businessStoreImpl) retrieveUserOrganizations(ctx context.Context, userID int32) ([]*dbgen.GetUserOrganizationsRow, error) {
	cacheKey := userOrgsCacheKey(userID)

	if orgs, err := fetchCachedMany[dbgen.GetUserOrganizationsRow](ctx, impl.cache, cacheKey); err == nil {
		return orgs, nil
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
		_ = impl.cache.Set(ctx, cacheKey, orgs)
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

	org, err := impl.queries.GetOrganizationByID(ctx, orgID)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve organization by ID", "orgID", orgID, common.ErrAttr(err))

		return nil, err
	}

	if org != nil {
		_ = impl.cache.Set(ctx, cacheKey, org)
	}

	return org, nil
}

func (impl *businessStoreImpl) retrieveProperty(ctx context.Context, propID int32) (*dbgen.Property, error) {
	cacheKey := propertyByIDCacheKey(propID)

	if prop, err := fetchCachedOne[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
		return prop, nil
	}

	property, err := impl.queries.GetPropertyByID(ctx, propID)
	if err != nil {
		if err == pgx.ErrNoRows {
			impl.cache.SetMissing(ctx, cacheKey)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by ID", "propID", propID, common.ErrAttr(err))

		return nil, err
	}

	if property != nil {
		_ = impl.cache.Set(ctx, cacheKey, property)
		sitekey := UUIDToSiteKey(property.ExternalID)
		_ = impl.cache.Set(ctx, PropertyBySitekeyCacheKey(sitekey), property)
	}

	return property, nil
}

func (impl *businessStoreImpl) retrieveSubscription(ctx context.Context, sID int32) (*dbgen.Subscription, error) {
	cacheKey := subscriptionCacheKey(sID)
	if subscription, err := fetchCachedOne[dbgen.Subscription](ctx, impl.cache, cacheKey); err == nil {
		return subscription, nil
	}

	subscription, err := impl.queries.GetSubscriptionByID(ctx, sID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch subscription from DB", "id", sID, common.ErrAttr(err))
		return nil, err
	}

	if subscription != nil {
		_ = impl.cache.Set(ctx, cacheKey, subscription)
	}

	return subscription, nil
}

func (impl *businessStoreImpl) updateSubscription(ctx context.Context, params *dbgen.UpdateSubscriptionParams) error {
	subscription, err := impl.queries.UpdateSubscription(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update subscription in DB", "paddleSubscriptionID", params.PaddleSubscriptionID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Updated subscription in DB", "id", subscription.ID, "status", subscription.Status)

	if subscription != nil {
		cacheKey := subscriptionCacheKey(subscription.ID)
		_ = impl.cache.Set(ctx, cacheKey, subscription)
	}

	return nil
}

func (impl *businessStoreImpl) findOrgProperty(ctx context.Context, name string, orgID int32) (*dbgen.Property, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
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
	property, err := impl.queries.CreateProperty(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property in DB", "name", params.Name, "org", params.OrgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Created new property", "id", property.ID, "name", params.Name, "org", params.OrgID)

	cacheKey := propertyByIDCacheKey(property.ID)
	_ = impl.cache.Set(ctx, cacheKey, property)
	sitekey := UUIDToSiteKey(property.ExternalID)
	_ = impl.cache.Set(ctx, PropertyBySitekeyCacheKey(sitekey), property)
	// invalidate org properties in cache as we just created a new property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(params.OrgID.Int32))

	return property, nil
}

func (impl *businessStoreImpl) updateProperty(ctx context.Context, propID int32, name string, level dbgen.DifficultyLevel, growth dbgen.DifficultyGrowth) (*dbgen.Property, error) {
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
	_ = impl.cache.Set(ctx, cacheKey, property)
	// invalidate org properties in cache as we just created a new property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(property.OrgID.Int32))

	return property, nil
}

func (impl *businessStoreImpl) softDeleteProperty(ctx context.Context, propID int32, orgID int32) error {
	if err := impl.queries.SoftDeleteProperty(ctx, propID); err != nil {
		slog.ErrorContext(ctx, "Failed to mark property as deleted in DB", "propID", propID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted property", "propID", propID)

	// update caches
	_ = impl.cache.SetMissing(ctx, propertyByIDCacheKey(propID))
	// invalidate org properties in cache as we just deleted a property
	_ = impl.cache.Delete(ctx, orgPropertiesCacheKey(orgID))

	return nil
}

func (impl *businessStoreImpl) retrieveOrgProperties(ctx context.Context, orgID int32) ([]*dbgen.Property, error) {
	cacheKey := orgPropertiesCacheKey(orgID)

	if properties, err := fetchCachedMany[dbgen.Property](ctx, impl.cache, cacheKey); err == nil {
		return properties, nil
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
		_ = impl.cache.Set(ctx, cacheKey, properties)
	}

	return properties, err
}

func (impl *businessStoreImpl) updateOrganization(ctx context.Context, orgID int32, name string) (*dbgen.Organization, error) {
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
	_ = impl.cache.Set(ctx, cacheKey, org)
	// invalidate user orgs in cache as we just updated name
	_ = impl.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))

	return org, nil
}

func (impl *businessStoreImpl) softDeleteOrganization(ctx context.Context, orgID int32, userID int32) error {
	if err := impl.queries.SoftDeleteOrganization(ctx, orgID); err != nil {
		slog.ErrorContext(ctx, "Failed to mark organization as deleted in DB", "orgID", orgID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted organization", "orgID", orgID)

	// update caches
	_ = impl.cache.SetMissing(ctx, orgCacheKey(orgID))
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

	users, err := impl.queries.GetOrganizationUsers(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch organization users", "orgID", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched organization users", "orgID", orgID, "count", len(users))

	if len(users) > 0 {
		_ = impl.cache.Set(ctx, cacheKey, users)
	}

	return users, nil
}

func (impl *businessStoreImpl) inviteUserToOrg(ctx context.Context, orgID int32, userID int32) error {
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
	return impl.queries.UpdateUserSubscription(ctx, &dbgen.UpdateUserSubscriptionParams{
		ID:             userID,
		SubscriptionID: Int(subscriptionID),
	})
}

func (impl *businessStoreImpl) updateUser(ctx context.Context, userID int32, name string, newEmail, oldEmail string) error {
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

	// delete old email from cache
	_ = impl.cache.Delete(ctx, emailCacheKey(oldEmail))

	if user != nil {
		_ = impl.cache.Set(ctx, emailCacheKey(newEmail), user)
	}

	return nil
}

func (impl *businessStoreImpl) retrieveUserAPIKeys(ctx context.Context, userID int32) ([]*dbgen.APIKey, error) {
	cacheKey := userAPIKeysCacheKey(userID)

	if keys, err := fetchCachedMany[dbgen.APIKey](ctx, impl.cache, cacheKey); err == nil {
		return keys, nil
	}

	keys, err := impl.queries.GetUserAPIKeys(ctx, Int(userID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Retrieved API keys", "count", len(keys))

	if len(keys) > 0 {
		_ = impl.cache.Set(ctx, cacheKey, keys)
	}

	return keys, err
}

func (impl *businessStoreImpl) updateAPIKey(ctx context.Context, externalID pgtype.UUID, expiration time.Time, enabled bool) error {
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
		_ = impl.cache.Set(ctx, cacheKey, key)

		// invalidate keys cache
		_ = impl.cache.Delete(ctx, userAPIKeysCacheKey(key.UserID.Int32))
	}

	return nil
}

func (impl *businessStoreImpl) createAPIKey(ctx context.Context, userID int32, name string, expiration time.Time) (*dbgen.APIKey, error) {
	key, err := impl.queries.CreateAPIKey(ctx, &dbgen.CreateAPIKeyParams{
		Name:      name,
		UserID:    Int(userID),
		ExpiresAt: Timestampz(expiration),
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create API key", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	if key != nil {
		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.Set(ctx, cacheKey, key)

		// invalidate keys cache
		_ = impl.cache.Delete(ctx, userAPIKeysCacheKey(userID))
	}

	return key, nil
}

func (impl *businessStoreImpl) softDeleteAPIKey(ctx context.Context, userID, keyID int32) error {
	key, err := impl.queries.SoftDeleteAPIKey(ctx, &dbgen.SoftDeleteAPIKeyParams{
		ID:     keyID,
		UserID: Int(userID),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			slog.ErrorContext(ctx, "Failed to find API Key", "keyID", keyID, "userID", userID)
			return ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to mark API Key as deleted in DB", "keyID", keyID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted API Key", "keyID", keyID)

	// invalidate keys cache
	if key != nil {
		secret := UUIDToSecret(key.ExternalID)
		cacheKey := APIKeyCacheKey(secret)
		_ = impl.cache.Delete(ctx, cacheKey)

		_ = impl.cache.Delete(ctx, userAPIKeysCacheKey(userID))
	}

	return nil
}

func (impl *businessStoreImpl) createSupportTicket(ctx context.Context, category dbgen.SupportCategory, message string, userID int32) error {
	ticket, err := impl.queries.CreateSupportTicket(ctx, &dbgen.CreateSupportTicketParams{
		Category: category,
		Message:  Text(message),
		UserID:   Int(userID),
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create support ticket", "userID", userID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Created support ticket in DB", "ticketID", ticket.ID)

	return nil
}
