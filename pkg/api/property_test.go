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
)

func TestNormalizeApiPropertyInput(t *testing.T) {
	tests := []struct {
		name     string
		input    apiPropertyInput
		expected apiPropertyInput
	}{
		{
			name: "normalizes all fields with invalid/out-of-range values",
			input: apiPropertyInput{
				Name:            "  Test Property  ",
				Domain:          "example.com",
				Level:           999,
				Growth:          "invalid",
				ValiditySeconds: -100,
				MaxReplayCount:  2_000_000,
			},
			expected: apiPropertyInput{
				Name:            "Test Property",
				Domain:          "example.com",
				Level:           int(common.MaxDifficultyLevel),
				Growth:          "medium",
				ValiditySeconds: int((6 * time.Hour).Seconds()),
				MaxReplayCount:  1_000_000,
			},
		},
		{
			name: "clamps to minimum values",
			input: apiPropertyInput{
				Name:            "Test",
				Domain:          "example.com",
				Level:           0,
				Growth:          "medium",
				ValiditySeconds: 0,
				MaxReplayCount:  0,
			},
			expected: apiPropertyInput{
				Name:            "Test",
				Domain:          "example.com",
				Level:           1,
				Growth:          "medium",
				ValiditySeconds: int((6 * time.Hour).Seconds()),
				MaxReplayCount:  1,
			},
		},
		{
			name: "preserves valid values",
			input: apiPropertyInput{
				Name:            "Test Property",
				Domain:          "example.com",
				Level:           5,
				Growth:          "fast",
				ValiditySeconds: 3600,
				MaxReplayCount:  100,
			},
			expected: apiPropertyInput{
				Name:            "Test Property",
				Domain:          "example.com",
				Level:           5,
				Growth:          "fast",
				ValiditySeconds: 3600,
				MaxReplayCount:  100,
			},
		},
		{
			name: "accepts all valid growth values",
			input: apiPropertyInput{
				Name:            "Test",
				Domain:          "example.com",
				Level:           5,
				Growth:          "constant",
				ValiditySeconds: 3600,
				MaxReplayCount:  100,
			},
			expected: apiPropertyInput{
				Name:            "Test",
				Domain:          "example.com",
				Level:           5,
				Growth:          "constant",
				ValiditySeconds: 3600,
				MaxReplayCount:  100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &apiPropertyInput{}
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
	inputs := make([]*apiPropertyInput, 0, count)
	for i := 0; i < count; i++ {
		inputs = append(inputs, &apiPropertyInput{
			Name:   fmt.Sprintf("%s %s %d", t.Name(), "Property", i),
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

	properties, _, err := s.BusinessDB.Impl().RetrieveOrgProperties(ctx, org, 0, db.OrgPropertiesPageSize)
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
	inputs := make([]*apiPropertyInput, 0, count)
	for i := 0; i < count; i++ {
		inputs = append(inputs, &apiPropertyInput{
			Name:   fmt.Sprintf("%s %s %d", t.Name(), "Property", i),
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
	inputs := make([]*apiPropertyInput, 0, count)
	for i := 0; i < count; i++ {
		inputs = append(inputs, &apiPropertyInput{
			Name:   fmt.Sprintf("%s %s %d", t.Name(), "Property", i),
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
	props1, _, err := s.BusinessDB.Impl().RetrieveOrgProperties(ctx, org1, 0, db.OrgPropertiesPageSize)
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
	props2, _, err := s.BusinessDB.Impl().RetrieveOrgProperties(ctx, org2, 0, db.OrgPropertiesPageSize)
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
