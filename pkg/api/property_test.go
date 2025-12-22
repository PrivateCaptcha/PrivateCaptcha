package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

func TestNormalizeApiPropertyInput(t *testing.T) {
	tests := []struct {
		name     string
		input    apiCreatePropertyInput
		expected apiCreatePropertyInput
	}{
		{
			name: "normalizes all fields with invalid/out-of-range values",
			input: apiCreatePropertyInput{
				Domain: "example.com",
				apiPropertySettings: apiPropertySettings{
					Name:            "  Test Property  ",
					Level:           999,
					Growth:          "invalid",
					ValiditySeconds: -100,
					MaxReplayCount:  2_000_000,
				},
			},
			expected: apiCreatePropertyInput{
				Domain: "example.com",
				apiPropertySettings: apiPropertySettings{
					Name:            "Test Property",
					Level:           int(common.MaxDifficultyLevel),
					Growth:          "medium",
					ValiditySeconds: int((6 * time.Hour).Seconds()),
					MaxReplayCount:  1_000_000,
				},
			},
		},
		{
			name: "clamps to minimum values",
			input: apiCreatePropertyInput{
				Domain: "example.com",
				apiPropertySettings: apiPropertySettings{
					Name:            "Test",
					Level:           0,
					Growth:          "medium",
					ValiditySeconds: 0,
					MaxReplayCount:  0,
				},
			},
			expected: apiCreatePropertyInput{
				Domain: "example.com",
				apiPropertySettings: apiPropertySettings{
					Name:            "Test",
					Level:           1,
					Growth:          "medium",
					ValiditySeconds: int((6 * time.Hour).Seconds()),
					MaxReplayCount:  1,
				},
			},
		},
		{
			name: "preserves valid values",
			input: apiCreatePropertyInput{
				Domain: "example.com",
				apiPropertySettings: apiPropertySettings{
					Name:            "Test Property",
					Level:           5,
					Growth:          "fast",
					ValiditySeconds: 3600,
					MaxReplayCount:  100,
				},
			},
			expected: apiCreatePropertyInput{
				Domain: "example.com",
				apiPropertySettings: apiPropertySettings{
					Name:            "Test Property",
					Level:           5,
					Growth:          "fast",
					ValiditySeconds: 3600,
					MaxReplayCount:  100,
				},
			},
		},
		{
			name: "accepts all valid growth values",
			input: apiCreatePropertyInput{
				Domain: "example.com",
				apiPropertySettings: apiPropertySettings{
					Name:            "Test",
					Level:           5,
					Growth:          "constant",
					ValiditySeconds: 3600,
					MaxReplayCount:  100,
				},
			},
			expected: apiCreatePropertyInput{
				Domain: "example.com",
				apiPropertySettings: apiPropertySettings{
					Name:            "Test",
					Level:           5,
					Growth:          "constant",
					ValiditySeconds: 3600,
					MaxReplayCount:  100,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &apiCreatePropertyInput{}
			*input = tt.input
			input.Normalize()

			if input.Name != tt.expected.Name {
				t.Errorf("Name: got %q, want %q", input.Name, tt.expected.Name)
			}
			if input.Level != tt.expected.Level {
				t.Errorf("Level: got %d, want %d", input.Level, tt.expected.Level)
			}
			if input.Growth != tt.expected.Growth {
				t.Errorf("Growth: got %q, want %q", input.Growth, tt.expected.Growth)
			}
			if input.MaxReplayCount != tt.expected.MaxReplayCount {
				t.Errorf("MaxReplayCount: got %d, want %d", input.MaxReplayCount, tt.expected.MaxReplayCount)
			}
			if input.ValiditySeconds != tt.expected.ValiditySeconds {
				t.Errorf("ValiditySeconds: got %d, want %d", input.ValiditySeconds, tt.expected.ValiditySeconds)
			}
		})
	}
}

func TestNormalizeApiUpdatePropertyInput(t *testing.T) {
	input := apiUpdatePropertyInput{
		ID: "test-id",
		apiPropertySettings: apiPropertySettings{
			Name:            "  Test Property  ",
			Level:           999,
			Growth:          "invalid",
			ValiditySeconds: -100,
			MaxReplayCount:  2_000_000,
		},
	}

	expected := apiUpdatePropertyInput{
		ID: "test-id",
		apiPropertySettings: apiPropertySettings{
			Name:            "Test Property",
			Level:           int(common.MaxDifficultyLevel),
			Growth:          "medium",
			ValiditySeconds: int((6 * time.Hour).Seconds()),
			MaxReplayCount:  1_000_000,
		},
	}

	input.Normalize()

	if input.ID != expected.ID {
		t.Errorf("ID: got %q, want %q", input.ID, expected.ID)
	}
	if input.Name != expected.Name {
		t.Errorf("Name: got %q, want %q", input.Name, expected.Name)
	}
	if input.Level != expected.Level {
		t.Errorf("Level: got %d, want %d", input.Level, expected.Level)
	}
	if input.Growth != expected.Growth {
		t.Errorf("Growth: got %q, want %q", input.Growth, expected.Growth)
	}
	if input.MaxReplayCount != expected.MaxReplayCount {
		t.Errorf("MaxReplayCount: got %d, want %d", input.MaxReplayCount, expected.MaxReplayCount)
	}
	if input.ValiditySeconds != expected.ValiditySeconds {
		t.Errorf("ValiditySeconds: got %d, want %d", input.ValiditySeconds, expected.ValiditySeconds)
	}
}

func TestApiPostProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	const count = 10
	inputs := make([]*apiCreatePropertyInput, 0, count)
	for i := 0; i < count; i++ {
		inputs = append(inputs, &apiCreatePropertyInput{
			apiPropertySettings: apiPropertySettings{
				Name: fmt.Sprintf("%s %s %d", t.Name(), "Property", i),
			},
			Domain: fmt.Sprintf("example%d.com", i),
		})
	}

	output, meta, err := requestResponseAPISuite[*apiAsyncTaskOutput](ctx, inputs,
		http.MethodPost,
		fmt.Sprintf("/%s/%s/%s", common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertiesEndpoint),
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	finished := false
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)

		result, meta, err := requestResponseAPISuite[*apiAsyncTaskResultOutput](ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+output.ID, apiKey)
		if err != nil {
			t.Fatal(err)
		}

		if !meta.Code.Success() {
			t.Fatalf("Unexpected status code: %v", meta.Description)
		}

		if result.Finished {
			finished = true
			slog.DebugContext(ctx, "Async task is finished", "attempt", i)
			break
		}
	}

	if !finished {
		t.Fatal("Async task did not complete within timeout")
	}

	properties, _, err := s.BusinessDB.Impl().RetrieveOrgProperties(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}

	if len(properties) != len(inputs) {
		t.Fatalf("Unexpected number of properties: %v", len(properties))
	}

	for i := 0; i < len(inputs); i++ {
		if properties[i].Name != inputs[i].Name {
			t.Errorf("Property name does not match at %v", i)
		}

		if properties[i].Domain != inputs[i].Domain {
			t.Errorf("Property domain does not match at %v", i)
		}
	}
}

func TestApiPostPropertiesNoSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, err := db_test.CreateNewAccountForTestEx(ctx, store, t.Name(), nil /*subscription*/)
	if err != nil {
		t.Fatal(err)
	}

	keyParams := tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/)
	keyParams.Scope = dbgen.ApiKeyScopePortal
	apiKey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		t.Fatal(err)
	}

	const count = 2
	inputs := make([]*apiCreatePropertyInput, 0, count)
	for i := 0; i < count; i++ {
		inputs = append(inputs, &apiCreatePropertyInput{
			apiPropertySettings: apiPropertySettings{
				Name: fmt.Sprintf("%s %s %d", t.Name(), "Property", i),
			},
			Domain: fmt.Sprintf("example%d.com", i),
		})
	}

	apiKeyStr := db.UUIDToSecret(apiKey.ExternalID)

	resp, err := apiRequestSuite(ctx, inputs,
		http.MethodPost,
		fmt.Sprintf("/%s/%s/%s", common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertiesEndpoint),
		apiKeyStr)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestApiPostPropertiesOtherOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	const count = 2
	inputs := make([]*apiCreatePropertyInput, 0, count)
	for i := 0; i < count; i++ {
		inputs = append(inputs, &apiCreatePropertyInput{
			apiPropertySettings: apiPropertySettings{
				Name: fmt.Sprintf("%s %s %d", t.Name(), "Property", i),
			},
			Domain: fmt.Sprintf("example%d.com", i),
		})
	}

	_, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name()+"_another", testPlan)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := apiRequestSuite(ctx, inputs,
		http.MethodPost,
		fmt.Sprintf("/%s/%s/%s", common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertiesEndpoint),
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestApiDeleteProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org1, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Create another org for the same user
	org2, _, err := s.BusinessDB.Impl().CreateNewOrganization(ctx, t.Name()+"_org2", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Create properties
	// P1 in Org1
	p1, _, err := s.BusinessDB.Impl().CreateNewProperty(ctx, db_test.CreateNewPropertyParams(user.ID, "p1.com"), org1)
	if err != nil {
		t.Fatal(err)
	}
	// P2 in Org2
	p2, _, err := s.BusinessDB.Impl().CreateNewProperty(ctx, db_test.CreateNewPropertyParams(user.ID, "p2.com"), org2)
	if err != nil {
		t.Fatal(err)
	}
	// P3 in Org1 (should not be deleted)
	p3, _, err := s.BusinessDB.Impl().CreateNewProperty(ctx, db_test.CreateNewPropertyParams(user.ID, "p3.com"), org1)
	if err != nil {
		t.Fatal(err)
	}

	// Prepare request
	idsToDelete := []string{
		s.IDHasher.Encrypt(int(p1.ID)),
		s.IDHasher.Encrypt(int(p2.ID)),
	}

	output, meta, err := requestResponseAPISuite[*apiAsyncTaskOutput](ctx, idsToDelete,
		http.MethodDelete,
		"/"+common.PropertiesEndpoint,
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	finished := false
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)

		result, meta, err := requestResponseAPISuite[*apiAsyncTaskResultOutput](ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+output.ID, apiKey)
		if err != nil {
			t.Fatal(err)
		}

		if !meta.Code.Success() {
			t.Fatalf("Unexpected status code: %v", meta.Description)
		}

		if result.Finished {
			finished = true
			slog.DebugContext(ctx, "Async task is finished", "attempt", i)
			break
		}
	}

	if !finished {
		t.Fatal("Async task did not complete within timeout")
	}

	// Verify P1 deleted
	props1, _, err := s.BusinessDB.Impl().RetrieveOrgProperties(ctx, org1, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	// Should contain P3 but not P1
	foundP1 := false
	foundP3 := false
	for _, p := range props1 {
		if p.ID == p1.ID {
			foundP1 = true
		}
		if p.ID == p3.ID {
			foundP3 = true
		}
	}
	if foundP1 {
		t.Error("Property P1 should be deleted")
	}
	if !foundP3 {
		t.Error("Property P3 should exist")
	}

	// Verify P2 deleted
	props2, _, err := s.BusinessDB.Impl().RetrieveOrgProperties(ctx, org2, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	foundP2 := false
	for _, p := range props2 {
		if p.ID == p2.ID {
			foundP2 = true
		}
	}
	if foundP2 {
		t.Error("Property P2 should be deleted")
	}
}

func verifyPropertyUpdate(t *testing.T, property *dbgen.Property, expected *apiUpdatePropertyInput) {
	t.Helper()

	if property.Name != expected.Name {
		t.Errorf("Name: got %q, want %q", property.Name, expected.Name)
	}
	if int(property.Level.Int16) != expected.Level {
		t.Errorf("Level: got %d, want %d", property.Level.Int16, expected.Level)
	}
	if string(property.Growth) != expected.Growth {
		t.Errorf("Growth: got %q, want %q", property.Growth, expected.Growth)
	}
	if int(property.ValidityInterval.Seconds()) != expected.ValiditySeconds {
		t.Errorf("ValiditySeconds: got %d, want %d", int(property.ValidityInterval.Seconds()), expected.ValiditySeconds)
	}
	if property.AllowSubdomains != expected.AllowSubdomains {
		t.Errorf("AllowSubdomains: got %v, want %v", property.AllowSubdomains, expected.AllowSubdomains)
	}
	if property.AllowLocalhost != expected.AllowLocalhost {
		t.Errorf("AllowLocalhost: got %v, want %v", property.AllowLocalhost, expected.AllowLocalhost)
	}
	if int(property.MaxReplayCount) != expected.MaxReplayCount {
		t.Errorf("MaxReplayCount: got %d, want %d", property.MaxReplayCount, expected.MaxReplayCount)
	}
}

func TestApiUpdateProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org1, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Create another org for the same user
	org2, _, err := s.BusinessDB.Impl().CreateNewOrganization(ctx, t.Name()+"_org2", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Create properties
	// P1 in Org1
	p1, _, err := s.BusinessDB.Impl().CreateNewProperty(ctx, db_test.CreateNewPropertyParams(user.ID, "p1.com"), org1)
	if err != nil {
		t.Fatal(err)
	}
	// P2 in Org2
	p2, _, err := s.BusinessDB.Impl().CreateNewProperty(ctx, db_test.CreateNewPropertyParams(user.ID, "p2.com"), org2)
	if err != nil {
		t.Fatal(err)
	}

	// Prepare update request
	updates := []*apiUpdatePropertyInput{
		{
			ID: s.IDHasher.Encrypt(int(p1.ID)),
			apiPropertySettings: apiPropertySettings{
				Name:            "Updated Property 1",
				Level:           int(common.DifficultyLevelHigh),
				Growth:          string(dbgen.DifficultyGrowthMedium),
				ValiditySeconds: int(puzzle.ValidityDurations[7].Seconds()),
				AllowSubdomains: true,
				AllowLocalhost:  false,
				MaxReplayCount:  500,
			},
		},
		{
			ID: s.IDHasher.Encrypt(int(p2.ID)),
			apiPropertySettings: apiPropertySettings{
				Name:            "Updated Property 2",
				Level:           int(common.DifficultyLevelSmall),
				Growth:          string(dbgen.DifficultyGrowthFast),
				ValiditySeconds: int(puzzle.ValidityDurations[1].Seconds()),
				AllowSubdomains: false,
				AllowLocalhost:  true,
				MaxReplayCount:  200,
			},
		},
	}

	output, meta, err := requestResponseAPISuite[*apiAsyncTaskOutput](ctx, updates,
		http.MethodPut,
		"/"+common.PropertiesEndpoint,
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	finished := false
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)

		result, meta, err := requestResponseAPISuite[*apiAsyncTaskResultOutput](ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+output.ID, apiKey)
		if err != nil {
			t.Fatal(err)
		}

		if !meta.Code.Success() {
			t.Fatalf("Unexpected status code: %v", meta.Description)
		}

		if result.Finished {
			finished = true
			slog.DebugContext(ctx, "Async task is finished", "attempt", i)
			break
		}
	}

	if !finished {
		t.Fatal("Async task did not complete within timeout")
	}

	// Verify P1 updated
	updatedP1, err := s.BusinessDB.Impl().RetrieveOrgProperty(ctx, org1, p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	verifyPropertyUpdate(t, updatedP1, updates[0])

	// Verify P2 updated
	updatedP2, err := s.BusinessDB.Impl().RetrieveOrgProperty(ctx, org2, p2.ID)
	if err != nil {
		t.Fatal(err)
	}
	verifyPropertyUpdate(t, updatedP2, updates[1])
}

func TestApiGetProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3*db.MaxOrgPropertiesPageSize/2; i++ {
		if _, _, err := s.BusinessDB.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, fmt.Sprintf("example%v.com", i)), org); err != nil {
			t.Fatalf("Failed to create new property: %v", err)
		}
	}

	// with api key 1 it should work
	endpoint := fmt.Sprintf("/%s/%v/%s?page=1&per_page=%d", common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertiesEndpoint, db.MaxOrgPropertiesPageSize/2-1)
	properties, meta, err := requestResponseAPISuite[[]*apiOrgPropertyOutput](ctx, nil, http.MethodGet, endpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	if actual := len(properties); actual != db.MaxOrgPropertiesPageSize/2-1 {
		t.Fatalf("Unexpected number of properties: %v", actual)
	}
}

func TestApiGetProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := s.BusinessDB.Impl().CreateNewProperty(ctx, db_test.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatal(err)
	}

	propertyID := s.IDHasher.Encrypt(int(property.ID))
	output, meta, err := requestResponseAPISuite[*apiPropertyOutput](ctx, nil,
		http.MethodGet,
		fmt.Sprintf("/%s/%s/%s/%s", common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)),
			common.PropertyEndpoint, propertyID),
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	if output.ID != propertyID {
		t.Errorf("Received property ID %v but %v expected", output.ID, property.ID)
	}

	if output.Name != property.Name {
		t.Errorf("Received property Name %v, but %v expected", output.Name, property.Name)
	}

	if output.Sitekey != db.UUIDToSiteKey(property.ExternalID) {
		t.Errorf("Unexpected property sitekey: %v", output.Sitekey)
	}

	if output.Level != int(property.Level.Int16) {
		t.Errorf("Received property Level %v but %v expected", output.Level, property.Level.Int16)
	}

	if output.Growth != string(property.Growth) {
		t.Errorf("Received property Growth %v but %v expected", output.Growth, property.Growth)
	}

	if output.ValiditySeconds != int(property.ValidityInterval.Seconds()) {
		t.Errorf("Received property Validity Seconds %v but %v expected", output.ValiditySeconds, property.ValidityInterval.Seconds())
	}

	if output.AllowSubdomains != property.AllowSubdomains {
		t.Errorf("Received property Subdomains %v but %v expected", output.AllowSubdomains, property.AllowSubdomains)
	}

	if output.AllowLocalhost != property.AllowLocalhost {
		t.Errorf("Received property Localhost %v but %v expected", output.AllowLocalhost, property.AllowLocalhost)
	}

	if output.MaxReplayCount != int(property.MaxReplayCount) {
		t.Errorf("Received property MaxReplayCount %v but %v expected", output.MaxReplayCount, property.MaxReplayCount)
	}
}

func TestApiGetPropertyPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	owner, org, _, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := s.BusinessDB.Impl().CreateNewProperty(ctx, db_test.CreateNewPropertyParams(owner.ID, "example.com"), org)
	if err != nil {
		t.Fatal(err)
	}

	_, _, apiKey, err := setupAPISuite(ctx, t.Name()+"_user2")
	if err != nil {
		t.Fatal(err)
	}

	propertyID := s.IDHasher.Encrypt(int(property.ID))

	resp, err := apiRequestSuite(ctx, nil,
		http.MethodGet,
		fmt.Sprintf("/%s/%s/%s/%s", common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)),
			common.PropertyEndpoint, propertyID),
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestApiPostPropertiesInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	inputs := []*apiCreatePropertyInput{
		{
			apiPropertySettings: apiPropertySettings{
				Name: "Property",
			},
			Domain: "example.com",
		},
	}

	apiKey := db.UUIDToSecret(*randomUUID())

	// We need a valid path structure even if auth fails, usually.
	// The route is /org/{org}/properties.
	// We can use a dummy org ID.
	dummyOrgID := s.IDHasher.Encrypt(123)

	resp, err := apiRequestSuite(ctx, inputs,
		http.MethodPost,
		fmt.Sprintf("/%s/%s/%s", common.OrgEndpoint, dummyOrgID, common.PropertiesEndpoint),
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestApiDeletePropertiesInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	idsToDelete := []string{"some-id"}
	apiKey := db.UUIDToSecret(*randomUUID())

	resp, err := apiRequestSuite(ctx, idsToDelete,
		http.MethodDelete,
		"/"+common.PropertiesEndpoint,
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestApiUpdatePropertiesInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	updates := []*apiUpdatePropertyInput{
		{
			ID: "some-id",
			apiPropertySettings: apiPropertySettings{
				Name: "Updated Property",
			},
		},
	}
	apiKey := db.UUIDToSecret(*randomUUID())

	resp, err := apiRequestSuite(ctx, updates,
		http.MethodPut,
		"/"+common.PropertiesEndpoint,
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestApiGetPropertiesInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	apiKey := db.UUIDToSecret(*randomUUID())
	dummyOrgID := s.IDHasher.Encrypt(123)

	endpoint := fmt.Sprintf("/%s/%v/%s", common.OrgEndpoint, dummyOrgID, common.PropertiesEndpoint)
	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, endpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestApiGetPropertyInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	apiKey := db.UUIDToSecret(*randomUUID())
	dummyOrgID := s.IDHasher.Encrypt(123)
	dummyPropID := s.IDHasher.Encrypt(456)

	resp, err := apiRequestSuite(ctx, nil,
		http.MethodGet,
		fmt.Sprintf("/%s/%s/%s/%s", common.OrgEndpoint, dummyOrgID,
			common.PropertyEndpoint, dummyPropID),
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}
