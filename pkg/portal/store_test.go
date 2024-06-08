package portal

import (
	"context"
	"slices"
	"testing"

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
