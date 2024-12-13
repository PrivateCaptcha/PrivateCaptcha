package db

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrRecordNotFound   = errors.New("record not found")
	ErrSoftDeleted      = errors.New("record is marked as deleted")
	ErrDuplicateAccount = errors.New("this subscrption already has an account")
	ErrLocked           = errors.New("lock is already acquired")
	ErrMaintenance      = errors.New("maintenance mode")
	errInvalidCacheType = errors.New("cache record type does not match")
	markerData          = []byte{'{', '}'}
	PortalPropertyID    = "1ca8041a-5761-40a4-addf-f715a991bfea"
)

type BusinessStore struct {
	pool            *pgxpool.Pool
	defaultImpl     *businessStoreImpl
	cacheOnlyImpl   *businessStoreImpl
	cache           common.Cache[string, any]
	maintenanceMode atomic.Bool
}

type puzzleCacheMarker struct {
	Data [4]byte
}

func userCacheKey(id int32) string                    { return "user/" + strconv.Itoa(int(id)) }
func APIKeyCacheKey(str string) string                { return "apikey/" + str }
func puzzleCacheKey(str string) string                { return "puzzle/" + str }
func orgCacheKey(orgID int32) string                  { return "org/" + strconv.Itoa(int(orgID)) }
func orgPropertiesCacheKey(orgID int32) string        { return "orgprops/" + strconv.Itoa(int(orgID)) }
func propertyByIDCacheKey(propID int32) string        { return "prop/" + strconv.Itoa(int(propID)) }
func PropertyBySitekeyCacheKey(sitekey string) string { return "propeid/" + sitekey }
func userOrgsCacheKey(userID int32) string            { return "userorgs/" + strconv.Itoa(int(userID)) }
func orgUsersCacheKey(orgID int32) string             { return "orgusers/" + strconv.Itoa(int(orgID)) }
func userAPIKeysCacheKey(userID int32) string         { return "userapikeys/" + strconv.Itoa(int(userID)) }
func subscriptionCacheKey(sID int32) string           { return "subscr/" + strconv.Itoa(int(sID)) }
func notificationCacheKey(ID int32) string            { return "notif/" + strconv.Itoa(int(ID)) }

func NewBusiness(pool *pgxpool.Pool, cache common.Cache[string, any]) *BusinessStore {
	return &BusinessStore{
		pool:          pool,
		defaultImpl:   &businessStoreImpl{cache: cache, queries: dbgen.New(pool)},
		cacheOnlyImpl: &businessStoreImpl{cache: cache},
		cache:         cache,
	}
}

func (s *BusinessStore) UpdateConfig(maintenanceMode bool) {
	s.maintenanceMode.Store(maintenanceMode)
}

func (s *BusinessStore) impl() *businessStoreImpl {
	if s.maintenanceMode.Load() {
		return s.cacheOnlyImpl
	}

	return s.defaultImpl
}

func (s *BusinessStore) Ping(ctx context.Context) error {
	// NOTE: we always use "real" DB connection to check for ping
	return s.defaultImpl.ping(ctx)
}

func (s *BusinessStore) DeleteExpiredCache(ctx context.Context) error {
	return s.impl().deleteExpiredCache(ctx)
}

func (s *BusinessStore) GetCachedPropertyBySitekey(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	return s.impl().getCachedPropertyBySitekey(ctx, sitekey)
}

func (s *BusinessStore) RetrievePropertiesBySitekey(ctx context.Context, sitekeys []string) ([]*dbgen.Property, error) {
	return s.impl().retrievePropertiesBySitekey(ctx, sitekeys)
}

func (s *BusinessStore) GetCachedAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	return s.impl().getCachedAPIKey(ctx, secret)
}

// Fetches API keyfrom DB, backed by cache
func (s *BusinessStore) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	return s.impl().retrieveAPIKey(ctx, secret)
}

func (s *BusinessStore) CheckPuzzleCached(ctx context.Context, p *puzzle.Puzzle) bool {
	return s.impl().checkPuzzleCached(ctx, p.PuzzleIDString())
}

func (s *BusinessStore) CachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error {
	// this check should have been done before in the pipeline. Here the check only to safeguard storing in Redis
	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Skipping caching expired puzzle", "now", tnow, "expiration", p.Expiration)
		return nil
	}

	return s.impl().cachePuzzle(ctx, p, tnow)
}

