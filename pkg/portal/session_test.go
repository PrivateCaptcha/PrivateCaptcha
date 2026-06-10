package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func setupSessionSuite(ctx context.Context, manager *session.Manager, t *testing.T) (*session.Session, *http.Cookie) {
	t.Helper()

	req := httptest.NewRequest("GET", "/settings", nil)
	w := httptest.NewRecorder()

	sess := manager.SessionStart(w, req)
	if sess == nil {
		t.Fatal("session was not started")
	}
	sess.Set(ctx, session.KeyUserName, t.Name())
	sess.Set(ctx, session.KeyPersistent, true)

	resp1 := w.Result()
	idx := slices.IndexFunc(resp1.Cookies(), func(c *http.Cookie) bool { return c.Name == manager.CookieName })
	if idx == -1 {
		t.Error("cannot find session cookie in response")
	}
	cookie := resp1.Cookies()[idx]

	var data []byte
	var err error

	for attempt := 0; attempt < 5; attempt++ {
		time.Sleep(400 * time.Millisecond)

		data, err = store.Impl().RetrieveFromCache(ctx, "session/"+sess.ID())
		if err == nil {
			break
		}
	}

	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Errorf("Empty data was saved to DB")
	}

	return sess, cookie
}

func TestPersistentSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	sessionStore := db.NewSessionStore(store, session.KeyPersistent, monitoring.NewStub())

	manager := &session.Manager{
		CookieName:  "pcsid",
		Store:       sessionStore,
		MaxLifetime: 10 * time.Minute,
	}

	manager.Init("test", "/", 400*time.Millisecond)
	defer sessionStore.Shutdown()

	ctx := common.TraceContext(t.Context(), t.Name())

	sess1, cookie := setupSessionSuite(ctx, manager, t)

	if found := cache.Delete(ctx, db.SessionCacheKey(sess1.ID())); !found {
		t.Fatal("Didn't find cached session to delete")
	}

	req2 := httptest.NewRequest("GET", "/support", nil)
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()

	sess2 := manager.SessionStart(w2, req2)

	if sess1.ID() != sess2.ID() {
		t.Errorf("New session ID (%v) is different from original (%v)", sess2.ID(), sess1.ID())
	}

	if name, ok := sess2.Get(ctx, session.KeyUserName).(string); !ok || (name != t.Name()) {
		t.Errorf("Session field is not serialized or present in session")
	}
}

func TestDeleteSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	sessionStore := db.NewSessionStore(store, session.KeyPersistent, monitoring.NewStub())

	manager := &session.Manager{
		CookieName:  "pcsid",
		Store:       sessionStore,
		MaxLifetime: 10 * time.Minute,
	}

	manager.Init("test", "/", 400*time.Millisecond)
	defer sessionStore.Shutdown()

	ctx := common.TraceContext(t.Context(), t.Name())

	sess1, cookie := setupSessionSuite(ctx, manager, t)

	req2 := httptest.NewRequest("GET", "/support", nil)
	req2.AddCookie(cookie)
	manager.SessionDestroy(httptest.NewRecorder(), req2)

	req3 := httptest.NewRequest("GET", "/about", nil)
	req3.AddCookie(cookie)
	w3 := httptest.NewRecorder()
	sess2 := manager.SessionStart(w3, req3)

	if sess1.ID() == sess2.ID() {
		t.Errorf("Destroyed session ID (%v) was reused", sess2.ID())
	}

	if name, ok := sess2.Get(ctx, session.KeyUserName).(string); ok {
		t.Errorf("Session field (%v) should not be serialized or present in session", name)
	}
}

func TestSessionRecoveryOverwritesStaleValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	sessionStore := db.NewSessionStore(store, session.KeyPersistent, monitoring.NewStub())
	ctx := common.TraceContext(t.Context(), t.Name())

	manager := &session.Manager{
		CookieName:  "pcsid",
		Store:       sessionStore,
		MaxLifetime: 10 * time.Minute,
	}
	manager.Init("test", "/", 400*time.Millisecond)
	defer sessionStore.Shutdown()

	req1 := httptest.NewRequest("GET", "/support", nil)
	w1 := httptest.NewRecorder()
	// Step 1: Create session with stale values (Node A's cache)
	sess := manager.SessionStart(w1, req1)
	sess.Set(ctx, session.KeyLoginStep, loginStepSignInVerify) // STALE
	sess.Set(ctx, session.KeyUserEmail, "stale@example.com")
	sess.Set(ctx, session.KeyPersistent, true)

	resp1 := w1.Result()
	idx := slices.IndexFunc(resp1.Cookies(), func(c *http.Cookie) bool { return c.Name == manager.CookieName })
	if idx == -1 {
		t.Error("cannot find session cookie in response")
	}
	cookie := resp1.Cookies()[idx]

	// Step 2: Wait for session to be persisted to DB
	var err error

	for attempt := 0; attempt < 5; attempt++ {
		time.Sleep(400 * time.Millisecond)

		_, err = store.Impl().RetrieveFromCache(ctx, "session/"+sess.ID())
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}

	// Step 3: Simulate Node B completing login - update DB directly
	freshSess := session.NewSession(session.NewSessionData(sess.ID()), sessionStore)
	freshSess.Set(ctx, session.KeyLoginStep, loginStepCompleted) // FRESH
	freshSess.Set(ctx, session.KeyUserEmail, "fresh@example.com")
	freshSess.Set(ctx, session.KeyPersistent, true)
	freshData, _ := freshSess.Data().MarshalBinary()
	_ = store.Impl().StoreInCache(ctx, db.SessionCacheKey(sess.ID()).String(), freshData, 3*time.Hour)

	req2 := httptest.NewRequest("GET", "/settings", nil)
	w2 := httptest.NewRecorder()
	req2.AddCookie(cookie)
	// Step 4: Node A reads from its local cache (has stale values)
	staleSess := manager.SessionStart(w2, req2) // Returns cached stale values

	// Step 5: Attempt to recover from DB
	manager.RecoverSession(ctx, staleSess)

	// Step 6: Assert values were updated
	step, _ := staleSess.Get(ctx, session.KeyLoginStep).(int)
	if step != loginStepCompleted {
		t.Errorf("KeyLoginStep not updated. Expected %d, got %d", loginStepCompleted, step)
	}

	email, _ := staleSess.Get(ctx, session.KeyUserEmail).(string)
	if email != "fresh@example.com" {
		t.Errorf("KeyUserEmail not updated. Expected %s, got %s", "fresh@example.com", email)
	}
}
