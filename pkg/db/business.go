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
	negativeCacheDuration    = 1 * time.Minute
	propertyCacheDuration    = 1 * time.Minute
	apiKeyCacheDuration      = 1 * time.Minute
	userCacheDuration        = 1 * time.Minute
	orgCacheDuration         = 1 * time.Minute
	puzzleCacheDuration      = 1 * time.Minute
	emailCachePrefix         = "email/"
	PropertyOrgCachePrefix   = "proporg/"
	APIKeyCachePrefix        = "apikey/"
	puzzleCachePrefix        = "puzzle/"
	orgsCachePrefix          = "orgs/"
	orgCachePrefix           = "org/"
	orgPropertiesCachePrefix = "orgprops/"
	propertyCachePrefix      = "prop/"
	userOrgsCachePrefix      = "userorgs/"
)

type BusinessStore struct {
	db         *dbgen.Queries
	cache      common.Cache
	cancelFunc context.CancelFunc
}

type puzzleCacheMarker struct {
	Data [4]byte
}

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

// Fetches property from DB, backed by cache
func (s *BusinessStore) RetrievePropertyAndOrg(ctx context.Context, sitekey string) (*dbgen.PropertyAndOrgByExternalIDRow, error) {
	eid := UUIDFromSiteKey(sitekey)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	cacheKey := PropertyOrgCachePrefix + sitekey

	if property, err := fetchCachedOne[dbgen.PropertyAndOrgByExternalIDRow](ctx, s.cache, cacheKey, propertyCacheDuration); err == nil {
		return property, nil
	} else if err == ErrNegativeCacheHit {
		return nil, ErrNegativeCacheHit
	}

	propertyAndOrg, err := s.db.PropertyAndOrgByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.cache.SetMissing(ctx, cacheKey, negativeCacheDuration)
			return nil, ErrRecordNotFound
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by external ID", "sitekey", sitekey, common.ErrAttr(err))

		return nil, err
	}

	if propertyAndOrg != nil {
		_ = s.cache.SetItem(ctx, cacheKey, propertyAndOrg, propertyCacheDuration)

		if propertyAndOrg.Property.DeletedAt.Valid || propertyAndOrg.Organization.DeletedAt.Valid {
			return propertyAndOrg, ErrSoftDeleted
		}
	}

	return propertyAndOrg, nil
}

// Fetches API keyfrom DB, backed by cache
func (s *BusinessStore) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	eid := UUIDFromSecret(secret)
	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	cacheKey := APIKeyCachePrefix + secret

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
	}

	return apiKey, nil
}

func (s *BusinessStore) CheckPuzzleCached(ctx context.Context, p *puzzle.Puzzle) bool {
	key := puzzleCachePrefix + p.PuzzleIDString()

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

	key := puzzleCachePrefix + p.PuzzleIDString()
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

	cacheKey := emailCachePrefix + email
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
	cacheKey := userOrgsCachePrefix + strconv.Itoa(int(userID))

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
	cacheKey := orgCachePrefix + strconv.Itoa(int(orgID))

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
	cacheKey := propertyCachePrefix + strconv.Itoa(int(propID))

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
		cacheKey := orgCachePrefix + strconv.Itoa(int(org.ID))
		_ = s.cache.SetItem(ctx, cacheKey, org, orgCacheDuration)

		// invalidate user orgs in cache as we just created another one
		_ = s.cache.Delete(ctx, userOrgsCachePrefix+strconv.Itoa(int(org.UserID.Int32)))
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
		cacheKey := emailCachePrefix + email
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

	org, err := s.db.GetUserOrgByName(ctx, &dbgen.GetUserOrgByNameParams{
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

func (s *BusinessStore) CreateNewProperty(ctx context.Context, name string, orgID int32, userID int32, domain string, level dbgen.DifficultyLevel, growth dbgen.DifficultyGrowth) (*dbgen.Property, error) {
	property, err := s.db.CreateProperty(ctx, &dbgen.CreatePropertyParams{
		Name:      name,
		OrgID:     Int(orgID),
		CreatorID: Int(userID),
		Domain:    domain,
		Level:     level,
		Growth:    growth,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property in DB", "name", name, "org", orgID, common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Created new property", "id", property.ID, "name", name, "org", orgID)

	cacheKey := propertyCachePrefix + strconv.Itoa(int(property.ID))
	_ = s.cache.SetItem(ctx, cacheKey, property, propertyCacheDuration)
	// invalidate org properties in cache as we just created a new property
	_ = s.cache.Delete(ctx, orgPropertiesCachePrefix+strconv.Itoa(int(orgID)))

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

	cacheKey := propertyCachePrefix + strconv.Itoa(int(property.ID))
	_ = s.cache.SetItem(ctx, cacheKey, property, propertyCacheDuration)
	// invalidate org properties in cache as we just created a new property
	_ = s.cache.Delete(ctx, orgPropertiesCachePrefix+strconv.Itoa(int(property.OrgID.Int32)))

	return property, nil
}

func (s *BusinessStore) SoftDeleteProperty(ctx context.Context, propID int32, orgID int32) error {
	if err := s.db.SoftDeleteProperty(ctx, propID); err != nil {
		slog.ErrorContext(ctx, "Failed to mark property as deleted in DB", "propID", propID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted property", "propID", propID)

	// update caches
	_ = s.cache.SetMissing(ctx, propertyCachePrefix+strconv.Itoa(int(propID)), negativeCacheDuration)
	// invalidate org properties in cache as we just deleted a property
	_ = s.cache.Delete(ctx, orgPropertiesCachePrefix+strconv.Itoa(int(orgID)))

	return nil
}

func (s *BusinessStore) RetrieveOrgProperties(ctx context.Context, orgID int32) ([]*dbgen.Property, error) {
	cacheKey := orgPropertiesCachePrefix + strconv.Itoa(int(orgID))

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

	cacheKey := orgCachePrefix + strconv.Itoa(int(org.ID))
	_ = s.cache.SetItem(ctx, cacheKey, org, orgCacheDuration)
	// invalidate user orgs in cache as we just updated name
	_ = s.cache.Delete(ctx, userOrgsCachePrefix+strconv.Itoa(int(org.UserID.Int32)))

	return org, nil
}

func (s *BusinessStore) SoftDeleteOrganization(ctx context.Context, orgID int32, userID int32) error {
	if err := s.db.SoftDeleteOrganization(ctx, orgID); err != nil {
		slog.ErrorContext(ctx, "Failed to mark organization as deleted in DB", "orgID", orgID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Soft-deleted organization", "orgID", orgID)

	// update caches
	_ = s.cache.SetMissing(ctx, orgCachePrefix+strconv.Itoa(int(orgID)), negativeCacheDuration)
	// invalidate user orgs in cache as we just deleted one
	_ = s.cache.Delete(ctx, userOrgsCachePrefix+strconv.Itoa(int(userID)))

	return nil
}
