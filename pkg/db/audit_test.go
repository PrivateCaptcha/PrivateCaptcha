package db

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type auditCaptureQuerier struct {
	*QuerierStub
	batch []*dbgen.CreateAuditLogsParams
}

func (q *auditCaptureQuerier) CreateAuditLogs(_ context.Context, batch []*dbgen.CreateAuditLogsParams) (int64, error) {
	q.batch = batch
	return int64(len(batch)), nil
}

func TestAuditLogPersistsSessionHashWithoutSessionID(t *testing.T) {
	const sid = "raw-session-bearer"
	hash := common.HashSessionID(sid)
	querier := &auditCaptureQuerier{QuerierStub: &QuerierStub{}}
	auditLog := NewAuditLog(querier, 1)
	ctx := context.WithValue(t.Context(), common.SessionHashContextKey, hash)
	ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, netip.MustParseAddr("192.0.2.1"))
	event := &common.AuditLogEvent{UserID: 1, Action: common.AuditLogActionLogin}

	auditLog.RecordEvent(ctx, event, common.AuditLogSourcePortal)
	queued := <-auditLog.persistChan
	if queued.SessionHash != hash {
		t.Fatalf("queued session hash = %q, want %q", queued.SessionHash.String(), hash.String())
	}
	if err := auditLog.PersistAuditLog(t.Context(), []*common.AuditLogEvent{queued}); err != nil {
		t.Fatal(err)
	}
	if len(querier.batch) != 1 {
		t.Fatalf("persisted batch length = %d, want 1", len(querier.batch))
	}
	if got := querier.batch[0].SessionID; got != hash.String() {
		t.Fatalf("persisted session correlation = %q, want %q", got, hash.String())
	}
	if strings.Contains(querier.batch[0].SessionID, sid) {
		t.Fatalf("persisted audit record contains raw session ID %q", sid)
	}
}

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
