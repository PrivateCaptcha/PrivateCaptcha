package portal

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestSoftDeleteOrganization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create a new user and organization
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	// Verify that the organization is returned by FindUserOrganizations
	orgs, err := store.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to find user organizations: %v", err)
	}
	if len(orgs) != 1 || orgs[0].Organization.ID != org.ID {
		t.Errorf("Expected to find the created organization, but got: %v", orgs)
	}

	_, err = store.Impl().SoftDeleteOrganization(ctx, org, user)
	if err != nil {
		t.Fatalf("Failed to soft delete organization: %v", err)
	}

	orgs, err = store.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to find user organizations: %v", err)
	}
	if len(orgs) != 0 {
		t.Errorf("Expected to find no organizations after soft deletion, but got: %v", orgs)
	}
}

func TestSoftDeleteProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Retrieve the organization's properties
	orgProperties, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatalf("Failed to retrieve organization properties: %v", err)
	}

	// Ensure the created property is present
	idx := slices.IndexFunc(orgProperties, func(p *dbgen.Property) bool { return p.ID == prop.ID })
	if idx == -1 {
		t.Errorf("Created property not found in organization properties")
	}

	// Soft delete the property
	_, err = store.Impl().SoftDeleteProperty(ctx, prop, org, user)
	if err != nil {
		t.Fatalf("Failed to soft delete property: %v", err)
	}

	// Retrieve the organization's properties again
	orgProperties, _, err = store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatalf("Failed to retrieve organization properties: %v", err)
	}

	// Ensure the soft-deleted property is not present
	idx = slices.IndexFunc(orgProperties, func(p *dbgen.Property) bool { return p.ID == prop.ID })
	if idx != -1 {
		t.Errorf("Soft-deleted property found in organization properties")
	}
}

func TestBusinessStoreImplFormPropertyRestrictions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	org2, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	form, property, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "form-restrictions.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org1)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	if _, err := store.Impl().SoftDeleteProperty(ctx, property, org1, user); !errors.Is(err, db.ErrPropertyAttachedToForm) {
		t.Fatalf("expected single soft delete to reject form-owned property with attached form error, got %v", err)
	}

	deletedIDs, _, err := store.Impl().SoftDeleteProperties(ctx, []int32{property.ID}, user, org1)
	if err != nil {
		t.Fatalf("expected bulk soft delete to return no result without error, got %v", err)
	}
	if _, deleted := deletedIDs[property.ID]; deleted {
		t.Fatalf("expected bulk soft delete not to delete form-owned property")
	}

	if _, _, err := store.Impl().MoveProperty(ctx, user, property, &dbgen.GetUserOrganizationsRow{Organization: *org2, Level: dbgen.AccessLevelOwner}); !errors.Is(err, db.ErrPropertyAttachedToForm) {
		t.Fatalf("expected move to reject form-owned property with attached form error, got %v", err)
	}

	if err := store.Impl().DeleteProperties(ctx, []int32{property.ID}); err != nil {
		t.Fatalf("expected hard delete to succeed, got %v", err)
	}

	var formCount int
	if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.forms WHERE id = $1", form.ID).Scan(&formCount); err != nil {
		t.Fatalf("failed to verify form cascade: %v", err)
	}
	if formCount != 0 {
		t.Fatalf("expected hard delete to cascade form row, got count %d", formCount)
	}
}

func acquireLock(ctx context.Context, store db.Implementor, name string, expiration time.Time) (*dbgen.Lock, error) {
	var lock *dbgen.Lock
	_, err := store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		var err error
		lock, err = impl.AcquireLock(ctx, name, nil, expiration)
		return nil, err
	})

	return lock, err
}