func (s *BusinessStore) RetrieveUser(ctx context.Context, id int32) (*dbgen.User, error) {
	return s.impl().retrieveUser(ctx, id)
}

func (s *BusinessStore) FindUserByEmail(ctx context.Context, email string) (*dbgen.User, error) {
	return s.impl().findUserByEmail(ctx, email)
}

func (s *BusinessStore) FindUserBySubscriptionID(ctx context.Context, subscriptionID int32) (*dbgen.User, error) {
	return s.impl().findUserBySubscriptionID(ctx, subscriptionID)
}

func (s *BusinessStore) RetrieveUserOrganizations(ctx context.Context, userID int32) ([]*dbgen.GetUserOrganizationsRow, error) {
	return s.impl().retrieveUserOrganizations(ctx, userID)
}

func (s *BusinessStore) RetrieveOrganization(ctx context.Context, orgID int32) (*dbgen.Organization, error) {
	return s.impl().retrieveOrganization(ctx, orgID)
}

func (s *BusinessStore) RetrieveProperty(ctx context.Context, propID int32) (*dbgen.Property, error) {
	return s.impl().retrieveProperty(ctx, propID)
}

func (s *BusinessStore) CreateNewOrganization(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	return s.impl().createNewOrganization(ctx, name, userID)
}

func (s *BusinessStore) RetrieveSubscriptionsByUserIDs(ctx context.Context, userIDs []int32) ([]*dbgen.GetSubscriptionsByUserIDsRow, error) {
	return s.impl().retrieveSubscriptionsByUserIDs(ctx, userIDs)
}

func (s *BusinessStore) RetrieveUsersWithoutSubscription(ctx context.Context, userIDs []int32) ([]*dbgen.User, error) {
	return s.impl().retrieveUsersWithoutSubscription(ctx, userIDs)
}

func (s *BusinessStore) RetrieveSubscription(ctx context.Context, sID int32) (*dbgen.Subscription, error) {
	return s.impl().retrieveSubscription(ctx, sID)
}

func (s *BusinessStore) UpdateSubscription(ctx context.Context, params *dbgen.UpdateSubscriptionParams) (*dbgen.Subscription, error) {
	return s.impl().updateSubscription(ctx, params)
}

