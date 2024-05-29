package db

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrRecordNotFound   = errors.New("record not found")
	ErrSoftDeleted      = errors.New("record is marked as deleted")
	errInvalidCacheType = errors.New("cache record type does not match")
	markerData          = []byte{'{', '}'}
)

const (
	// TODO: Adjust caching durations mindfully
	negativeCacheDuration = 1 * time.Minute
	propertyCacheDuration = 1 * time.Minute
	apiKeyCacheDuration   = 1 * time.Minute
	userCacheDuration     = 1 * time.Minute
	orgCacheDuration      = 1 * time.Minute
	puzzleCacheDuration   = 1 * time.Minute
)

type BusinessStore struct {
	db         *dbgen.Queries
	cache      common.Cache
	cancelFunc context.CancelFunc
}

type puzzleCacheMarker struct {
	Data [4]byte
}

func emailCacheKey(email string) string               { return "email/" + email }
func APIKeyCacheKey(str string) string                { return "apikey/" + str }
func puzzleCacheKey(str string) string                { return "puzzle/" + str }
func orgCacheKey(orgID int32) string                  { return "org/" + strconv.Itoa(int(orgID)) }
func orgPropertiesCacheKey(orgID int32) string        { return "orgprops/" + strconv.Itoa(int(orgID)) }
func propertyByIDCacheKey(propID int32) string        { return "prop/" + strconv.Itoa(int(propID)) }
func PropertyBySitekeyCacheKey(sitekey string) string { return "propeid/" + sitekey }
func userOrgsCacheKey(userID int32) string            { return "userorgs/" + strconv.Itoa(int(userID)) }
func orgUsersCacheKey(orgID int32) string             { return "orgusers/" + strconv.Itoa(int(orgID)) }
func userAPIKeysCacheKey(userID int32) string         { return "userapikeys/" + strconv.Itoa(int(userID)) }

func NewBusiness(queries *dbgen.Queries, cache common.Cache, cleanupInterval time.Duration) *BusinessStore {
	s := &BusinessStore{
		db:    queries,
		cache: cache,
	}

	var ctx context.Context
	ctx, s.cancelFunc = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "cleanup_db_cache"))
	go s.cleanupCache(ctx, cleanupInterval)

	return s
}

func (s *BusinessStore) Shutdown() {
	slog.Debug("Shutting down cache cleanup")
	s.cancelFunc()
}

func (s *BusinessStore) cleanupCache(ctx context.Context, interval time.Duration) {
	slog.Debug("Store cache cleanup started")
	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
		case <-time.After(interval):
			err := s.db.DeleteExpiredCache(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to delete expired items", common.ErrAttr(err))
				continue
			}
		}
	}
	slog.Debug("Store cache cleanup finished")
}

