package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	common_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/rs/xid"
)

func apiRequestSuite(ctx context.Context, request interface{}, method, endpoint, apiKey string) (*http.Response, error) {
	return apiRequestSuiteWithIP(ctx, request, method, endpoint, apiKey, "")
}

func apiRequestSuiteWithIP(ctx context.Context, request interface{}, method, endpoint, apiKey, requesterIP string) (*http.Response, error) {
	srv := http.NewServeMux()
	server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	//srv.HandleFunc("/", catchAll)

	var reader io.Reader
	if request != nil {
		data, err := json.Marshal(request)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set(common.HeaderContentType, common.ContentTypeJSON)
	req.Header.Set(common.HeaderAPIKey, apiKey)
	if len(requesterIP) == 0 {
		requesterIP = common_test.GenerateRandomIPv4()
	}
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), requesterIP)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	return resp, nil
}

func requestResponseAPISuite[T any](ctx context.Context, request interface{}, method, endpoint, apiKey string) (T, *ResponseMetadata, error) {
	var zero T

	resp, err := apiRequestSuite(ctx, request, method, endpoint, apiKey)
	if err != nil {
		return zero, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read response body", common.ErrAttr(err))
		return zero, nil, err
	}

	var envelope APIResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal envelope", "body", string(body), common.ErrAttr(err))
		return zero, nil, err
	}

	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to marshal data back", common.ErrAttr(err))
		return zero, nil, err
	}

	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal type T", "body", string(raw), common.ErrAttr(err))
		return zero, nil, err
	}

	meta := new(ResponseMetadata)
	*meta = envelope.Meta

	return out, meta, nil
}

func setupAPISuite(ctx context.Context, username string) (*dbgen.User, *dbgen.Organization, string, error) {
	return setupAPISuiteEx(ctx, username, dbgen.ApiKeyScopePortal, false /*read-only*/, false /*org scope*/)
}

func setupAPISuiteEx(ctx context.Context, username string, scope dbgen.ApiKeyScope, readOnly bool, orgScope bool) (*dbgen.User, *dbgen.Organization, string, error) {
	user, org, err := db_test.CreateNewAccountForTest(ctx, store, username, testPlan)
	if err != nil {
		return nil, nil, "", err
	}

	keyParams := tests.CreateNewPuzzleAPIKeyParams(username+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/)
	keyParams.Scope = scope
	keyParams.Readonly = readOnly
	if orgScope {
		keyParams.OrgID = db.Int(org.ID)
	}
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		return nil, nil, "", err
	}

	return user, org, db.UUIDToSecret(apikey.ExternalID), nil
}

func TestAPICreateOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, baseOrg, apiKey, err := setupAPISuite(t.Context(), t.Name())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := server.BusinessDB.Impl().SoftDeleteOrganization(ctx, baseOrg, user); err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{
		Name: t.Name(),
	}

	org, meta, err := requestResponseAPISuite[*apiOrgOutput](ctx, input, http.MethodPost, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	if len(org.ID) == 0 {
		t.Error("Created org ID is empty")
	}

	if org.Name != input.Name {
		t.Error("Created org name is different")
	}
}

func TestAPIDeleteOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{
		ID: server.IDHasher.Encrypt(int(org.ID)),
	}

	_, meta, err := requestResponseAPISuite[json.RawMessage](ctx, input, http.MethodDelete, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	if _, _, err := server.BusinessDB.Impl().RetrieveUserOrganization(t.Context(), user, org.ID); (err != db.ErrSoftDeleted) && (err != db.ErrNegativeCacheHit) {
		t.Fatalf("Unexpected error when retrieving deleted org: %v", err)
	}
}

func TestAPIUpdateOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{
		ID:   server.IDHasher.Encrypt(int(org.ID)),
		Name: "Org Update " + xid.New().String(),
	}

	_, meta, err := requestResponseAPISuite[json.RawMessage](ctx, input, http.MethodPut, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	org, _, err = server.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, org.ID)
	if err != nil {
		t.Fatalf("Unexpected error when retrieving org: %v", err)
	}

	if org.Name != input.Name {
		t.Errorf("Org name was not updated. expected (%v), actual (%v)", input.Name, org.Name)
	}
}

