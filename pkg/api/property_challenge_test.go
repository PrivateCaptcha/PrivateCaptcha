//go:build enterprise

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

type stubFeatureFlags bool

func (s stubFeatureFlags) Enabled(context.Context, string, *int32, *int32) bool {
	return bool(s)
}

type featureFlagsFunc func(context.Context, string, *int32, *int32) bool

func (f featureFlagsFunc) Enabled(ctx context.Context, feature string, userID, orgID *int32) bool {
	return f(ctx, feature, userID, orgID)
}

type propertyBudgetConfigItem struct {
	value string
	reads int
}

func (item *propertyBudgetConfigItem) Key() common.ConfigKey { return common.Argon2IDMemoryBudgetKey }
func (item *propertyBudgetConfigItem) Value() string {
	item.reads++
	return item.value
}

func TestReadPropertiesRequestChallengeSupport(t *testing.T) {
	tests := []struct {
		name           string
		budget         string
		challenge      string
		disableFeature bool
		want           common.StatusCode
	}{
		{name: "DisabledArgon2ID", budget: "0", challenge: "argon2id", want: common.StatusPropertyChallengeUnsupportedError},
		{name: "EmptyBudgetArgon2ID", challenge: "argon2id", want: common.StatusPropertyChallengeUnsupportedError},
		{name: "EnabledArgon2ID", budget: "256", challenge: "argon2id", want: common.StatusOK},
		{name: "FeatureDisabledArgon2ID", budget: "256", challenge: "argon2id", disableFeature: true, want: common.StatusPropertyChallengeUnsupportedError},
		{name: "FeatureDisabledBlake2b", budget: "256", challenge: "blake2b", disableFeature: true, want: common.StatusOK},
		{name: "FeatureDisabledDefault", budget: "256", disableFeature: true, want: common.StatusOK},
		{name: "DisabledBlake2b", budget: "0", challenge: "blake2b", want: common.StatusOK},
		{name: "DisabledDefault", budget: "0", want: common.StatusOK},
		{name: "DisabledUnknown", budget: "0", challenge: "unknown", want: common.StatusOK},
	}

	for _, operation := range []string{"Create", "Update"} {
		for _, tt := range tests {
			t.Run(operation+"/"+tt.name, func(t *testing.T) {
				budget := &propertyBudgetConfigItem{value: tt.budget}
				srv := &Server{
					FeatureFlags: stubFeatureFlags(!tt.disableFeature),
					BusinessDB: db.NewBusinessWithQuerier(nil, &db.QuerierStub{},
						db.NewStaticCache[db.CacheKey, any](100, &db.CacheMissingValue{})),
					IDHasher: common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "salt")),
					Verifier: &Verifier{Argon2IDMemoryBudgetKey: budget},
				}
				body := fmt.Sprintf(`[{"id":%q,"name":"First Property","challenge":"blake2b"},{"id":%q,"name":"Second Property","challenge":%q}]`,
					srv.IDHasher.Encrypt(1), srv.IDHasher.Encrypt(2), tt.challenge)
				request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
				request.Header.Set(common.HeaderContentType, common.ContentTypeJSON)

				var status common.StatusCode
				var err error
				var count int
				if operation == "Create" {
					var inputs []*apiCreatePropertyInput
					inputs, status, err = srv.readCreatePropertiesRequest(t.Context(), request, 1, 1)
					count = len(inputs)
				} else {
					var inputs []*apiUpdatePropertyInput
					inputs, status, err = srv.readUpdatePropertiesRequest(t.Context(), request, 1, nil)
					count = len(inputs)
				}
				if err != nil {
					t.Fatal(err)
				}
				if status != tt.want {
					t.Errorf("status = %d (%s), want %d", status, status.String(), tt.want)
				}
				wantCount := 0
				if tt.want.Success() {
					wantCount = 2
				}
				if count != wantCount {
					t.Errorf("input count = %d, want %d", count, wantCount)
				}
				if budget.reads != 1 {
					t.Errorf("budget read %d times, want once per batch", budget.reads)
				}
			})
		}
	}
}