func (s *BusinessStore) CreateNewAccount(ctx context.Context, params *dbgen.CreateSubscriptionParams, email, name, orgName string, existingUserID int32) (*dbgen.User, *dbgen.Organization, error) {
	if s.maintenanceMode.Load() {
		return nil, nil, ErrMaintenance
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	db := dbgen.New(s.pool)
	tmpCache := NewTxCache()
	impl := &businessStoreImpl{cache: tmpCache, queries: db.WithTx(tx)}

	var subscriptionID *int32

	if params != nil {
		subscription, err := impl.createNewSubscription(ctx, params)
		if err != nil {
			return nil, nil, err
		}

		subscriptionID = &subscription.ID

		if existingUser, err := impl.findUserByEmail(ctx, email); err == nil {
			slog.InfoContext(ctx, "User with such email already exists", "userID", existingUser.ID, "subscriptionID", existingUser.SubscriptionID)
			if ((existingUser.ID == existingUserID) || (existingUserID == -1)) && !existingUser.SubscriptionID.Valid {
				if err := impl.updateUserSubscription(ctx, existingUser.ID, subscription.ID); err != nil {
					return nil, nil, err
				}

				err = tx.Commit(ctx)
				if err != nil {
					return nil, nil, err
				}

				tmpCache.Commit(ctx, s.cache)

				return existingUser, nil, nil
			} else {
				slog.ErrorContext(ctx, "Cannot update existing user with same email", "existingUserID", existingUser.ID,
					"passthrough", existingUserID, "subscribed", existingUser.SubscriptionID.Valid, "email", email)
				return nil, nil, ErrDuplicateAccount
			}
		}
	}

	user, err := impl.createNewUser(ctx, email, name, subscriptionID)
	if err != nil {
		return nil, nil, err
	}

	org, err := impl.createNewOrganization(ctx, orgName, user.ID)
	if err != nil {
		return nil, nil, err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, nil, err
	}

	tmpCache.Commit(ctx, s.cache)

	return user, org, nil
}

func (s *BusinessStore) FindOrgProperty(ctx context.Context, name string, orgID int32) (*dbgen.Property, error) {
	return s.impl().findOrgProperty(ctx, name, orgID)
}

func (s *BusinessStore) FindOrg(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	return s.impl().findOrg(ctx, name, userID)
}

func (s *BusinessStore) CreateNewProperty(ctx context.Context, params *dbgen.CreatePropertyParams) (*dbgen.Property, error) {
	return s.impl().createNewProperty(ctx, params)
}

func (s *BusinessStore) UpdateProperty(ctx context.Context, propID int32, name string, level dbgen.DifficultyLevel, growth dbgen.DifficultyGrowth) (*dbgen.Property, error) {
	return s.impl().updateProperty(ctx, propID, name, level, growth)
}

func (s *BusinessStore) SoftDeleteProperty(ctx context.Context, propID int32, orgID int32) error {
	return s.impl().softDeleteProperty(ctx, propID, orgID)
}

func (s *BusinessStore) RetrieveOrgProperties(ctx context.Context, orgID int32) ([]*dbgen.Property, error) {
	return s.impl().retrieveOrgProperties(ctx, orgID)
}

func (s *BusinessStore) UpdateOrganization(ctx context.Context, orgID int32, name string) (*dbgen.Organization, error) {
	return s.impl().updateOrganization(ctx, orgID, name)
}

func (s *BusinessStore) SoftDeleteOrganization(ctx context.Context, orgID int32, userID int32) error {
	return s.impl().softDeleteOrganization(ctx, orgID, userID)
}

func (s *BusinessStore) RetrieveOrganizationUsers(ctx context.Context, orgID int32) ([]*dbgen.GetOrganizationUsersRow, error) {
	return s.impl().retrieveOrganizationUsers(ctx, orgID)
}

func (s *BusinessStore) InviteUserToOrg(ctx context.Context, orgID int32, userID int32) error {
	return s.impl().inviteUserToOrg(ctx, orgID, userID)
}

func (s *BusinessStore) JoinOrg(ctx context.Context, orgID int32, userID int32) error {
	return s.impl().joinOrg(ctx, orgID, userID)
}

func (s *BusinessStore) LeaveOrg(ctx context.Context, orgID int32, userID int32) error {
	return s.impl().leaveOrg(ctx, orgID, userID)
}

func (s *BusinessStore) RemoveUserFromOrg(ctx context.Context, orgID int32, userID int32) error {
	return s.impl().removeUserFromOrg(ctx, orgID, userID)
}

func (s *BusinessStore) UpdateUser(ctx context.Context, userID int32, name string, newEmail, oldEmail string) error {
	return s.impl().updateUser(ctx, userID, name, newEmail, oldEmail)
}

func (s *BusinessStore) SoftDeleteUser(ctx context.Context, userID int32) error {
	if s.maintenanceMode.Load() {
		return ErrMaintenance
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	db := dbgen.New(s.pool)
	tmpCache := NewTxCache()
	impl := &businessStoreImpl{cache: tmpCache, queries: db.WithTx(tx)}
	err = impl.softDeleteUser(ctx, userID)
	if err != nil {
		return err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	tmpCache.Commit(ctx, s.cache)

	return nil
}

func (s *BusinessStore) RetrieveUserAPIKeys(ctx context.Context, userID int32) ([]*dbgen.APIKey, error) {
	return s.impl().retrieveUserAPIKeys(ctx, userID)
}

func (s *BusinessStore) CreateAPIKey(ctx context.Context, userID int32, name string, expiration time.Time, requestsPerSecond float64) (*dbgen.APIKey, error) {
	return s.impl().createAPIKey(ctx, userID, name, expiration, requestsPerSecond)
}

func (s *BusinessStore) UpdateAPIKey(ctx context.Context, externalID pgtype.UUID, expiration time.Time, enabled bool) error {
	return s.impl().updateAPIKey(ctx, externalID, expiration, enabled)
}

func (s *BusinessStore) DeleteAPIKey(ctx context.Context, userID, keyID int32) error {
	return s.impl().deleteAPIKey(ctx, userID, keyID)
}

func (s *BusinessStore) UpdateUserAPIKeysRateLimits(ctx context.Context, userID int32, requestsPerSecond float64) error {
	return s.impl().updateUserAPIKeysRateLimits(ctx, userID, requestsPerSecond)
}

func (s *BusinessStore) CreateSupportTicket(ctx context.Context, category dbgen.SupportCategory, message string, userID int32) (*dbgen.Support, error) {
	return s.impl().createSupportTicket(ctx, category, message, userID)
}

func (s *BusinessStore) CachePaddlePrices(ctx context.Context, prices map[string]int) error {
	return s.impl().cachePaddlePrices(ctx, prices)
}

func (s *BusinessStore) RetrievePaddlePrices(ctx context.Context) (map[string]int, error) {
	return s.impl().retrievePaddlePrices(ctx)
}

func (s *BusinessStore) AddUsageLimitsViolations(ctx context.Context, violations []*common.UserTimeCount) error {
	return s.impl().addUsageLimitsViolations(ctx, violations)
}

func (s *BusinessStore) RetrieveUsersWithConsecutiveViolations(ctx context.Context) ([]*dbgen.GetUsersWithConsecutiveViolationsRow, error) {
	return s.impl().retrieveUsersWithConsecutiveViolations(ctx)
}

func (s *BusinessStore) RetrieveUsersWithLargeViolations(ctx context.Context, from time.Time, rate float64) ([]*dbgen.GetUsersWithLargeViolationsRow, error) {
	return s.impl().retrieveUsersWithLargeViolations(ctx, from, rate)
}

func (s *BusinessStore) AcquireLock(ctx context.Context, name string, data []byte, expiration time.Time) (*dbgen.Lock, error) {
	if s.maintenanceMode.Load() {
		return nil, ErrMaintenance
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	db := dbgen.New(s.pool)
	impl := &businessStoreImpl{queries: db.WithTx(tx)}
	lock, err := impl.acquireLock(ctx, name, data, expiration)
	if err != nil {
		return nil, err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, err
	}

	return lock, nil
}

func (s *BusinessStore) ReleaseLock(ctx context.Context, name string) error {
	if s.maintenanceMode.Load() {
		return ErrMaintenance
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	db := dbgen.New(s.pool)
	impl := &businessStoreImpl{queries: db.WithTx(tx)}
	err = impl.releaseLock(ctx, name)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *BusinessStore) DeleteDeletedRecords(ctx context.Context, before time.Time) error {
	return s.impl().deleteDeletedRecords(ctx, before)
}

func (s *BusinessStore) RetrieveSoftDeletedProperties(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedPropertiesRow, error) {
	return s.impl().retrieveSoftDeletedProperties(ctx, before, limit)
}

func (s *BusinessStore) DeleteProperties(ctx context.Context, ids []int32) error {
	return s.impl().deleteProperties(ctx, ids)
}

func (s *BusinessStore) RetrieveSoftDeletedOrganizations(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedOrganizationsRow, error) {
	return s.impl().retrieveSoftDeletedOrganizations(ctx, before, limit)
}

func (s *BusinessStore) DeleteOrganizations(ctx context.Context, ids []int32) error {
	return s.impl().deleteOrganizations(ctx, ids)
}

func (s *BusinessStore) RetrieveSoftDeletedUsers(ctx context.Context, before time.Time, limit int) ([]*dbgen.GetSoftDeletedUsersRow, error) {
	return s.impl().retrieveSoftDeletedUsers(ctx, before, limit)
}

func (s *BusinessStore) DeleteUsers(ctx context.Context, ids []int32) error {
	return s.impl().deleteUsers(ctx, ids)
}

func (s *BusinessStore) RetrieveNotification(ctx context.Context, id int32) (*dbgen.SystemNotification, error) {
	return s.impl().retrieveNotification(ctx, id)
}

func (s *BusinessStore) RetrieveUserNotification(ctx context.Context, tnow time.Time, userID int32) (*dbgen.SystemNotification, error) {
	return s.impl().retrieveUserNotification(ctx, tnow, userID)
}

func (s *BusinessStore) CreateNotification(ctx context.Context, message string, tnow time.Time, duration *time.Duration, userID *int32) (*dbgen.SystemNotification, error) {
	return s.impl().createNotification(ctx, message, tnow, duration, userID)
}

func (s *BusinessStore) RetrieveProperties(ctx context.Context, limit int) ([]*dbgen.Property, error) {
	return s.impl().retrieveProperties(ctx, limit)
}