func fetchCachedOne[T any](ctx context.Context, cache common.Cache, key string, expiration time.Duration) (*T, error) {
	data, err := cache.GetAndExpireItem(ctx, key, expiration)
	if err != nil {
		return nil, err
	}

	if t, ok := data.(*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

func fetchCachedMany[T any](ctx context.Context, cache common.Cache, key string, expiration time.Duration) ([]*T, error) {
	data, err := cache.GetAndExpireItem(ctx, key, expiration)
	if err != nil {
		return nil, err
	}

	if t, ok := data.([]*T); ok {
		return t, nil
	}

	return nil, errInvalidCacheType
}

func (s *BusinessStore) GetCachedPropertyBySitekey(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	eid := UUIDFromSiteKey(sitekey)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	cacheKey := PropertyBySitekeyCacheKey(sitekey)

	if property, err := fetchCachedOne[dbgen.Property](ctx, s.cache, cacheKey, propertyCacheDuration); err == nil {
		return property, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	} else {
		return nil, err
	}
}

func (s *BusinessStore) RetrievePropertiesBySitekey(ctx context.Context, sitekeys []string) ([]*dbgen.Property, error) {
	keys := make([]pgtype.UUID, 0, len(sitekeys))
	keysMap := make(map[string]bool)
	result := make([]*dbgen.Property, 0, len(sitekeys))

	for _, sitekey := range sitekeys {
		eid := UUIDFromSiteKey(sitekey)
		if !eid.Valid {
			continue
		}

		cacheKey := PropertyBySitekeyCacheKey(sitekey)
		if property, err := fetchCachedOne[dbgen.Property](ctx, s.cache, cacheKey, propertyCacheDuration); err == nil {
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

	properties, err := s.db.GetPropertiesByExternalID(ctx, keys)
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
		_ = s.cache.SetItem(ctx, cacheKey, p, propertyCacheDuration)
		delete(keysMap, sitekey)
	}

	for missingKey := range keysMap {
		_ = s.cache.SetMissing(ctx, PropertyBySitekeyCacheKey(missingKey), propertyCacheDuration)
	}

	result = append(result, properties...)

	return result, nil
}

// Fetches API keyfrom DB, backed by cache
func (s *BusinessStore) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	eid := UUIDFromSecret(secret)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	cacheKey := APIKeyCacheKey(secret)

	if apiKey, err := fetchCachedOne[dbgen.APIKey](ctx, s.cache, cacheKey, apiKeyCacheDuration); err == nil {
		return apiKey, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	apiKey, err := s.db.GetAPIKeyByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.cache.SetMissing(ctx, cacheKey, negativeCacheDuration)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve API Key by external ID", "secret", secret, common.ErrAttr(err))

		return nil, err
	}

	if apiKey != nil {
		_ = s.cache.SetItem(ctx, cacheKey, apiKey, apiKeyCacheDuration)

		if apiKey.DeletedAt.Valid {
			return apiKey, ErrSoftDeleted
		}
	}

	return apiKey, nil
}

func (s *BusinessStore) CheckPuzzleCached(ctx context.Context, p *puzzle.Puzzle) bool {
	key := puzzleCacheKey(p.PuzzleIDString())

	data, err := s.db.GetCachedByKey(ctx, key)
	if err == pgx.ErrNoRows {
		return false
	} else if err != nil {
		slog.ErrorContext(ctx, "Failed to check if puzzle is cached", common.ErrAttr(err))
		return false
	}

	return bytes.Equal(data[:], markerData[:])
}

func (s *BusinessStore) CachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error {
	// this check should have been done before in the pipeline. Here the check only to safeguard storing in Redis
	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Skipping caching expired puzzle", "now", tnow, "expiration", p.Expiration)
		return nil
	}

	key := puzzleCacheKey(p.PuzzleIDString())
	diff := p.Expiration.Sub(tnow)

	return s.db.CreateCache(ctx, &dbgen.CreateCacheParams{
		Key:     key,
		Value:   markerData[:],
		Column3: diff,
	})
}

func (s *BusinessStore) FindUser(ctx context.Context, email string) (*dbgen.User, error) {
	if len(email) == 0 {
		return nil, ErrInvalidInput
	}

	cacheKey := emailCacheKey(email)
	if user, err := fetchCachedOne[dbgen.User](ctx, s.cache, cacheKey, userCacheDuration); err == nil {
		return user, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	user, err := s.db.GetUserByEmail(ctx, email)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.cache.SetMissing(ctx, cacheKey, negativeCacheDuration)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve user by email", "email", email, common.ErrAttr(err))

		return nil, err
	}

	if user != nil {
		_ = s.cache.SetItem(ctx, cacheKey, user, userCacheDuration)
	}

	return user, nil
}

func (s *BusinessStore) RetrieveUserOrganizations(ctx context.Context, userID int32) ([]*dbgen.GetUserOrganizationsRow, error) {
	cacheKey := userOrgsCacheKey(userID)

	if orgs, err := fetchCachedMany[dbgen.GetUserOrganizationsRow](ctx, s.cache, cacheKey, userCacheDuration); err == nil {
		return orgs, nil
	}

	orgs, err := s.db.GetUserOrganizations(ctx, Int(userID))
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
		_ = s.cache.SetItem(ctx, cacheKey, orgs, userCacheDuration)
	}

	slog.DebugContext(ctx, "Retrieved user organizations", "count", len(orgs))

	// TODO: Also sort by orgs that have any properties in them
	return orgs, nil
}

func (s *BusinessStore) RetrieveOrganization(ctx context.Context, orgID int32) (*dbgen.Organization, error) {
	cacheKey := orgCacheKey(orgID)

	if org, err := fetchCachedOne[dbgen.Organization](ctx, s.cache, cacheKey, orgCacheDuration); err == nil {
		return org, nil
	}

	org, err := s.db.GetOrganizationByID(ctx, orgID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.cache.SetMissing(ctx, cacheKey, negativeCacheDuration)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve organization by ID", "orgID", orgID, common.ErrAttr(err))

		return nil, err
	}

	if org != nil {
		_ = s.cache.SetItem(ctx, cacheKey, org, orgCacheDuration)
	}

	return org, nil
}

