package portal

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func TestSoftDeleteOrganization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()

	// Create a new user and organization
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	// Verify that the organization is returned by FindUserOrganizations
	orgs, err := store.RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to find user organizations: %v", err)
	}
	if len(orgs) != 1 || orgs[0].Organization.ID != org.ID {
		t.Errorf("Expected to find the created organization, but got: %v", orgs)
	}

	err = store.SoftDeleteOrganization(ctx, org.ID, user.ID)
	if err != nil {
		t.Fatalf("Failed to soft delete organization: %v", err)
	}

	orgs, err = store.RetrieveUserOrganizations(ctx, user.ID)
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

	ctx := context.TODO()

	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	prop, err := store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       "Test Property",
		OrgID:      db.Int(org.ID),
		CreatorID:  org.UserID,
		OrgOwnerID: org.UserID,
		Domain:     "example.com",
		Level:      dbgen.DifficultyLevelMedium,
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	//propName, org.ID, org.UserID.Int32, domain, level, growth)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Retrieve the organization's properties
	orgProperties, err := store.RetrieveOrgProperties(ctx, org.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve organization properties: %v", err)
	}

	// Ensure the created property is present
	idx := slices.IndexFunc(orgProperties, func(p *dbgen.Property) bool { return p.ID == prop.ID })
	if idx == -1 {
		t.Errorf("Created property not found in organization properties")
	}

	// Soft delete the property
	err = store.SoftDeleteProperty(ctx, prop.ID, org.ID)
	if err != nil {
		t.Fatalf("Failed to soft delete property: %v", err)
	}

	// Retrieve the organization's properties again
	orgProperties, err = store.RetrieveOrgProperties(ctx, org.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve organization properties: %v", err)
	}

	// Ensure the soft-deleted property is not present
	idx = slices.IndexFunc(orgProperties, func(p *dbgen.Property) bool { return p.ID == prop.ID })
	if idx != -1 {
		t.Errorf("Soft-deleted property found in organization properties")
	}
}

func TestLockTwice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	const lockDuration = 2 * time.Second
	var lockName = t.Name()

	lock, err := store.AcquireLock(ctx, lockName, nil, lockDuration)
	if err != nil {
		t.Fatal(err)
	}
	initialExpiration := lock.ExpiresAt.Time

	const iterations = 100

	for i := 0; i < iterations; i++ {
		tnow := time.Now().UTC()
		if tnow.After(initialExpiration) {
			// lock is actually not active anymore so it's not an error
			break
		}

		if lock, err = store.AcquireLock(ctx, lockName, nil, lockDuration); err == nil {
			t.Fatalf("Was able to acquire a lock again right away. i=%v tnow=%v expires_at=%v", i, tnow, lock.ExpiresAt.Time)
		}

		time.Sleep(lockDuration / iterations)
	}

	// now it should succeed after the lock TTL
	_, err = store.AcquireLock(ctx, lockName, nil, lockDuration)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLockUnlock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	const lockDuration = 10 * time.Second
	var lockName = t.Name()

	_, err := store.AcquireLock(ctx, lockName, nil, lockDuration)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.AcquireLock(ctx, lockName, nil, lockDuration)
	if err == nil {
		t.Fatal("Was able to acquire a lock again right away")
	}

	err = store.ReleaseLock(ctx, lockName)
	if err != nil {
		t.Fatal(err)
	}

	// this time it should succeed as we just released the lock
	_, err = store.AcquireLock(ctx, lockName, nil, lockDuration)
	if err != nil {
		t.Fatal("Was able to acquire a lock again right away")
	}
}
