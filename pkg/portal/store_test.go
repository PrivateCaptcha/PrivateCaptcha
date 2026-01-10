package portal

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
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
	//propName, org.ID, org.UserID.Int32, domain, level, growth)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Retrieve the organization's properties
	orgProperties, _, err := store.Impl().RetrieveOrgProperties(ctx, org, 0, db.MaxOrgPropertiesPageSize)
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
	orgProperties, _, err = store.Impl().RetrieveOrgProperties(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatalf("Failed to retrieve organization properties: %v", err)
	}

	// Ensure the soft-deleted property is not present
	idx = slices.IndexFunc(orgProperties, func(p *dbgen.Property) bool { return p.ID == prop.ID })
	if idx != -1 {
		t.Errorf("Soft-deleted property found in organization properties")
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

	const iterations = 100
	i := 0

	for i = 0; i < iterations; i++ {
		tnow := time.Now().UTC().Truncate(time.Millisecond)
		if tnow.Equal(initialExpiration) || tnow.After(initialExpiration) {
			// lock is actually not active anymore so it's not an error
			break
		}

		if lock, err := acquireLock(ctx, store, lockName, tnow.Add(lockDuration)); err == nil {
			t.Fatalf("Was able to acquire a lock again. i=%v tnow=%v expires_at=%v", i, tnow, lock.ExpiresAt.Time)
		}

		time.Sleep(lockDuration / iterations)
	}

	if i < 75 {
		t.Errorf("Lock was released too soon. i=%v", i)
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
		t.Fatal("Was able to acquire a lock again right away")
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

	subscr, err := store.Impl().RetrieveSubscription(ctx, newUser.SubscriptionID.Int32)
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
	_, err = store.Impl().GetCachedPropertyBySitekey(ctx, sitekey, nil)
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

func TestRetrieveTrialUsers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create a trial user
	subParams := db_tests.CreateNewSubscriptionParams(nil)
	subParams.Status = "trialing"

	user, _, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name(), subParams)
	if err != nil {
		t.Fatalf("Failed to create trial account: %v", err)
	}

	// Retrieve trial users with proper params
	// from, to times span the trial period, status is "trialing"
	tnow := time.Now().UTC()
	from := tnow.Add(-30 * 24 * time.Hour)
	to := tnow
	trialUsers, err := store.Impl().RetrieveTrialUsers(ctx, from, to, "trialing", 100, true)
	if err != nil {
		t.Fatalf("Failed to retrieve trial users: %v", err)
	}

	// Check that our trial user is in the list
	found := false
	for _, tu := range trialUsers {
		if tu.ID == user.ID {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected to find trial user %d in list, but didn't", user.ID)
	}
}
