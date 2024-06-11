package db

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
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
	errInvalidCacheType = errors.New("cache record type does not match")
	markerData          = []byte{'{', '}'}
)

type BusinessStore struct {
	pool        *pgxpool.Pool
	defaultImpl *businessStoreImpl
	cache       common.Cache
	cancelFunc  context.CancelFunc
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
func subscriptionCacheKey(sID int32) string           { return "subscr/" + strconv.Itoa(int(sID)) }

func NewBusiness(pool *pgxpool.Pool, cache common.Cache, cleanupInterval time.Duration) *BusinessStore {
	s := &BusinessStore{
		pool:        pool,
		defaultImpl: &businessStoreImpl{cache: cache, queries: dbgen.New(pool)},
		cache:       cache,
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
			err := s.defaultImpl.deleteExpiredCache(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to delete expired items", common.ErrAttr(err))
				continue
			}
		}
	}
	slog.Debug("Store cache cleanup finished")
}

func (s *BusinessStore) GetCachedPropertyBySitekey(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	return s.defaultImpl.getCachedPropertyBySitekey(ctx, sitekey)
}

func (s *BusinessStore) RetrievePropertiesBySitekey(ctx context.Context, sitekeys []string) ([]*dbgen.Property, error) {
	return s.defaultImpl.retrievePropertiesBySitekey(ctx, sitekeys)
}

// Fetches API keyfrom DB, backed by cache
func (s *BusinessStore) RetrieveAPIKey(ctx context.Context, secret string) (*dbgen.APIKey, error) {
	return s.defaultImpl.retrieveAPIKey(ctx, secret)
}

func (s *BusinessStore) CheckPuzzleCached(ctx context.Context, p *puzzle.Puzzle) bool {
	return s.defaultImpl.checkPuzzleCached(ctx, p.PuzzleIDString())
}

func (s *BusinessStore) CachePuzzle(ctx context.Context, p *puzzle.Puzzle, tnow time.Time) error {
	// this check should have been done before in the pipeline. Here the check only to safeguard storing in Redis
	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Skipping caching expired puzzle", "now", tnow, "expiration", p.Expiration)
		return nil
	}

	return s.defaultImpl.cachePuzzle(ctx, p, tnow)
}

func (s *BusinessStore) FindUser(ctx context.Context, email string) (*dbgen.User, error) {
	return s.defaultImpl.findUser(ctx, email)
}

func (s *BusinessStore) RetrieveUserOrganizations(ctx context.Context, userID int32) ([]*dbgen.GetUserOrganizationsRow, error) {
	return s.defaultImpl.retrieveUserOrganizations(ctx, userID)
}

func (s *BusinessStore) RetrieveOrganization(ctx context.Context, orgID int32) (*dbgen.Organization, error) {
	return s.defaultImpl.retrieveOrganization(ctx, orgID)
}

func (s *BusinessStore) RetrieveProperty(ctx context.Context, propID int32) (*dbgen.Property, error) {
	return s.defaultImpl.retrieveProperty(ctx, propID)
}

func (s *BusinessStore) CreateNewOrganization(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	return s.defaultImpl.createNewOrganization(ctx, name, userID)
}

func (s *BusinessStore) RetrieveSubscription(ctx context.Context, sID int32) (*dbgen.Subscription, error) {
	return s.defaultImpl.retrieveSubscription(ctx, sID)
}

func (s *BusinessStore) UpdateSubscription(ctx context.Context, params *dbgen.UpdateSubscriptionParams) error {
	return s.defaultImpl.updateSubscription(ctx, params)
}

