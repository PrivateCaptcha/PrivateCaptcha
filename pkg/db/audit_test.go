package db

import (
	"context"
	"testing"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func TestParseAuditLogPayloadsValidOldAndNew(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	log := &dbgen.AuditLog{
		OldValue: []byte(`{"name":"old-user","email":"old@test.com"}`),
		NewValue: []byte(`{"name":"new-user","email":"new@test.com"}`),
	}

	oldUser, newUser, err := ParseAuditLogPayloads[AuditLogUser](ctx, log)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if oldUser == nil || oldUser.Name != "old-user" {
		t.Errorf("Expected old user with name 'old-user', got %+v", oldUser)
	}

	if newUser == nil || newUser.Name != "new-user" {
		t.Errorf("Expected new user with name 'new-user', got %+v", newUser)
	}
}

func TestParseAuditLogPayloadsOnlyOldValue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	log := &dbgen.AuditLog{
		OldValue: []byte(`{"name":"deleted-user"}`),
		NewValue: nil,
	}

	oldUser, newUser, err := ParseAuditLogPayloads[AuditLogUser](ctx, log)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if oldUser == nil || oldUser.Name != "deleted-user" {
		t.Errorf("Expected old user with name 'deleted-user', got %+v", oldUser)
	}

	if newUser != nil {
		t.Errorf("Expected nil new user, got %+v", newUser)
	}
}

func TestParseAuditLogPayloadsOnlyNewValue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	log := &dbgen.AuditLog{
		OldValue: nil,
		NewValue: []byte(`{"name":"created-user"}`),
	}

	oldUser, newUser, err := ParseAuditLogPayloads[AuditLogUser](ctx, log)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if oldUser != nil {
		t.Errorf("Expected nil old user, got %+v", oldUser)
	}

	if newUser == nil || newUser.Name != "created-user" {
		t.Errorf("Expected new user with name 'created-user', got %+v", newUser)
	}
}

func TestParseAuditLogPayloadsEmptyValues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	log := &dbgen.AuditLog{
		OldValue: nil,
		NewValue: nil,
	}

	oldUser, newUser, err := ParseAuditLogPayloads[AuditLogUser](ctx, log)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if oldUser != nil {
		t.Errorf("Expected nil old user, got %+v", oldUser)
	}

	if newUser != nil {
		t.Errorf("Expected nil new user, got %+v", newUser)
	}
}

func TestParseAuditLogPayloadsInvalidOldValue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	log := &dbgen.AuditLog{
		OldValue: []byte(`{invalid json}`),
		NewValue: []byte(`{"name":"valid"}`),
	}

	_, _, err := ParseAuditLogPayloads[AuditLogUser](ctx, log)
	if err == nil {
		t.Error("Expected error for invalid old value JSON")
	}
}

func TestParseAuditLogPayloadsInvalidNewValue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	log := &dbgen.AuditLog{
		OldValue: []byte(`{"name":"valid"}`),
		NewValue: []byte(`{invalid json}`),
	}

	_, _, err := ParseAuditLogPayloads[AuditLogUser](ctx, log)
	if err == nil {
		t.Error("Expected error for invalid new value JSON")
	}
}

func TestParseAuditLogPayloadsProperty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	log := &dbgen.AuditLog{
		OldValue: []byte(`{"name":"old-prop","domain":"old.com","level":1}`),
		NewValue: []byte(`{"name":"new-prop","domain":"new.com","level":2}`),
	}

	oldProp, newProp, err := ParseAuditLogPayloads[AuditLogProperty](ctx, log)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if oldProp == nil || oldProp.Name != "old-prop" {
		t.Errorf("Expected old property with name 'old-prop', got %+v", oldProp)
	}

	if newProp == nil || newProp.Name != "new-prop" {
		t.Errorf("Expected new property with name 'new-prop', got %+v", newProp)
	}
}

func TestParseAuditLogPayloadsSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	log := &dbgen.AuditLog{
		OldValue: []byte(`{"source":"internal","status":"trialing"}`),
		NewValue: []byte(`{"source":"external","status":"active"}`),
	}

	oldSub, newSub, err := ParseAuditLogPayloads[AuditLogSubscription](ctx, log)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if oldSub == nil || oldSub.Source != "internal" {
		t.Errorf("Expected old subscription with source 'internal', got %+v", oldSub)
	}

	if newSub == nil || newSub.Source != "external" {
		t.Errorf("Expected new subscription with source 'external', got %+v", newSub)
	}
}
