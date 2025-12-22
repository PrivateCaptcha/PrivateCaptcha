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
	srv := http.NewServeMux()
	s.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

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
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

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

	if _, err := s.BusinessDB.Impl().SoftDeleteOrganization(ctx, baseOrg, user); err != nil {
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
		ID: s.IDHasher.Encrypt(int(org.ID)),
	}

	_, meta, err := requestResponseAPISuite[json.RawMessage](ctx, input, http.MethodDelete, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	if _, err := s.BusinessDB.Impl().RetrieveUserOrganization(t.Context(), user, org.ID); (err != db.ErrSoftDeleted) && (err != db.ErrNegativeCacheHit) {
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
		ID:   s.IDHasher.Encrypt(int(org.ID)),
		Name: "Org Update " + xid.New().String(),
	}

	_, meta, err := requestResponseAPISuite[json.RawMessage](ctx, input, http.MethodPut, "/"+common.OrgEndpoint, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}

	org, err = s.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, org.ID)
	if err != nil {
		t.Fatalf("Unexpected error when retrieving org: %v", err)
	}

	if org.Name != input.Name {
		t.Errorf("Org name was not updated. expected (%v), actual (%v)", input.Name, org.Name)
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
		fmt.Sprintf("/%s/%s", common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID))),
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
		ID: s.IDHasher.Encrypt(int(org.ID)),
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
		ID:   s.IDHasher.Encrypt(int(org.ID)),
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
		ID: s.IDHasher.Encrypt(int(org2.ID)),
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
		ID:   s.IDHasher.Encrypt(int(org2.ID)),
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