func (s *BusinessStore) CreateNewAccount(ctx context.Context, params *dbgen.CreateSubscriptionParams, email, name, orgName string) (*dbgen.User, *dbgen.Organization, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	db := dbgen.New(s.pool)
	// TODO: Add cache implementation that will be flushed on success
	// because currently if transaction is cancelled, cache will still be used "as is"
	impl := &businessStoreImpl{cache: s.cache, queries: db.WithTx(tx)}

	var subscriptionID *int32

	if params != nil {
		subscription, err := impl.createNewSubscription(ctx, params)
		if err != nil {
			return nil, nil, err
		}

		subscriptionID = &subscription.ID

		if existingUser, err := impl.findUser(ctx, email); err == nil {
			slog.ErrorContext(ctx, "User with such email already exists", "userID", existingUser.ID, "subscriptionID", existingUser.SubscriptionID)
			// TODO: We also need to send and verify passthrough from Paddle here
			// to make sure it's the same account
			if !existingUser.SubscriptionID.Valid {
				if err := impl.updateUserSubscription(ctx, existingUser.ID, subscription.ID); err != nil {
					return nil, nil, err
				}
			}

			// we explicitly do nothing in such edge case, as this has to be resolved via support
			if txerr := tx.Commit(ctx); txerr != nil {
				return nil, nil, txerr
			}

			return nil, nil, ErrDuplicateAccount
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

	return user, org, tx.Commit(ctx)
}

func (s *BusinessStore) FindOrgProperty(ctx context.Context, name string, orgID int32) (*dbgen.Property, error) {
	return s.defaultImpl.findOrgProperty(ctx, name, orgID)
}

func (s *BusinessStore) FindOrg(ctx context.Context, name string, userID int32) (*dbgen.Organization, error) {
	return s.defaultImpl.findOrg(ctx, name, userID)
}

func (s *BusinessStore) CreateNewProperty(ctx context.Context, params *dbgen.CreatePropertyParams) (*dbgen.Property, error) {
	return s.defaultImpl.createNewProperty(ctx, params)
}

func (s *BusinessStore) UpdateProperty(ctx context.Context, propID int32, name string, level dbgen.DifficultyLevel, growth dbgen.DifficultyGrowth) (*dbgen.Property, error) {
	return s.defaultImpl.updateProperty(ctx, propID, name, level, growth)
}

func (s *BusinessStore) SoftDeleteProperty(ctx context.Context, propID int32, orgID int32) error {
	return s.defaultImpl.softDeleteProperty(ctx, propID, orgID)
}

func (s *BusinessStore) RetrieveOrgProperties(ctx context.Context, orgID int32) ([]*dbgen.Property, error) {
	return s.defaultImpl.retrieveOrgProperties(ctx, orgID)
}

func (s *BusinessStore) UpdateOrganization(ctx context.Context, orgID int32, name string) (*dbgen.Organization, error) {
	return s.defaultImpl.updateOrganization(ctx, orgID, name)
}

func (s *BusinessStore) SoftDeleteOrganization(ctx context.Context, orgID int32, userID int32) error {
	return s.defaultImpl.softDeleteOrganization(ctx, orgID, userID)
}

func (s *BusinessStore) RetrieveOrganizationUsers(ctx context.Context, orgID int32) ([]*dbgen.GetOrganizationUsersRow, error) {
	return s.defaultImpl.retrieveOrganizationUsers(ctx, orgID)
}

func (s *BusinessStore) InviteUserToOrg(ctx context.Context, orgID int32, userID int32) error {
	return s.defaultImpl.inviteUserToOrg(ctx, orgID, userID)
}

func (s *BusinessStore) JoinOrg(ctx context.Context, orgID int32, userID int32) error {
	return s.defaultImpl.joinOrg(ctx, orgID, userID)
}

func (s *BusinessStore) LeaveOrg(ctx context.Context, orgID int32, userID int32) error {
	return s.defaultImpl.leaveOrg(ctx, orgID, userID)
}

func (s *BusinessStore) RemoveUserFromOrg(ctx context.Context, orgID int32, userID int32) error {
	return s.defaultImpl.removeUserFromOrg(ctx, orgID, userID)
}

func (s *BusinessStore) UpdateUser(ctx context.Context, userID int32, name string, newEmail, oldEmail string) error {
	return s.defaultImpl.updateUser(ctx, userID, name, newEmail, oldEmail)
}

func (s *BusinessStore) SoftDeleteUser(ctx context.Context, userID int32, email string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	db := dbgen.New(s.pool)
	impl := &businessStoreImpl{cache: s.cache, queries: db.WithTx(tx)}
	err = impl.softDeleteUser(ctx, userID, email)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *BusinessStore) RetrieveUserAPIKeys(ctx context.Context, userID int32) ([]*dbgen.APIKey, error) {
	return s.defaultImpl.retrieveUserAPIKeys(ctx, userID)
}

func (s *BusinessStore) CreateAPIKey(ctx context.Context, userID int32, name string, expiration time.Time) (*dbgen.APIKey, error) {
	return s.defaultImpl.createAPIKey(ctx, userID, name, expiration)
}

func (s *BusinessStore) UpdateAPIKey(ctx context.Context, externalID pgtype.UUID, expiration time.Time, enabled bool) error {
	return s.defaultImpl.updateAPIKey(ctx, externalID, expiration, enabled)
}

func (s *BusinessStore) SoftDeleteAPIKey(ctx context.Context, userID, keyID int32) error {
	return s.defaultImpl.softDeleteAPIKey(ctx, userID, keyID)
}

func (s *BusinessStore) CreateSupportTicket(ctx context.Context, category dbgen.SupportCategory, message string, userID int32) error {
	return s.defaultImpl.createSupportTicket(ctx, category, message, userID)
}