func TestHandlePropertiesRechecksChallengeSupport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	for _, operation := range []string{"Create", "Update", "ScopedUpdate"} {
		for _, disabledBy := range []string{"MemoryBudget", "FeatureFlag"} {
			t.Run(operation+"/"+disabledBy, func(t *testing.T) {
				ctx := common.TraceContext(t.Context(), t.Name())
				user, org, _, err := setupAPISuite(ctx, t.Name())
				if err != nil {
					t.Fatal(err)
				}
				budget := &propertyBudgetConfigItem{value: "256"}
				srv := &Server{
					FeatureFlags:       stubFeatureFlags(true),
					BusinessDB:         store,
					SubscriptionLimits: server.SubscriptionLimits,
					IDHasher:           server.IDHasher,
					Verifier:           &Verifier{Argon2IDMemoryBudgetKey: budget},
				}

				var inputs []*apiUpdatePropertyInput
				var originals []*dbgen.Property
				for i, challenge := range []string{"blake2b", "argon2id", ""} {
					input := &apiUpdatePropertyInput{
						apiPropertySettings: apiPropertySettings{Name: fmt.Sprintf("Property %d", i), Challenge: challenge},
					}
					if operation != "Create" {
						params := db_test.CreateNewPropertyParams(user.ID, fmt.Sprintf("example%d.com", i))
						if i == 2 {
							params.Challenge = dbgen.NullChallengeType{ChallengeType: dbgen.ChallengeTypeArgon2ID, Valid: true}
						}
						property, _, err := store.Impl().CreateNewProperty(ctx, params, org)
						if err != nil {
							t.Fatal(err)
						}
						input.ID = srv.IDHasher.Encrypt(int(property.ID))
						originals = append(originals, property)
					}
					inputs = append(inputs, input)
				}
				body, err := json.Marshal(inputs)
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
				request.Header.Set(common.HeaderContentType, common.ContentTypeJSON)
				task := &dbgen.AsyncTask{UserID: db.Int(user.ID)}
				var status common.StatusCode
				if operation == "Create" {
					var validated []*apiCreatePropertyInput
					validated, status, err = srv.readCreatePropertiesRequest(ctx, request, user.ID, org.ID)
					if err == nil && status.Success() {
						task.Input, err = json.Marshal(&asyncTaskCreateProperties{OrgID: org.ID, Properties: validated})
					}
				} else {
					var validated []*apiUpdatePropertyInput
					var orgID *int32
					if operation == "ScopedUpdate" {
						orgID = &org.ID
					}
					validated, status, err = srv.readUpdatePropertiesRequest(ctx, request, user.ID, orgID)
					if err == nil && status.Success() {
						params := &asyncTaskUpdateProperties{Properties: validated}
						if operation == "ScopedUpdate" {
							params.AllowedOrgID = org.ID
						}
						task.Input, err = json.Marshal(params)
					}
				}
				if err != nil || !status.Success() {
					t.Fatalf("enabled request failed: status = %s, error = %v", status, err)
				}

				if disabledBy == "MemoryBudget" {
					budget.value = "0"
				} else {
					srv.FeatureFlags = stubFeatureFlags(false)
				}
				budget.reads = 0
				var data []byte
				if operation == "Create" {
					data, err = srv.handleCreateProperties(ctx, task)
				} else {
					data, err = srv.handleUpdateProperties(ctx, task)
				}
				if err != nil {
					t.Fatal(err)
				}
				var results []*operationResult
				if err := json.Unmarshal(data, &results); err != nil {
					t.Fatal(err)
				}
				if len(results) != len(inputs) {
					t.Fatalf("result count = %d, want %d", len(results), len(inputs))
				}
				for i, want := range []common.StatusCode{common.StatusOK, common.StatusPropertyChallengeUnsupportedError, common.StatusOK} {
					if results[i].Code != want {
						t.Errorf("result %d = %d, want %d", i, results[i].Code, want)
					}
				}
				if budget.reads != 1 {
					t.Errorf("budget read %d times, want once per batch", budget.reads)
				}

				properties, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
				if err != nil {
					t.Fatal(err)
				}
				if operation == "Create" {
					if len(properties) != 2 {
						t.Fatalf("property count = %d, want 2", len(properties))
					}
					for i, input := range []*apiUpdatePropertyInput{inputs[0], inputs[2]} {
						property := properties[i]
						if property.Name != input.Name || property.Challenge != dbgen.ChallengeTypeBlake2b {
							t.Errorf("unexpected created property: name = %q, challenge = %q", property.Name, property.Challenge)
						}
					}
				} else {
					if len(properties) != 3 {
						t.Fatalf("property count = %d, want 3", len(properties))
					}
					for i, property := range properties {
						wantName := inputs[i].Name
						if i == 1 {
							wantName = originals[i].Name
						}
						if property.Name != wantName || property.Challenge != originals[i].Challenge {
							t.Errorf("property %d: name = %q, challenge = %q; want %q, %q",
								i, property.Name, property.Challenge, wantName, originals[i].Challenge)
						}
					}
				}
			})
		}
	}
}

func TestAPIPropertiesFeatureFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	for _, operation := range []string{"Create", "Update"} {
		for _, scoped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/Scoped%v", operation, scoped), func(t *testing.T) {
				ctx := common.TraceContext(t.Context(), t.Name())
				user, org, key, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, false, scoped)
				if err != nil {
					t.Fatal(err)
				}
				params := db_test.CreateNewPropertyParams(user.ID, "feature.example.com")
				params.Challenge = dbgen.NullChallengeType{
					ChallengeType: dbgen.ChallengeTypeArgon2ID,
					Valid:         true,
				}
				property, _, err := store.Impl().CreateNewProperty(ctx, params, org)
				if err != nil {
					t.Fatal(err)
				}
				previousFlags := server.FeatureFlags
				t.Cleanup(func() { server.FeatureFlags = previousFlags })
				server.FeatureFlags = featureFlagsFunc(func(_ context.Context, feature string, userID, orgID *int32) bool {
					if feature != common.FeatureArgon2ID || userID == nil || *userID != user.ID {
						return true
					}
					if operation == "Create" || scoped {
						return orgID == nil || *orgID != org.ID
					}
					return orgID != nil
				})
				settings := apiPropertySettings{
					Name:      "Feature-gated property",
					Challenge: string(dbgen.ChallengeTypeArgon2ID),
				}
				var input any
				method := http.MethodPut
				endpoint := "/" + common.PropertiesEndpoint
				if operation == "Create" {
					method = http.MethodPost
					endpoint = fmt.Sprintf("/%s/%s/%s", common.OrgEndpoint, server.IDHasher.Encrypt(int(org.ID)), common.PropertiesEndpoint)
					input = []*apiCreatePropertyInput{{apiPropertySettings: settings}}
				} else {
					input = []*apiUpdatePropertyInput{{
						ID:                  server.IDHasher.Encrypt(int(property.ID)),
						apiPropertySettings: settings,
					}}
				}
				_, meta, err := requestResponseAPISuite[*apiAsyncTaskOutput](ctx, input, method, endpoint, key)
				if err != nil {
					t.Fatal(err)
				}
				if meta.Code != common.StatusPropertyChallengeUnsupportedError {
					t.Fatalf("status = %s, want unsupported challenge", meta.Code)
				}
				properties, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
				if err != nil {
					t.Fatal(err)
				}
				if len(properties) != 1 || properties[0].Name != property.Name || properties[0].Challenge != property.Challenge {
					t.Fatal("rejected request modified properties")
				}
			})
		}
	}
}
