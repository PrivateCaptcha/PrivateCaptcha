package session

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type payloadStoreStub struct {
	update func(context.Context, string)
}

type payloadTestContextKey struct{}

func (s payloadStoreStub) UpdatePayload(ctx context.Context, sid string) {
	if s.update != nil {
		s.update(ctx, sid)
	}
}

func TestSessionAuthorityReturnsCopy(t *testing.T) {
	authority := Authority{
		State:          StateAuthenticated,
		Version:        3,
		UserID:         42,
		ChallengeKind:  ChallengeKindEmailChange,
		ChallengeEmail: "user@example.com",
		ExpiresAt:      time.Now().Add(time.Hour),
		LeaseUntil:     time.Now().Add(10 * time.Minute),
	}
	sess := NewSessionWithAuthority(authority, NewPayload("sid", payloadStoreStub{}))
	if sess.ID() != "sid" {
		t.Fatalf("Session ID = %q, want sid", sess.ID())
	}

	actual, ok := sess.Authority()
	if !ok {
		t.Fatal("authoritative session has no Authority")
	}
	actual.Version = 99
	actual.ChallengeEmail = "changed@example.com"

	unchanged, _ := sess.Authority()
	if unchanged.Version != authority.Version || unchanged.ChallengeEmail != authority.ChallengeEmail {
		t.Fatalf("caller mutated cached Authority: %+v", unchanged)
	}
}

func TestSessionStoresCorrelationHash(t *testing.T) {
	const sid = "session-secret"
	want := common.HashSessionID(sid)

	authoritative := NewSessionWithAuthority(Authority{}, NewPayload(sid, payloadStoreStub{}))
	if authoritative.Hash() != want {
		t.Fatalf("authoritative Session hash = %q, want %q", authoritative.Hash().String(), want.String())
	}

	anonymous := NewAnonymousSession(sid, payloadStoreStub{})
	if anonymous.Hash() != want {
		t.Fatalf("anonymous Session hash = %q, want %q", anonymous.Hash().String(), want.String())
	}
}