func TestLockTwice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	const lockDuration = 2 * time.Second
	var lockName = t.Name()

	initialExpiration := time.Now().UTC().Add(lockDuration).Truncate(time.Millisecond)
	_, err := acquireLock(ctx, store, lockName, initialExpiration)
	if err != nil {
		t.Fatal(err)
	}

	if lock, err := acquireLock(ctx, store, lockName, time.Now().UTC().Add(lockDuration)); err == nil {
		t.Fatalf("Was able to acquire a lock again right away. expires_at=%v", lock.ExpiresAt.Time)
	}

	const iterations = 100
	// PostgreSQL decides lock expiry with its own NOW(), so avoid asserting at the clock boundary.
	const expirationBoundaryMargin = 200 * time.Millisecond
	reacquireDeadline := initialExpiration.Add(-expirationBoundaryMargin)

	for i := 0; i < iterations; i++ {
		tnow := time.Now().UTC().Truncate(time.Millisecond)
		if !tnow.Before(reacquireDeadline) {
			break
		}

		if lock, err := acquireLock(ctx, store, lockName, tnow.Add(lockDuration)); err == nil {
			t.Fatalf("Was able to acquire a lock again. i=%v tnow=%v expires_at=%v", i, tnow, lock.ExpiresAt.Time)
		}

		time.Sleep(lockDuration / iterations)
	}

	if sleepDuration := time.Until(initialExpiration.Add(expirationBoundaryMargin)); sleepDuration > 0 {
		time.Sleep(sleepDuration)
	}

	// now it should succeed after the lock TTL
	_, err = acquireLock(ctx, store, lockName, time.Now().UTC().Add(lockDuration))
	if err != nil {
		t.Fatal(err)
	}
}

func TestLockUnlock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	const lockDuration = 10 * time.Second
	var lockName = t.Name()
	expiration := time.Now().UTC().Add(lockDuration)

	_, err := acquireLock(ctx, store, lockName, expiration)
	if err != nil {
		t.Fatal(err)
	}

	_, err = acquireLock(ctx, store, lockName, expiration)
	if err == nil {
		t.Fatal("Was able to acquire a lock again right away")
	}

	_, err = store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		return nil, impl.ReleaseLock(ctx, lockName)
	})
	if err != nil {
		t.Fatal(err)
	}

	// this time it should succeed as we just released the lock
	_, err = acquireLock(ctx, store, lockName, expiration)
	if err != nil {
		t.Fatal("Failed to acquire lock after release")
	}
}

func TestSystemNotification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	tnow := time.Now().UTC()

	// Create a new user and organization
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	if _, err := store.Impl().RetrieveSystemUserNotification(ctx, tnow, user.ID); err != db.ErrRecordNotFound {
		t.Errorf("Unexpected result for user notification: %v", err)
	}

	generalNotification, err := store.Impl().CreateSystemNotification(ctx, "message", tnow, nil /*duration*/, nil /*userID*/)
	if err != nil {
		t.Error(err)
	}

	if n, err := store.Impl().RetrieveSystemUserNotification(ctx, tnow, user.ID); (err != nil) || (n.ID != generalNotification.ID) {
		t.Errorf("Cannot retrieve generic user notification: %v", err)
	}

	userNotification, err := store.Impl().CreateSystemNotification(ctx, "message", tnow.Add(-1*time.Minute), nil /*duration*/, &user.ID)
	if err != nil {
		t.Error(err)
	}

	// specific notification has precedence over general one, even though both are active AND system notification is "fresher"
	if n, err := store.Impl().RetrieveSystemUserNotification(ctx, tnow, user.ID); (err != nil) || (n.ID != userNotification.ID) {
		t.Errorf("Cannot retrieve specific user notification: %v", err)
	}
}

// despite being called "Test Update Subscription", what we're actually checking are:
// - ability to find existing user account in `CreateNewAccount()`
// - not relying on cache inside the transaction
func TestUpdateUserSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	oldSubscriptionID := user.SubscriptionID.Int32

	subscrParams := db_tests.CreateNewSubscriptionParams(testPlan)
	subscrParams.Source = dbgen.SubscriptionSourceExternal

	newUser, _, _, err := store.Impl().CreateNewAccount(ctx, subscrParams, user.Email, "", common.DefaultOrgName, user.ID)
	if err != nil {
		t.Fatalf("Failed to update subscription: %v", err)
	}

	if newUser == user {
		t.Fatal("Same user reference returned")
	}

	if newUser.SubscriptionID.Int32 == oldSubscriptionID {
		t.Errorf("Subscription ID was not updated: %v", oldSubscriptionID)
	}

	subscr, err := store.Impl().RetrieveSubscription(ctx, newUser.SubscriptionID.Int32, false /*skip cache*/)
	if err != nil {
		t.Fatalf("Failed to fetch subscription: %v", err)
	}

	if subscr.ExternalSubscriptionID.String != subscrParams.ExternalSubscriptionID.String {
		t.Error("External subscription ID was not updated")
	}

	if subscr.Source != dbgen.SubscriptionSourceExternal {
		t.Errorf("Unexpected subscription source: %v", subscr.Source)
	}
}

