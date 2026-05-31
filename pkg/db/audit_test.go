package db

import (
	"encoding/json"
	"strings"
	"testing"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func TestNewUpdateFormAuditLogEventStoresRequestsPerMinute(t *testing.T) {
	updatedForm := &dbgen.Form{
		ID:                100,
		Name:              "contact",
		OrgID:             Int(10),
		OrgOwnerID:        Int(20),
		PropertyID:        30,
		URL:               "https://hooks.example.com/contact",
		RequestsPerMinute: 60,
		RetryRequestCount: 1,
		Method:            dbgen.FormMethodPost,
		Enabled:           true,
		Active:            true,
	}
	updateRow := &dbgen.UpdateFormRow{
		ID:                   updatedForm.ID,
		Name:                 updatedForm.Name,
		OrgID:                updatedForm.OrgID,
		OrgOwnerID:           updatedForm.OrgOwnerID,
		PropertyID:           updatedForm.PropertyID,
		URL:                  updatedForm.URL,
		RequestsPerMinute:    updatedForm.RequestsPerMinute,
		RetryRequestCount:    updatedForm.RetryRequestCount,
		Method:               updatedForm.Method,
		Enabled:              updatedForm.Enabled,
		Active:               updatedForm.Active,
		OldName:              updatedForm.Name,
		OldURL:               "https://hooks.example.com/old-contact",
		OldActive:            true,
		OldRetryRequestCount: 0,
		OldRequestsPerMinute: 30,
		OldMethod:            dbgen.FormMethodPut,
	}

	event := newUpdateFormAuditLogEvent(updatedForm, updateRow, &dbgen.Organization{Name: "Acme"}, &dbgen.User{ID: 99})
	if event == nil {
		t.Fatal("expected audit event")
	}

	oldValue, ok := event.OldValue.(*AuditLogForm)
	if !ok {
		t.Fatalf("expected old value AuditLogForm, got %T", event.OldValue)
	}
	newValue, ok := event.NewValue.(*AuditLogForm)
	if !ok {
		t.Fatalf("expected new value AuditLogForm, got %T", event.NewValue)
	}

	if oldValue.RequestsPerMinute != 30 {
		t.Fatalf("expected old requests per minute 30, got %d", oldValue.RequestsPerMinute)
	}
	if newValue.RequestsPerMinute != 60 {
		t.Fatalf("expected new requests per minute 60, got %d", newValue.RequestsPerMinute)
	}

	payload, err := json.Marshal(newValue)
	if err != nil {
		t.Fatalf("failed to marshal audit form: %v", err)
	}

	var serialized map[string]any
	if err := json.Unmarshal(payload, &serialized); err != nil {
		t.Fatalf("failed to unmarshal audit form: %v", err)
	}

	if _, ok := serialized["requests_per_minute"]; !ok {
		t.Fatalf("expected requests_per_minute field in audit payload, got %v", serialized)
	}

	rateLimitKeys := 0
	for key := range serialized {
		if strings.HasPrefix(key, "requests_per_") {
			rateLimitKeys++
		}
	}
	if rateLimitKeys != 1 {
		t.Fatalf("expected exactly one requests_per_* field in audit payload, got %v", serialized)
	}
}
