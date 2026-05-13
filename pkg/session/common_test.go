package session

import (
	"context"
	"testing"
	"time"
)

type stubStore struct{}

func (s *stubStore) Start(ctx context.Context, interval time.Duration) {}
func (s *stubStore) Init(ctx context.Context, session *Session) error  { return nil }
func (s *stubStore) Read(ctx context.Context, sid string, skipCache bool) (*Session, error) {
	return nil, ErrSessionMissing
}
func (s *stubStore) Update(ctx context.Context, session *Session) error { return nil }
func (s *stubStore) Destroy(ctx context.Context, sid string) error      { return nil }

func TestSessionKeyString(t *testing.T) {
	sessionKeys := []SessionKey{
		KeyLoginStep,
		KeyUserID,
		KeyUserEmail,
		KeyTwoFactorCode,
		KeyUserName,
		KeyPersistent,
		KeyNotificationID,
		KeyReturnURL,
		KeyTwoFactorCodeTimestamp,
		KeyOrgInviteID,
	}

	expectedStrings := []string{
		"LoginStep",
		"UserID",
		"UserEmail",
		"TwoFactorCode",
		"UserName",
		"Persistent",
		"NotificationID",
		"ReturnURL",
		"TwoFactorCodeTimestamp",
		"OrgInviteID",
	}

	for i, key := range sessionKeys {
		t.Run(expectedStrings[i], func(t *testing.T) {
			str := key.String()
			if str != expectedStrings[i] {
				t.Errorf("Expected %s, got %s", expectedStrings[i], str)
			}
		})
	}
}

func TestSessionKeyStringUnknown(t *testing.T) {
	unknown := SessionKey(9999)
	str := unknown.String()
	if str != "SessionKey" {
		t.Errorf("Unknown key should return 'SessionKey', got %s", str)
	}
}

func TestSessionDataMerge(t *testing.T) {
	sd1 := NewSessionData("session1")
	sd2 := NewSessionData("session2")

	sd1.set(KeyUserID, 123)
	sd2.set(KeyUserID, 456)
	sd2.set(KeyUserEmail, "test@example.com")

	sd1.Merge(sd2, false)

	if val, _ := sd1.get(KeyUserID); val != 123 {
		t.Errorf("Existing key should not be overwritten, got %v", val)
	}

	if val, ok := sd1.get(KeyUserEmail); !ok || val != "test@example.com" {
		t.Errorf("New key from source should be added, got %v, %v", val, ok)
	}
}

func TestSessionDataMergeSameIDs(t *testing.T) {
	sd1 := NewSessionData("aaa")
	sd2 := NewSessionData("zzz")

	sd1.set(KeyUserName, "name1")
	sd2.set(KeyPersistent, true)

	sd1.Merge(sd2, false)

	if val, _ := sd1.get(KeyUserName); val != "name1" {
		t.Errorf("Existing key should be preserved")
	}

	if val, ok := sd1.get(KeyPersistent); !ok || val != true {
		t.Errorf("New key should be added from merge")
	}
}

func TestSessionDataMergeEmpty(t *testing.T) {
	sd1 := NewSessionData("session1")
	sd2 := NewSessionData("session2")

	sd1.set(KeyUserID, 123)

	sd1.Merge(sd2, false)

	if sd1.Size() != 1 {
		t.Errorf("Size should remain 1 after merging empty session, got %d", sd1.Size())
	}
}

func TestSessionMerge(t *testing.T) {
	sd1 := NewSessionData("session1")
	sd2 := NewSessionData("session2")

	store := &stubStore{}
	s1 := NewSession(sd1, store)
	s2 := NewSession(sd2, store)

	sd1.set(KeyUserID, 100)
	sd2.set(KeyUserName, "testuser")

	s1.Merge(s2)

	if val, _ := s1.Data().get(KeyUserID); val != 100 {
		t.Errorf("Existing key should be preserved")
	}

	if val, ok := s1.Data().get(KeyUserName); !ok || val != "testuser" {
		t.Errorf("New key should be added from merge")
	}
}

func TestSessionDataHas(t *testing.T) {
	sd := NewSessionData("test")

	if sd.Has(KeyUserID) {
		t.Error("Should not have KeyUserID initially")
	}

	sd.set(KeyUserID, 123)

	if !sd.Has(KeyUserID) {
		t.Error("Should have KeyUserID after setting")
	}
}

func TestSessionDataMarshalling(t *testing.T) {
	sd := NewSessionData("test")
	sd.set(KeyUserID, 123)
	sd.set(KeyUserEmail, "test@example.com")

	data, err := sd.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	sd2 := NewSessionData("test2")
	if err := sd2.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	if val, _ := sd2.get(KeyUserID); val != 123 {
		t.Errorf("Expected UserID 123, got %v", val)
	}

	if val, _ := sd2.get(KeyUserEmail); val != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %v", val)
	}
}

func TestSessionGobEncoding(t *testing.T) {
	sd := NewSessionData("test")
	sd.set(KeyUserID, 123)
	sd.set(KeyUserEmail, "test@example.com")

	data, err := sd.GobEncode()
	if err != nil {
		t.Fatalf("GobEncode failed: %v", err)
	}

	sd2 := NewSessionData("test2")
	if err := sd2.GobDecode(data); err != nil {
		t.Fatalf("GobDecode failed: %v", err)
	}

	if sd2.ID() != "test" {
		t.Errorf("Session ID was not serialized. got %v", sd2.ID())
	}

	if val, _ := sd2.get(KeyUserID); val != 123 {
		t.Errorf("Expected UserID 123, got %v", val)
	}

	if val, _ := sd2.get(KeyUserEmail); val != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %v", val)
	}
}

func TestSessionDataMergeWithOverwrite(t *testing.T) {
	sd1 := NewSessionData("session1")
	sd2 := NewSessionData("session2")

	sd1.set(KeyUserID, 123)
	sd2.set(KeyUserID, 456)
	sd2.set(KeyUserEmail, "new@example.com")

	sd1.Merge(sd2, true)

	if val, _ := sd1.get(KeyUserID); val != 456 {
		t.Errorf("Existing key should be overwritten, got %v", val)
	}

	if val, ok := sd1.get(KeyUserEmail); !ok || val != "new@example.com" {
		t.Errorf("New key should be added, got %v, %v", val, ok)
	}
}

func TestSessionRefresh(t *testing.T) {
	sd1 := NewSessionData("session1")
	sd2 := NewSessionData("session2")

	store := &stubStore{}
	s1 := NewSession(sd1, store)
	s2 := NewSession(sd2, store)

	sd1.set(KeyUserID, 100)
	sd2.set(KeyUserID, 200)
	sd2.set(KeyUserName, "newuser")

	s1.Refresh(s2)

	if val, _ := s1.Data().get(KeyUserID); val != 200 {
		t.Errorf("Existing key should be overwritten, got %v", val)
	}

	if val, ok := s1.Data().get(KeyUserName); !ok || val != "newuser" {
		t.Errorf("New key should be added, got %v, %v", val, ok)
	}
}
