package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/rs/xid"
)

func TestGetAsyncTaskPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user1, _, apiKey1, err := setupAPISuite(ctx, t.Name()+"_owner")
	if err != nil {
		t.Fatal(err)
	}

	handlerID := xid.New().String()
	request := struct{}{}
	task, err := server.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user1, time.Now().UTC().Add(24*time.Hour), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	taskID := db.UUIDToString(task.ID)

	_, _, apiKey2, err := setupAPISuite(ctx, t.Name()+"_other")
	if err != nil {
		t.Fatal(err)
	}

	// api key 2 belongs to the wrong user
	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKey2)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}

	// with api key 1 it should work
	_, meta, err := requestResponseAPISuite[*apiAsyncTaskResultOutput](ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKey1)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}
}

func TestGetAsyncTaskInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	apiKey := db.UUIDToSecret(*randomUUID())
	taskID := "some-task-id"

	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestGetAsyncTaskReadOnlyKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, true /*read-only*/, false /*scope org*/)
	if err != nil {
		t.Fatal(err)
	}

	handlerID := xid.New().String()
	request := struct{}{}
	task, err := server.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user, time.Now().UTC().Add(24*time.Hour), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	taskID := db.UUIDToString(task.ID)

	// with read-only api key it still should work
	_, meta, err := requestResponseAPISuite[*apiAsyncTaskResultOutput](ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}
}

func TestGetAsyncTaskNoSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_test.CreateNewAccountForTestEx(ctx, store, t.Name(), nil)
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

	handlerID := xid.New().String()
	request := struct{}{}
	task, err := server.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user, time.Now().UTC().Add(24*time.Hour), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	taskID := db.UUIDToString(task.ID)

	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKeyStr)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("expected status %d, got %d", http.StatusPaymentRequired, resp.StatusCode)
	}
}

func TestGetAsyncTaskInvalidIDFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		taskID     string
		wantStatus int
	}{
		{
			name:       "Invalid UUID Format",
			taskID:     "not-a-valid-uuid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Empty Task ID",
			taskID:     "",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := "/" + common.AsyncTaskEndpoint + "/" + tt.taskID
			resp, err := apiRequestSuite(ctx, nil, http.MethodGet, endpoint, apiKey)
			if err != nil {
				t.Fatal(err)
			}

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}