func TestAPIUpdateOrgEmptyID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{
		ID:   "",
		Name: "Org Update " + xid.New().String(),
	}

	_, meta, err := requestResponseAPISuite[APIResponse](ctx, input, http.MethodPut, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Code != common.StatusOrgIDEmptyError {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	org, _, err = server.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, org.ID)
	if err != nil {
		t.Fatalf("Unexpected error when retrieving org: %v", err)
	}

	if org.Name == input.Name {
		t.Error("Org name was updated but shouldn't be")
	}
}

func TestAPIGetOrgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	orgs, meta, err := requestResponseAPISuite[[]*apiOrgOutput](ctx, nil, http.MethodGet, "/"+common.OrganizationsEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	if (len(orgs) != 1) || (orgs[0].Name != org.Name) {
		t.Errorf("Wrong organizations retrieved")
	}
}

func TestAPIOrgPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	_, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name()+"_another", testPlan)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := apiRequestSuite(ctx, nil,
		http.MethodDelete,
		fmt.Sprintf("/%s/%s", common.OrgEndpoint, server.IDHasher.Encrypt(int(org.ID))),
		apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPICreateOrgReadOnlyKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, true /*read-only*/, false /*org scope*/)
	if err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{
		Name: t.Name(),
	}

	resp, err := apiRequestSuite(ctx, input, http.MethodPost, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPICreateOrgInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	input := &apiOrgInput{
		Name: t.Name(),
	}

	apiKey := db.UUIDToSecret(*randomUUID())

	resp, err := apiRequestSuite(ctx, input, http.MethodPost, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIDeleteOrgInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	input := &apiOrgInput{
		ID: "some-id",
	}

	apiKey := db.UUIDToSecret(*randomUUID())

	resp, err := apiRequestSuite(ctx, input, http.MethodDelete, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIDeleteOrgReadOnlyKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, org, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, true /*read-only*/, false /*org scope*/)
	if err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{
		ID: server.IDHasher.Encrypt(int(org.ID)),
	}

	resp, err := apiRequestSuite(ctx, input, http.MethodDelete, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIUpdateOrgInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	input := &apiOrgInput{
		ID:   "some-id",
		Name: "Org Update",
	}

	apiKey := db.UUIDToSecret(*randomUUID())

	resp, err := apiRequestSuite(ctx, input, http.MethodPut, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIUpdateOrgReadOnlyKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, org, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, true /*read-only*/, false /*org scope*/)
	if err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{
		ID:   server.IDHasher.Encrypt(int(org.ID)),
		Name: "Org Update " + xid.New().String(),
	}

	resp, err := apiRequestSuite(ctx, input, http.MethodPut, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIGetOrgsInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	apiKey := db.UUIDToSecret(*randomUUID())

	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.OrganizationsEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIGetOrgsAPIKeyOrgScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, false /*read-only*/, true /*org scope*/)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	orgs, meta, err := requestResponseAPISuite[[]*apiOrgOutput](ctx, nil, http.MethodGet, "/"+common.OrganizationsEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	if (len(orgs) != 1) || (orgs[0].Name != org.Name) {
		t.Errorf("Wrong organizations retrieved")
	}
}

func TestAPICreateOrgAPIKeyOrgScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, false /*read-only*/, true /*org scope*/)
	if err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{
		Name: t.Name(),
	}

	resp, err := apiRequestSuite(ctx, input, http.MethodPost, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIDeleteOrgAPIKeyOrgScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, false /*read-only*/, true /*org scope*/)
	if err != nil {
		t.Fatal(err)
	}

	org2, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	input := &apiOrgInput{
		ID: server.IDHasher.Encrypt(int(org2.ID)),
	}

	resp, err := apiRequestSuite(ctx, input, http.MethodDelete, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIUpdateOrgAPIKeyOrgScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, false /*read-only*/, true /*org scope*/)
	if err != nil {
		t.Fatal(err)
	}

	org2, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	input := &apiOrgInput{
		ID:   server.IDHasher.Encrypt(int(org2.ID)),
		Name: "Org Update",
	}

	resp, err := apiRequestSuite(ctx, input, http.MethodPut, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestAPIOrgInvalidRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		method      string
		endpoint    string
		contentType string
		body        []byte
		wantStatus  int
	}{
		{
			name:        "Create Org - Invalid Content Type",
			method:      http.MethodPost,
			endpoint:    "/" + common.OrgEndpoint,
			contentType: "text/plain",
			body:        []byte("{}"),
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "Create Org - Malformed JSON",
			method:      http.MethodPost,
			endpoint:    "/" + common.OrgEndpoint,
			contentType: common.ContentTypeJSON,
			body:        []byte("{invalid-json"),
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "Update Org - Invalid Content Type",
			method:      http.MethodPut,
			endpoint:    "/" + common.OrgEndpoint,
			contentType: "text/plain",
			body:        []byte("{}"),
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "Update Org - Malformed JSON",
			method:      http.MethodPut,
			endpoint:    "/" + common.OrgEndpoint,
			contentType: common.ContentTypeJSON,
			body:        []byte("{invalid-json"),
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "Delete Org - Invalid Content Type",
			method:      http.MethodDelete,
			endpoint:    "/" + common.OrgEndpoint,
			contentType: "text/plain",
			body:        []byte("{}"),
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "Delete Org - Malformed JSON",
			method:      http.MethodDelete,
			endpoint:    "/" + common.OrgEndpoint,
			contentType: common.ContentTypeJSON,
			body:        []byte("{invalid-json"),
			wantStatus:  http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := http.NewServeMux()
			server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

			req, err := http.NewRequestWithContext(ctx, tt.method, tt.endpoint, bytes.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Set(common.HeaderAPIKey, apiKey)
			if tt.contentType != "" {
				req.Header.Set(common.HeaderContentType, tt.contentType)
			}
			// Bypass rate limiter
			req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if status := w.Result().StatusCode; status != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, status)
			}
		})
	}
}

func TestAPIOrgValidationErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		method   string
		input    *apiOrgInput
		wantCode common.StatusCode
	}{
		{
			name:     "Create Org - ID Not Empty",
			method:   http.MethodPost,
			input:    &apiOrgInput{ID: "some-id", Name: "Valid Name"},
			wantCode: common.StatusOrgIDNotEmptyError,
		},
		{
			name:     "Create Org - Empty Name",
			method:   http.MethodPost,
			input:    &apiOrgInput{Name: ""},
			wantCode: common.StatusOrgNameEmptyError,
		},
		{
			name:     "Create Org - Name Too Long",
			method:   http.MethodPost,
			input:    &apiOrgInput{Name: string(make([]byte, 300))},
			wantCode: common.StatusOrgNameTooLongError,
		},
		{
			name:     "Update Org - Empty Name",
			method:   http.MethodPut,
			input:    &apiOrgInput{ID: server.IDHasher.Encrypt(1), Name: ""},
			wantCode: common.StatusOrgNameEmptyError,
		},
		{
			name:     "Update Org - Invalid ID Format",
			method:   http.MethodPut,
			input:    &apiOrgInput{ID: "invalid-id-format!", Name: "Valid Name"},
			wantCode: common.StatusOrgIDInvalidError,
		},
		{
			name:     "Delete Org - Invalid ID Format",
			method:   http.MethodDelete,
			input:    &apiOrgInput{ID: "invalid-id-format!"},
			wantCode: common.StatusOrgIDInvalidError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, meta, err := requestResponseAPISuite[APIResponse](ctx, tt.input, tt.method, "/"+common.OrgEndpoint, apiKey)
			if err != nil {
				t.Fatal(err)
			}

			if meta.Code != tt.wantCode {
				t.Errorf("expected code %v, got %v (%s)", tt.wantCode, meta.Code, meta.Description)
			}
		})
	}
}

func TestAPIOrgNoSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, err := db_test.CreateNewAccountForTestEx(ctx, store, t.Name(), nil)
	if err != nil {
		t.Fatal(err)
	}

	keyParams := tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0)
	keyParams.Scope = dbgen.ApiKeyScopePortal
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		t.Fatal(err)
	}
	apiKeyStr := db.UUIDToSecret(apikey.ExternalID)

	tests := []struct {
		name       string
		method     string
		endpoint   string
		input      interface{}
		wantStatus int
	}{
		{
			name:       "Get Orgs - No Subscription",
			method:     http.MethodGet,
			endpoint:   "/" + common.OrganizationsEndpoint,
			wantStatus: http.StatusPaymentRequired,
		},
		{
			name:       "Create Org - No Subscription",
			method:     http.MethodPost,
			endpoint:   "/" + common.OrgEndpoint,
			input:      &apiOrgInput{Name: "New Org"},
			wantStatus: http.StatusPaymentRequired,
		},
		{
			name:       "Update Org - No Subscription",
			method:     http.MethodPut,
			endpoint:   "/" + common.OrgEndpoint,
			input:      &apiOrgInput{ID: server.IDHasher.Encrypt(int(org.ID)), Name: "Updated"},
			wantStatus: http.StatusPaymentRequired,
		},
		{
			name:       "Delete Org - No Subscription",
			method:     http.MethodDelete,
			endpoint:   "/" + common.OrgEndpoint,
			input:      &apiOrgInput{ID: server.IDHasher.Encrypt(int(org.ID))},
			wantStatus: http.StatusPaymentRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := apiRequestSuite(ctx, tt.input, tt.method, tt.endpoint, apiKeyStr)
			if err != nil {
				t.Fatal(err)
			}

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

func TestAPIDeleteOrgEmptyID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	input := &apiOrgInput{ID: ""}

	_, meta, err := requestResponseAPISuite[APIResponse](ctx, input, http.MethodDelete, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Code != common.StatusOrgIDEmptyError {
		t.Fatalf("expected code %v, got %v", common.StatusOrgIDEmptyError, meta.Code)
	}
}

func TestAPIUpdateOrgSoftDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Soft delete the org
	if _, err := server.BusinessDB.Impl().SoftDeleteOrganization(ctx, org, user); err != nil {
		t.Fatalf("Failed to soft delete org: %v", err)
	}

	input := &apiOrgInput{
		ID:   server.IDHasher.Encrypt(int(org.ID)),
		Name: "Updated Name",
	}

	_, meta, err := requestResponseAPISuite[json.RawMessage](ctx, input, http.MethodPut, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	// Should fail because org is soft-deleted
	if meta.Code != common.StatusOrgNotFoundError {
		t.Errorf("Expected StatusOrgNotFoundError, got %v", meta.Code)
	}
}

func TestAPIDeleteOrgSoftDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Soft delete the org
	if _, err := server.BusinessDB.Impl().SoftDeleteOrganization(ctx, org, user); err != nil {
		t.Fatalf("Failed to soft delete org: %v", err)
	}

	input := &apiOrgInput{
		ID: server.IDHasher.Encrypt(int(org.ID)),
	}

	_, meta, err := requestResponseAPISuite[json.RawMessage](ctx, input, http.MethodDelete, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	// Should fail because org is already soft-deleted
	if meta.Code != common.StatusOrgNotFoundError {
		t.Errorf("Expected StatusOrgNotFoundError, got %v", meta.Code)
	}
}

func TestAPIUpdateOrgNotOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	// Create first user with org
	_, org, _, err := setupAPISuite(ctx, t.Name()+"_owner")
	if err != nil {
		t.Fatal(err)
	}

	// Create another user with their own API key
	_, _, apiKey2, err := setupAPISuite(ctx, t.Name()+"_other")
	if err != nil {
		t.Fatal(err)
	}

	// Try to update org1 with user2's API key
	input := &apiOrgInput{
		ID:   server.IDHasher.Encrypt(int(org.ID)),
		Name: "Malicious Update",
	}

	_, meta, err := requestResponseAPISuite[json.RawMessage](ctx, input, http.MethodPut, "/"+common.OrgEndpoint, apiKey2)
	if err != nil {
		t.Fatal(err)
	}

	// Should fail because user2 is not the owner
	if meta.Code != common.StatusOrgPermissionsError {
		t.Errorf("Expected StatusOrgPermissionsError, got %v", meta.Code)
	}
}

func TestAPIDeleteOrgNotOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	// Create first user with org
	_, org, _, err := setupAPISuite(ctx, t.Name()+"_owner")
	if err != nil {
		t.Fatal(err)
	}

	// Create another user with their own API key
	_, _, apiKey2, err := setupAPISuite(ctx, t.Name()+"_other")
	if err != nil {
		t.Fatal(err)
	}

	// Try to delete org1 with user2's API key
	input := &apiOrgInput{
		ID: server.IDHasher.Encrypt(int(org.ID)),
	}

	_, meta, err := requestResponseAPISuite[json.RawMessage](ctx, input, http.MethodDelete, "/"+common.OrgEndpoint, apiKey2)
	if err != nil {
		t.Fatal(err)
	}

	// Should fail because user2 is not the owner
	if meta.Code != common.StatusOrgPermissionsError {
		t.Errorf("Expected StatusOrgPermissionsError, got %v", meta.Code)
	}
}