func TestRetrievePropertiesByID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	prop1, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, "example1.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	prop2, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, "example2.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	batch := map[int32]uint{
		prop1.ID: 1,
		prop2.ID: 1,
	}

	properties, err := store.Impl().RetrievePropertiesByID(ctx, batch)
	if err != nil {
		t.Fatalf("Failed to retrieve properties by ID: %v", err)
	}

	if len(properties) != 2 {
		t.Errorf("Expected 2 properties, got %d", len(properties))
	}
}

func TestStoreInCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	key := t.Name() + "_cache_key"
	data := []byte("test data")
	ttl := 1 * time.Hour

	err := store.Impl().StoreInCache(ctx, key, data, ttl)
	if err != nil {
		t.Fatalf("Failed to store in cache: %v", err)
	}

	retrieved, err := store.Impl().RetrieveFromCache(ctx, key)
	if err != nil {
		t.Fatalf("Failed to retrieve from cache: %v", err)
	}

	if string(retrieved) != string(data) {
		t.Errorf("Retrieved data doesn't match: got %s, want %s", retrieved, data)
	}
}

func TestRetrieveOrgProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	retrieved, err := store.Impl().RetrieveOrgProperty(ctx, org, prop.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve org property: %v", err)
	}

	if retrieved.ID != prop.ID {
		t.Errorf("Retrieved property ID doesn't match: got %d, want %d", retrieved.ID, prop.ID)
	}
}

func TestRetrieveOrgPropertyNotCached(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, "uncached-example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	sitekey := db.UUIDToSiteKey(prop.ExternalID)

	// Delete property from cache to ensure it's not cached
	cacheKey := db.PropertyBySitekeyCacheKey(sitekey)
	if deleted := cache.Delete(ctx, cacheKey); !deleted {
		t.Log("Property was not in cache before deletion attempt")
	}

	// Verify property is NOT cached using GetCachedPropertyBySitekey
	_, _, err = store.Impl().GetCachedPropertyBySitekey(ctx, sitekey)
	if err != db.ErrCacheMiss {
		t.Fatalf("Expected ErrCacheMiss for uncached property, got: %v", err)
	}

	// Now retrieve the property (this should fetch from DB)
	retrieved, err := store.Impl().RetrieveOrgProperty(ctx, org, prop.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve org property: %v", err)
	}

	if retrieved.ID != prop.ID {
		t.Errorf("Retrieved property ID doesn't match: got %d, want %d", retrieved.ID, prop.ID)
	}
}

func TestUpdateUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	newName := "Updated Name"
	// Keep the same email - only change the name
	_, err = store.Impl().UpdateUser(ctx, user, newName, user.Email, user.Email)
	if err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	// Clear user cache to get fresh data from DB
	if deleted := cache.Delete(ctx, db.UserCacheKey(user.ID)); !deleted {
		t.Log("User cache entry not found")
	}

	updated, err := store.Impl().RetrieveUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve user: %v", err)
	}

	if updated.Name != newName {
		t.Errorf("User name was not updated: got %s, want %s", updated.Name, newName)
	}
}

func TestRetrieveDisabledUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Disable the user
	if err := db_tests.DisableUserForTest(ctx, store, user.ID); err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	// Clear user cache to ensure disabled status is fetched from DB
	if deleted := cache.Delete(ctx, db.UserCacheKey(user.ID)); !deleted {
		t.Log("User cache entry not found")
	}

	// RetrieveUser should return ErrDisabled
	_, err = store.Impl().RetrieveUser(ctx, user.ID)
	if err != db.ErrDisabled {
		t.Errorf("Expected ErrDisabled, got: %v", err)
	}

	// FindUserByEmail should also return ErrDisabled
	// Clear cache again
	cache.Delete(ctx, db.UserCacheKey(user.ID))
	_, err = store.Impl().FindUserByEmail(ctx, user.Email)
	if err != db.ErrDisabled {
		t.Errorf("Expected ErrDisabled from FindUserByEmail, got: %v", err)
	}
}

func TestRetrieveSoftDeletedUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Soft-delete the user
	if _, err := store.Impl().SoftDeleteUser(ctx, user); err != nil {
		t.Fatalf("Failed to soft-delete user: %v", err)
	}

	// Clear user cache to ensure soft-deleted status is fetched from DB
	if deleted := cache.Delete(ctx, db.UserCacheKey(user.ID)); !deleted {
		t.Log("User cache entry not found")
	}

	// RetrieveUser should return ErrSoftDeleted
	_, err = store.Impl().RetrieveUser(ctx, user.ID)
	if err != db.ErrSoftDeleted {
		t.Errorf("Expected ErrSoftDeleted, got: %v", err)
	}

	// FindUserByEmail should also return ErrSoftDeleted
	// Clear cache again
	cache.Delete(ctx, db.UserCacheKey(user.ID))
	_, err = store.Impl().FindUserByEmail(ctx, user.Email)
	if err != db.ErrSoftDeleted {
		t.Errorf("Expected ErrSoftDeleted from FindUserByEmail, got: %v", err)
	}
}

func TestRetrievePropertiesAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	_, _, err = store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	properties, err := store.Impl().RetrieveProperties(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to retrieve properties: %v", err)
	}

	if len(properties) == 0 {
		t.Error("Expected at least 1 property")
	}
}

func TestRetrieveOrgPropertiesCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	_, _, err = store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, "example1.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	_, _, err = store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, "example2.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	count, err := store.Impl().RetrieveOrgPropertiesCount(ctx, org.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve org properties count: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2, got %d", count)
	}
}

func TestWarmupPortalAuthJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &maintenance.WarmupPortalAuthJob{
		Store:               store,
		RegistrationAllowed: true,
	}

	// Run the job - it will try to cache portal login/register properties
	// Note: This may fail if the portal properties don't exist in test DB,
	// but that's expected - we just want to verify the job runs
	err := job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Error(err)
	}

	// If portal properties exist, check if they're cached
	loginSitekey := db.PortalLoginSitekey
	if _, _, err = store.Impl().GetCachedPropertyBySitekey(ctx, loginSitekey); err != nil {
		t.Error(err)
	}
}

func TestCleanupDBCacheJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Add some cache records with past expiration
	cacheKey := "test_cleanup_" + t.Name()
	cacheData := []byte("test data")

	// Store with very short TTL (1 millisecond)
	err := store.Impl().StoreInCache(ctx, cacheKey, cacheData, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to store in cache: %v", err)
	}
	queries := dbgen.New(store.Pool)
	expiredSID := t.Name() + "-expired"
	liveSID := t.Name() + "-live"
	for sid, expiresAt := range map[string]time.Time{
		expiredSID: time.Now().Add(-time.Minute),
		liveSID:    time.Now().Add(time.Hour),
	} {
		if _, err := queries.CreateSession(ctx, &dbgen.CreateSessionParams{
			SessionID: sid,
			State:     dbgen.SessionStateAuthenticated,
			Data:      []byte{1},
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = queries.DeleteSessionsByID(context.WithoutCancel(ctx), []string{expiredSID, liveSID})
	})

	// Wait for the TTL to expire
	time.Sleep(10 * time.Millisecond)

	// Run the cleanup job
	job := &maintenance.CleanupDBCacheJob{
		Store: store,
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Fatalf("CleanupDBCacheJob failed: %v", err)
	}

	// After cleanup, the expired record should be gone
	if _, err = store.Impl().RetrieveFromCache(ctx, cacheKey); err == nil {
		t.Error("Item not deleted from cache")
	}
	var expiredCount int
	if err := store.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM backend.sessions WHERE session_id = $1", expiredSID).Scan(&expiredCount); err != nil {
		t.Fatal(err)
	}
	if expiredCount != 0 {
		t.Fatalf("expired session rows = %d, want 0", expiredCount)
	}
	if _, err := queries.GetSessionByID(ctx, liveSID); err != nil {
		t.Fatalf("live session was deleted: %v", err)
	}
}
