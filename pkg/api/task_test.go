package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
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
	task, err := s.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user1, time.Now().UTC().Add(24*time.Hour), t.Name())
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
	task, err := s.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user, time.Now().UTC().Add(24*time.Hour), t.Name())
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
