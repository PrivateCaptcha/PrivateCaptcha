package portal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5"
)

func waitForPersistedSession(ctx context.Context, store session.Store, sid string) (*session.Session, error) {
	var sess *session.Session
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		time.Sleep(400 * time.Millisecond)
		sess, err = store.Read(ctx, sid, true)
		if err == nil {
			return sess, nil
		}
	}
	return nil, err
}

func setupSessionSuite(ctx context.Context, manager *session.Manager, t *testing.T) (*session.Session, *http.Cookie) {
	t.Helper()

	req := httptest.NewRequest("GET", "/settings", nil)
	w := httptest.NewRecorder()

	sess := manager.SessionStart(w, req)
	if sess == nil {
		t.Fatal("session was not started")
	}
	if err := sess.Set(ctx, session.KeyUserName, t.Name()); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(ctx, session.KeyUserID, int32(1)); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(ctx, session.KeyLoginStep, loginStepCompleted); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	payload, err := sess.Data().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	row, err := dbgen.New(store.Pool).CreateSession(ctx, &dbgen.CreateSessionParams{
		SessionID: sess.ID(),
		State:     dbgen.SessionStateAuthenticated,
		UserID:    db.Int(1),
		Data:      payload,
		ExpiresAt: db.Timestampz(time.Now().Add(manager.MaxLifetime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	sess.Data().SetAuthority(session.StateAuthenticated, row.Version, row.ExpiresAt.Time, time.Now())

	resp1 := w.Result()
	cookie := responseCookieForTest(t, resp1, manager.CookieName)

	renewReq := httptest.NewRequest(http.MethodPost, "/twofactor", nil)
	renewReq.AddCookie(cookie)
	renewW := httptest.NewRecorder()
	sess = manager.SessionRenew(renewW, renewReq, sess)
	cookie = responseCookieForTest(t, renewW.Result(), manager.CookieName)

	persisted, err := waitForPersistedSession(ctx, manager.Store, sess.ID())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Data().Size() == 0 {
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
		MaxLifetime: sessionStore.TTL(),
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
		MaxLifetime: sessionStore.TTL(),
	}

	manager.Init("test", "/", 400*time.Millisecond)
	defer sessionStore.Shutdown()

	ctx := common.TraceContext(t.Context(), t.Name())

	sess1, cookie := setupSessionSuite(ctx, manager, t)

	req2 := httptest.NewRequest("GET", "/support", nil)
	req2.AddCookie(cookie)
	if _, err := manager.SessionDestroy(httptest.NewRecorder(), req2); err != nil {
		t.Fatal(err)
	}

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

func TestSessionRenewRejectsRemoteRevocation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	sessionStore := db.NewSessionStore(store, session.KeyPersistent, monitoring.NewStub())
	defer sessionStore.Shutdown()
	predecessor := session.NewSession(session.NewSessionData(t.Name()+"-old"), sessionStore)
	if err := sessionStore.Init(ctx, predecessor); err != nil {
		t.Fatal(err)
	}
	if err := predecessor.Set(ctx, session.KeyUserID, int32(1)); err != nil {
		t.Fatal(err)
	}
	if err := predecessor.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.Create(ctx, predecessor); err != nil {
		t.Fatal(err)
	}
	queries := dbgen.New(store.Pool)
	if _, err := queries.RevokeSession(ctx, predecessor.ID()); err != nil {
		t.Fatal(err)
	}

	successor := session.NewSession(session.NewSessionData(t.Name()+"-new"), sessionStore)
	if err := sessionStore.Renew(ctx, predecessor, successor, func() { successor.Merge(predecessor) }); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("Renew error = %v, want session missing", err)
	}
	if _, err := queries.GetSessionByID(ctx, successor.ID()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("successor lookup error = %v, want no rows", err)
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
		MaxLifetime: sessionStore.TTL(),
	}
	manager.Init("test", "/", 400*time.Millisecond)
	defer sessionStore.Shutdown()

	req1 := httptest.NewRequest("GET", "/support", nil)
	w1 := httptest.NewRecorder()
	// Step 1: Create session with stale values (Node A's cache)
	sess := manager.SessionStart(w1, req1)
	if err := sess.Set(ctx, session.KeyLoginStep, loginStepSignInVerify); err != nil { // STALE
		t.Fatal(err)
	}
	if err := sess.Set(ctx, session.KeyUserEmail, "stale@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := sess.Set(ctx, session.KeyTwoFactorCode, 123456); err != nil {
		t.Fatal(err)
	}
	if err := sess.Persist(ctx); err != nil {
		t.Fatal(err)
	}

	resp1 := w1.Result()
	cookie := responseCookieForTest(t, resp1, manager.CookieName)

	// Step 2: Wait for session to be persisted to DB
	persisted, err := waitForPersistedSession(ctx, manager.Store, sess.ID())
	if err != nil {
		t.Fatal(err)
	}

	// Step 3: Simulate Node B completing login - update DB directly
	freshCache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	freshStore := db.NewSessionStore(db.NewBusinessEx(store.Pool, freshCache), session.KeyPersistent, monitoring.NewStub())
	freshSess := session.NewSession(session.NewSessionData(sess.ID()), freshStore)
	if err := freshStore.Init(ctx, freshSess); err != nil {
		t.Fatal(err)
	}
	if err := freshSess.Set(ctx, session.KeyLoginStep, loginStepCompleted); err != nil { // FRESH
		t.Fatal(err)
	}
	if err := freshSess.Set(ctx, session.KeyUserEmail, "fresh@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := freshSess.Set(ctx, session.KeyPersistent, true); err != nil {
		t.Fatal(err)
	}
	freshData, err := freshSess.Data().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	version, _ := persisted.Data().Persistence()
	updated, err := dbgen.New(store.Pool).UpdateSessionDataCAS(ctx, &dbgen.UpdateSessionDataCASParams{
		SessionIds:       []string{sess.ID()},
		ExpectedVersions: []int32{version},
		Payloads:         [][]byte{freshData},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 {
		t.Fatal("authoritative session update was rejected")
	}

	req2 := httptest.NewRequest("GET", "/settings", nil)
	w2 := httptest.NewRecorder()
	req2.AddCookie(cookie)
	// Step 4: Node A reads from its local cache (has stale values)
	staleSess := manager.SessionStart(w2, req2) // Returns cached stale values

	// Step 5: Attempt to recover from DB
	manager.RecoverSessionBlocking(ctx, staleSess)

	// Step 6: Assert values were updated
	step, _ := staleSess.Get(ctx, session.KeyLoginStep).(int)
	if step != loginStepCompleted {
		t.Errorf("KeyLoginStep not updated. Expected %d, got %d", loginStepCompleted, step)
	}

	email, _ := staleSess.Get(ctx, session.KeyUserEmail).(string)
	if email != "fresh@example.com" {
		t.Errorf("KeyUserEmail not updated. Expected %s, got %s", "fresh@example.com", email)
	}
	if staleSess.Data().Has(session.KeyTwoFactorCode) {
		t.Error("stale-only session field was not removed")
	}
}