func TestPayloadReplaceRemovesMissingKeys(t *testing.T) {
	payload := NewPayload("sid", payloadStoreStub{})
	if err := payload.Set(t.Context(), KeyUserName, "old name"); err != nil {
		t.Fatal(err)
	}
	if err := payload.Set(t.Context(), KeyReturnURL, "/old"); err != nil {
		t.Fatal(err)
	}

	replacement := NewPayload("sid", payloadStoreStub{})
	if err := replacement.Set(t.Context(), KeyUserName, "new name"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := replacement.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := payload.Replace(snapshot); err != nil {
		t.Fatal(err)
	}

	if actual := payload.Get(KeyUserName); actual != "new name" {
		t.Fatalf("user name = %v, want new name", actual)
	}
	if actual := payload.Get(KeyReturnURL); actual != nil {
		t.Fatalf("removed return URL survived replacement: %v", actual)
	}
}

func TestPayloadKeyNumericValuesRemainStable(t *testing.T) {
	fixtures := []struct {
		key   SessionKey
		value SessionKey
		data  string
	}{
		{KeyUserEmail, 2, "email"},
		{KeyUserName, 4, "name"},
		{KeyNotificationID, 6, "notification"},
		{KeyReturnURL, 7, "return-url"},
		{KeyOrgInviteID, 9, "invite"},
		{KeyFirstSession, 10, "first-session"},
		{KeyAdhocNotification, 11, "adhoc-notification"},
	}
	legacy := make(map[SessionKey]SessionValue, len(fixtures))
	for _, fixture := range fixtures {
		if fixture.key != fixture.value {
			t.Fatalf("Payload key = %d, want %d", fixture.key, fixture.value)
		}
		legacy[fixture.value] = fixture.data
	}

	var snapshot bytes.Buffer
	if err := gob.NewEncoder(&snapshot).Encode(legacy); err != nil {
		t.Fatal(err)
	}
	payload := NewPayload("sid", payloadStoreStub{})
	if err := payload.Replace(snapshot.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		if actual := payload.Get(fixture.key); actual != fixture.data {
			t.Fatalf("Payload key %d = %v, want %q", fixture.key, actual, fixture.data)
		}
	}
}

func TestAuthoritativeSessionAccessorsAllowOnlyPayloadKeys(t *testing.T) {
	sess := NewSessionWithAuthority(Authority{State: StateAuthenticated, Version: 1}, NewPayload("sid", payloadStoreStub{}))
	ctx := t.Context()

	allowed := []SessionKey{KeyUserEmail, KeyUserName, KeyNotificationID, KeyReturnURL, KeyOrgInviteID, KeyFirstSession, KeyAdhocNotification}
	for _, key := range allowed {
		if err := sess.Set(ctx, key, key); err != nil {
			t.Fatalf("Payload key %v Set error: %v", key, err)
		}
		if actual := sess.Get(ctx, key); actual != key {
			t.Fatalf("Payload key %v value = %v", key, actual)
		}
		if err := sess.Delete(ctx, key); err != nil {
			t.Fatalf("Payload key %v Delete error: %v", key, err)
		}
	}

	forbidden := []SessionKey{-1, 12, 999}
	for _, key := range forbidden {
		if err := sess.Set(ctx, key, key); !errors.Is(err, ErrInvalidPayloadKey) {
			t.Fatalf("authority key %v Set error = %v, want %v", key, err, ErrInvalidPayloadKey)
		}
		if actual := sess.Get(ctx, key); actual != nil {
			t.Fatalf("authority key %v exposed through Payload getter: %v", key, actual)
		}
		if err := sess.Delete(ctx, key); !errors.Is(err, ErrInvalidPayloadKey) {
			t.Fatalf("authority key %v Delete error = %v, want %v", key, err, ErrInvalidPayloadKey)
		}
	}
}

func TestPayloadReplaceRejectsAuthorityKeys(t *testing.T) {
	payload := NewPayload("sid", payloadStoreStub{})
	if err := payload.Set(t.Context(), KeyUserName, "unchanged"); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(map[SessionKey]SessionValue{999: int32(42)}); err != nil {
		t.Fatal(err)
	}

	if err := payload.Replace(data.Bytes()); !errors.Is(err, ErrInvalidPayloadKey) {
		t.Fatalf("Replace error = %v, want %v", err, ErrInvalidPayloadKey)
	}
	if actual := payload.Get(KeyUserName); actual != "unchanged" {
		t.Fatalf("failed replacement changed Payload to %v", actual)
	}
}

func TestPayloadUpdatesStoreAfterMutation(t *testing.T) {
	var payload *Payload
	notifications := 0
	ctx := context.WithValue(t.Context(), payloadTestContextKey{}, "payload-test")
	store := &payloadStoreStub{update: func(actualCtx context.Context, sid string) {
		notifications++
		if actualCtx != ctx {
			t.Fatal("Payload store received a different context")
		}
		if sid != "sid" {
			t.Fatalf("Payload store SID = %q, want sid", sid)
		}
		if notifications == 1 && payload.Get(KeyUserName) != "name" {
			t.Fatal("Set notified before updating Payload")
		}
		if notifications == 2 && payload.Get(KeyUserName) != nil {
			t.Fatal("Delete notified before updating Payload")
		}
	}}
	payload = NewPayload("sid", store)

	if err := payload.Set(ctx, KeyUserName, "name"); err != nil {
		t.Fatal(err)
	}
	if err := payload.Delete(ctx, KeyUserName); err != nil {
		t.Fatal(err)
	}
	if notifications != 2 {
		t.Fatalf("notifications = %d, want 2", notifications)
	}
}

func TestSessionWithAuthorityRequiresPayload(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil Payload did not panic")
		}
	}()
	NewSessionWithAuthority(Authority{}, nil)
}

func TestAnonymousSessionCreatesEmptyPayload(t *testing.T) {
	sess := NewAnonymousSession("sid", payloadStoreStub{})
	if sess == nil || sess.ID() != "sid" {
		t.Fatalf("anonymous session = %+v, want SID sid", sess)
	}
	if _, ok := sess.Authority(); ok {
		t.Fatal("anonymous session unexpectedly has Authority")
	}
	if value := sess.Payload().Get(KeyUserName); value != nil {
		t.Fatalf("anonymous session Payload = %v, want empty", value)
	}
}

func TestPayloadRequiresStore(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil PayloadStore did not panic")
		}
	}()
	NewPayload("sid", nil)
}
