package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
func (s *stubStore) Renew(ctx context.Context, oldSID string, session *Session) error {
	return nil
}
func (s *stubStore) Destroy(ctx context.Context, sid string) error { return nil }

type memoryStore struct {
	sessions map[string]*Session
}

func newMemoryStore() *memoryStore {
	return &memoryStore{sessions: make(map[string]*Session)}
}

func (s *memoryStore) Start(ctx context.Context, interval time.Duration) {}
func (s *memoryStore) Init(ctx context.Context, session *Session) error {
	s.sessions[session.ID()] = session
	return nil
}
func (s *memoryStore) Read(ctx context.Context, sid string, skipCache bool) (*Session, error) {
	sess, ok := s.sessions[sid]
	if !ok {
		return nil, ErrSessionMissing
	}
	return sess, nil
}
func (s *memoryStore) Update(ctx context.Context, session *Session) error {
	s.sessions[session.ID()] = session
	return nil
}
func (s *memoryStore) Renew(ctx context.Context, oldSID string, session *Session) error {
	if err := s.Init(ctx, session); err != nil {
		return err
	}
	return s.Destroy(ctx, oldSID)
}
func (s *memoryStore) Destroy(ctx context.Context, sid string) error {
	delete(s.sessions, sid)
	return nil
}

type failingRenewStore struct {
	*memoryStore
}

func (s *failingRenewStore) Renew(ctx context.Context, oldSID string, session *Session) error {
	return errors.New("renew failed")
}

func TestSessionStartRejectsUnknownCookieID(t *testing.T) {
	store := newMemoryStore()
	manager := &Manager{
		CookieName:  "pcsid",
		Store:       store,
		MaxLifetime: 10 * time.Minute,
		Path:        "/",
	}

	attackerID := "attacker-known-session"
	req := httptest.NewRequest(http.MethodGet, "/portal/login", nil)
	req.AddCookie(&http.Cookie{Name: manager.CookieName, Value: url.QueryEscape(attackerID)})
	w := httptest.NewRecorder()

	sess := manager.SessionStart(w, req)

	if sess.ID() == attackerID {
		t.Fatal("unknown client supplied session ID was reused")
	}
	if _, ok := store.sessions[attackerID]; ok {
		t.Fatal("unknown client supplied session ID was initialized")
	}
	if _, ok := store.sessions[sess.ID()]; !ok {
		t.Fatal("fresh session ID was not initialized")
	}

	resp := w.Result()
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == manager.CookieName })
	if idx == -1 {
		t.Fatal("fresh session cookie was not returned")
	}
	cookie := resp.Cookies()[idx]
	if cookie.Value == url.QueryEscape(attackerID) {
		t.Fatal("fresh session cookie reused unknown client supplied value")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax, got %v", cookie.SameSite)
	}
}

func TestSessionRenewRotatesCookieAndDestroysOldSession(t *testing.T) {
	store := newMemoryStore()
	manager := &Manager{
		CookieName:  "pcsid",
		Store:       store,
		MaxLifetime: 10 * time.Minute,
		Path:        "/",
	}

	req := httptest.NewRequest(http.MethodPost, "/portal/twofactor", nil)
	w := httptest.NewRecorder()
	sess := manager.SessionStart(w, req)
	if err := sess.Set(req.Context(), KeyUserID, int32(123)); err != nil {
		t.Fatal(err)
	}

	renewW := httptest.NewRecorder()
	renewed := manager.SessionRenew(renewW, req, sess, map[SessionKey]SessionValue{
		KeyLoginStep: 2,
	}, []SessionKey{KeyUserID})

	if renewed.ID() == sess.ID() {
		t.Fatal("session ID was not rotated")
	}
	if _, ok := store.sessions[sess.ID()]; ok {
		t.Fatal("old session was not destroyed")
	}
	if _, ok := renewed.Get(req.Context(), KeyUserID).(int32); ok {
		t.Fatal("renewed session did not delete requested key")
	}
	if step, ok := renewed.Get(req.Context(), KeyLoginStep).(int); !ok || step != 2 {
		t.Fatalf("renewed session did not set requested key: %v", step)
	}

	resp := renewW.Result()
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == manager.CookieName })
	if idx == -1 {
		t.Fatal("rotated session cookie was not returned")
	}
	cookie := resp.Cookies()[idx]
	if cookie.Value == url.QueryEscape(sess.ID()) {
		t.Fatal("rotated session cookie reused old session ID")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax, got %v", cookie.SameSite)
	}
}

func TestSessionRenewFallsBackWhenStoreRenewFails(t *testing.T) {
	store := &failingRenewStore{memoryStore: newMemoryStore()}
	manager := &Manager{
		CookieName:  "pcsid",
		Store:       store,
		MaxLifetime: 10 * time.Minute,
		Path:        "/",
	}

	req := httptest.NewRequest(http.MethodPost, "/portal/twofactor", nil)
	w := httptest.NewRecorder()
	sess := manager.SessionStart(w, req)
	if err := sess.Set(req.Context(), KeyUserID, int32(123)); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(req.Context(), KeyTwoFactorCode, 456789); err != nil {
		t.Fatal(err)
	}

	renewW := httptest.NewRecorder()
	renewed := manager.SessionRenew(renewW, req, sess, map[SessionKey]SessionValue{
		KeyLoginStep: 2,
	}, []SessionKey{KeyTwoFactorCode})

	if renewed.ID() != sess.ID() {
		t.Fatal("renew failure should keep using the existing session")
	}
	if step, ok := renewed.Get(req.Context(), KeyLoginStep).(int); !ok || step != 2 {
		t.Fatalf("fallback session did not get final login step: %v", step)
	}
	if _, ok := renewed.Get(req.Context(), KeyTwoFactorCode).(int); ok {
		t.Fatal("fallback session kept 2FA code")
	}
	if len(renewW.Result().Cookies()) != 0 {
		t.Fatal("fallback renewal should not replace the session cookie")
	}
}

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

func TestSessionBool(t *testing.T) {
	store := &stubStore{}
	sess := NewSession(NewSessionData("sid"), store)
	ctx := t.Context()

	if _, ok := sess.Get(ctx, KeyVerifyRegistration).(bool); ok {
		t.Error("Get messed up return value for non-existing key")
	}

	sess.Set(ctx, KeyVerifyRegistration, true)

	if value, ok := sess.Get(ctx, KeyVerifyRegistration).(bool); !ok || !value {
		t.Error("Get messed up return value for existing key")
	}
}