func (s *BusinessStore) RetrieveProperty(ctx context.Context, propID int32) (*dbgen.Property, error) {
	cacheKey := propertyByIDCacheKey(propID)

	if prop, err := fetchCachedOne[dbgen.Property](ctx, s.cache, cacheKey, propertyCacheDuration); err == nil {
		return prop, nil
	}

	property, err := s.db.GetPropertyByID(ctx, propID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.cache.SetMissing(ctx, cacheKey, negativeCacheDuration)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by ID", "propID", propID, common.ErrAttr(err))

		return nil, err
	}

	if property != nil {
		_ = s.cache.SetItem(ctx, cacheKey, property, propertyCacheDuration)
		sitekey := UUIDToSiteKey(property.ExternalID)
		_ = s.cache.SetItem(ctx, PropertyBySitekeyCacheKey(sitekey), property, propertyCacheDuration)
	}

	return property, nil
}

func (s *BusinessStore) CreateNewOrganization(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	org, err := s.db.CreateOrganization(ctx, &dbgen.CreateOrganizationParams{
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
		_ = s.cache.SetItem(ctx, cacheKey, org, orgCacheDuration)

		// invalidate user orgs in cache as we just created another one
		_ = s.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))
	}

	return org, nil
}

func (s *BusinessStore) CreateNewAccount(ctx context.Context, email, name, orgName string) (*dbgen.Organization, error) {
	user, err := s.db.CreateUser(ctx, &dbgen.CreateUserParams{
		Name:  name,
		Email: email,
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create user in DB", "email", email, common.ErrAttr(err))
		return nil, err
	}

	if user != nil {
		// we need to update cache as we just set user as missing when checking for it's existence
		cacheKey := emailCacheKey(email)
		_ = s.cache.SetItem(ctx, cacheKey, user, userCacheDuration)
	}

	slog.DebugContext(ctx, "Created user in DB", "email", email, "id", user.ID)

	return s.CreateNewOrganization(ctx, orgName, user.ID)
}

func (s *BusinessStore) FindOrgProperty(ctx context.Context, name string, orgID int32) (*dbgen.Property, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	property, err := s.db.GetOrgPropertyByName(ctx, &dbgen.GetOrgPropertyByNameParams{
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

func (s *BusinessStore) FindOrg(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	if len(name) == 0 {
		return nil, ErrInvalidInput
	}

	org, err := s.db.FindUserOrgByName(ctx, &dbgen.FindUserOrgByNameParams{
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

func (s *BusinessStore) CreateNewProperty(ctx context.Context, params *dbgen.CreatePropertyParams) (*dbgen.Property, error) {
	property, err := s.db.CreateProperty(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property in DB", "name", params.Name, "org", params.OrgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Created new property", "id", property.ID, "name", params.Name, "org", params.OrgID)

	cacheKey := propertyByIDCacheKey(property.ID)
	_ = s.cache.SetItem(ctx, cacheKey, property, propertyCacheDuration)
	sitekey := UUIDToSiteKey(property.ExternalID)
	_ = s.cache.SetItem(ctx, PropertyBySitekeyCacheKey(sitekey), property, propertyCacheDuration)
	// invalidate org properties in cache as we just created a new property
	_ = s.cache.Delete(ctx, orgPropertiesCacheKey(params.OrgID.Int32))

	return property, nil
}

func (s *BusinessStore) UpdateProperty(ctx context.Context, propID int32, name string, level dbgen.DifficultyLevel, growth dbgen.DifficultyGrowth) (*dbgen.Property, error) {
	property, err := s.db.UpdateProperty(ctx, &dbgen.UpdatePropertyParams{
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
	_ = s.cache.SetItem(ctx, cacheKey, property, propertyCacheDuration)
	// invalidate org properties in cache as we just created a new property
	_ = s.cache.Delete(ctx, orgPropertiesCacheKey(property.OrgID.Int32))

	return property, nil
}

func (s *BusinessStore) SoftDeleteProperty(ctx context.Context, propID int32, orgID int32) error {
	if err := s.db.SoftDeleteProperty(ctx, propID); err != nil {
		slog.ErrorContext(ctx, "Failed to mark property as deleted in DB", "propID", propID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted property", "propID", propID)

	// update caches
	_ = s.cache.SetMissing(ctx, propertyByIDCacheKey(propID), negativeCacheDuration)
	// invalidate org properties in cache as we just deleted a property
	_ = s.cache.Delete(ctx, orgPropertiesCacheKey(orgID))

	return nil
}

func (s *BusinessStore) RetrieveOrgProperties(ctx context.Context, orgID int32) ([]*dbgen.Property, error) {
	cacheKey := orgPropertiesCacheKey(orgID)

	if properties, err := fetchCachedMany[dbgen.Property](ctx, s.cache, cacheKey, propertyCacheDuration); err == nil {
		return properties, nil
	}

	properties, err := s.db.GetOrgProperties(ctx, Int(orgID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return []*dbgen.Property{}, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve org properties", "org", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.Log(ctx, common.LevelTrace, "Retrieved properties", "count", len(properties))
	if len(properties) > 0 {
		_ = s.cache.SetItem(ctx, cacheKey, properties, propertyCacheDuration)
	}

	return properties, err
}

func (s *BusinessStore) UpdateOrganization(ctx context.Context, orgID int32, name string) (*dbgen.Organization, error) {
	org, err := s.db.UpdateOrganization(ctx, &dbgen.UpdateOrganizationParams{
		Name: name,
		ID:   orgID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update org in DB", "name", name, "orgID", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Updated organization", "name", name, "orgID", orgID)

	cacheKey := orgCacheKey(org.ID)
	_ = s.cache.SetItem(ctx, cacheKey, org, orgCacheDuration)
	// invalidate user orgs in cache as we just updated name
	_ = s.cache.Delete(ctx, userOrgsCacheKey(org.UserID.Int32))

	return org, nil
}

func (s *BusinessStore) SoftDeleteOrganization(ctx context.Context, orgID int32, userID int32) error {
	if err := s.db.SoftDeleteOrganization(ctx, orgID); err != nil {
		slog.ErrorContext(ctx, "Failed to mark organization as deleted in DB", "orgID", orgID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted organization", "orgID", orgID)

	// update caches
	_ = s.cache.SetMissing(ctx, orgCacheKey(orgID), negativeCacheDuration)
	// invalidate user orgs in cache as we just deleted one
	_ = s.cache.Delete(ctx, userOrgsCacheKey(userID))

	return nil
}

// NOTE: by definition this does not include the owner as this relationship is set directly in the 'organizations' table
func (s *BusinessStore) RetrieveOrganizationUsers(ctx context.Context, orgID int32) ([]*dbgen.GetOrganizationUsersRow, error) {
	cacheKey := orgUsersCacheKey(orgID)

	if users, err := fetchCachedMany[dbgen.GetOrganizationUsersRow](ctx, s.cache, cacheKey, userCacheDuration); err == nil {
		return users, nil
	}

	users, err := s.db.GetOrganizationUsers(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch organization users", "orgID", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Fetched organization users", "orgID", orgID, "count", len(users))

	if len(users) > 0 {
		_ = s.cache.SetItem(ctx, cacheKey, users, userCacheDuration)
	}

	return users, nil
}

func (s *BusinessStore) InviteUserToOrg(ctx context.Context, orgID int32, userID int32) error {
	_, err := s.db.InviteUserToOrg(ctx, &dbgen.InviteUserToOrgParams{
		OrgID:  orgID,
		UserID: userID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to add user to org", "orgID", orgID, "userID", userID, common.ErrAttr(err))
	}

	// invalidate relevant caches
	_ = s.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = s.cache.Delete(ctx, orgUsersCacheKey(orgID))

	slog.DebugContext(ctx, "Added org membership invite", "orgID", orgID, "userID", userID)

	return nil
}

func (s *BusinessStore) JoinOrg(ctx context.Context, orgID int32, userID int32) error {
	err := s.db.UpdateOrgMembershipLevel(ctx, &dbgen.UpdateOrgMembershipLevelParams{
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
	_ = s.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = s.cache.Delete(ctx, orgUsersCacheKey(orgID))

	return nil
}

func (s *BusinessStore) LeaveOrg(ctx context.Context, orgID int32, userID int32) error {
	err := s.db.UpdateOrgMembershipLevel(ctx, &dbgen.UpdateOrgMembershipLevelParams{
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
	_ = s.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = s.cache.Delete(ctx, orgUsersCacheKey(orgID))

	return nil
}

func (s *BusinessStore) RemoveUserFromOrg(ctx context.Context, orgID int32, userID int32) error {
	err := s.db.RemoveUserFromOrg(ctx, &dbgen.RemoveUserFromOrgParams{
		OrgID:  orgID,
		UserID: userID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to remove user from org", "orgID", orgID, "userID", userID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Removed user from org", "orgID", orgID, "userID", userID)

	// invalidate relevant caches
	_ = s.cache.Delete(ctx, userOrgsCacheKey(userID))
	_ = s.cache.Delete(ctx, orgUsersCacheKey(orgID))

	return nil
}

func (s *BusinessStore) UpdateUser(ctx context.Context, userID int32, name string, newEmail, oldEmail string) error {
	user, err := s.db.UpdateUser(ctx, &dbgen.UpdateUserParams{
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
	_ = s.cache.Delete(ctx, emailCacheKey(oldEmail))

	if user != nil {
		_ = s.cache.SetItem(ctx, emailCacheKey(newEmail), user, userCacheDuration)
	}

	return nil
}

func (s *BusinessStore) SoftDeleteUser(ctx context.Context, userID int32, email string) error {
	if err := s.db.SoftDeleteUser(ctx, userID); err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user", "userID", userID, common.ErrAttr(err))
		return err
	} else {
		slog.DebugContext(ctx, "Soft-deleted user", "userID", userID)
	}

	if err := s.db.SoftDeleteUserOrganizations(ctx, Int(userID)); err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user organizations", "userID", userID, common.ErrAttr(err))
		// intentionally do not return here
	} else {
		slog.DebugContext(ctx, "Soft-deleted user organizations", "userID", userID)
	}

	if err := s.db.SoftDeleteUserAPIKeys(ctx, Int(userID)); err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete user API keys", "userID", userID, common.ErrAttr(err))
		// intentionally do not return here
	} else {
		slog.DebugContext(ctx, "Disabled user API keys", "userID", userID)
	}

	// TODO: Delete user API keys from cache

	// invalidate user caches
	userOrgsCacheKey := userOrgsCacheKey(userID)
	if orgs, err := fetchCachedMany[dbgen.GetUserOrganizationsRow](ctx, s.cache, userOrgsCacheKey, userCacheDuration); err == nil {
		for _, org := range orgs {
			_ = s.cache.Delete(ctx, orgCacheKey(org.Organization.ID))
			_ = s.cache.Delete(ctx, orgPropertiesCacheKey(org.Organization.ID))
		}
		_ = s.cache.Delete(ctx, userOrgsCacheKey)
	}

	_ = s.cache.Delete(ctx, emailCacheKey(email))

	return nil
}

func (s *BusinessStore) RetrieveUserAPIKeys(ctx context.Context, userID int32) ([]*dbgen.APIKey, error) {
	cacheKey := userAPIKeysCacheKey(userID)

	if keys, err := fetchCachedMany[dbgen.APIKey](ctx, s.cache, cacheKey, apiKeyCacheDuration); err == nil {
		return keys, nil
	}

	keys, err := s.db.GetUserAPIKeys(ctx, Int(userID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Retrieved API keys", "count", len(keys))

	if len(keys) > 0 {
		_ = s.cache.SetItem(ctx, cacheKey, keys, apiKeyCacheDuration)
	}

	return keys, err
}

func (s *BusinessStore) CreateAPIKey(ctx context.Context, userID int32, name string, expiration time.Time) (*dbgen.APIKey, error) {
	key, err := s.db.CreateAPIKey(ctx, &dbgen.CreateAPIKeyParams{
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
		_ = s.cache.SetItem(ctx, cacheKey, key, apiKeyCacheDuration)

		// invalidate keys cache
		_ = s.cache.Delete(ctx, userAPIKeysCacheKey(userID))
	}

	return key, nil
}

func (s *BusinessStore) SoftDeleteAPIKey(ctx context.Context, userID, keyID int32) error {
	key, err := s.db.SoftDeleteAPIKey(ctx, &dbgen.SoftDeleteAPIKeyParams{
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
		_ = s.cache.Delete(ctx, cacheKey)

		_ = s.cache.Delete(ctx, userAPIKeysCacheKey(userID))
	}

	return nil
}

func (s *BusinessStore) CreateSupportTicket(ctx context.Context, category dbgen.SupportCategory, message string, userID int32) error {
	ticket, err := s.db.CreateSupportTicket(ctx, &dbgen.CreateSupportTicketParams{
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
