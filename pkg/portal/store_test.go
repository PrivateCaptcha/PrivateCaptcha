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
	orgProperties, err := store.Impl().RetrieveOrgProperties(ctx, org)
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
	orgProperties, err = store.Impl().RetrieveOrgProperties(ctx, org)
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
