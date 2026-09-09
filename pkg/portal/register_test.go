package portal

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type blockingRegistrationJobs struct {
	db.UserJobs
	job common.OneOffJob
}

func (j *blockingRegistrationJobs) CheckRegistration(*session.Session, *http.Request, int32) common.OneOffJob {
	return j.job
}

type blockingRegistrationCheckJob struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (j *blockingRegistrationCheckJob) Name() string                { return "BlockingRegistrationCheck" }
func (j *blockingRegistrationCheckJob) InitialPause() time.Duration { return 0 }
func (j *blockingRegistrationCheckJob) NewParams() any              { return struct{}{} }
func (j *blockingRegistrationCheckJob) RunOnce(context.Context, any) error {
	close(j.started)
	<-j.release
	close(j.finished)
	return nil
}

type registrationJobsWithoutOnboarding struct {
	db.UserJobs
}

func (j *registrationJobsWithoutOnboarding) OnboardUser(*dbgen.User, billing.Plan) common.OneOffJob {
	return &common.StubOneOffJob{}
}

type inviteMessageSender struct {
	messages chan *email.Message
}

func (s *inviteMessageSender) SendEmail(_ context.Context, message *email.Message) error {
	s.messages <- message
	return nil
}

func registerSuite(srv *http.ServeMux, name, email, token string, cookies ...*http.Cookie) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, name)
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	return w.Result()
}

func TestPostRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	email := t.Name() + "@privatecaptcha.com"
	name := "Foo Bar"
	resp := registerSuite(srv, name, email, server.XSRF.Token(""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected login status code: %v", resp.StatusCode)
	}

	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find session cookie in response")
	}
	cookie := resp.Cookies()[idx]

	ctx := t.Context()
	code, err := portal_tests.TwoFactorCodeFromEmail(email)
	if err != nil {
		t.Fatal(err)
	}

	resp = twoFactorSuite(srv, email, server.XSRF.Token(email), code, cookie)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect response but got error: %v", err)
	}

	user, err := store.Impl().FindUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}

	orgs, err := store.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil || len(orgs) == 0 {
		t.Fatalf("Expected user to have an organization after registration, err: %v", err)
	}

	expectedPath := fmt.Sprintf("/?%s=true", common.ParamOnboarding)
	if path := location.String(); path != expectedPath {
		t.Errorf("unexpected redirect: %v, expected: %v", path, expectedPath)
	}

	if user.Email != email {
		t.Errorf("Unexpected user email")
	}
}

func TestRegisterFromEmailInviteRedirectsToInvitedOrganization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	ownerCookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	originalJobs := server.Jobs
	server.Jobs = &registrationJobsWithoutOnboarding{UserJobs: originalJobs}
	t.Cleanup(func() { server.Jobs = originalJobs })

	mailer, ok := server.Mailer.(*email.StubMailer)
	if !ok {
		t.Fatalf("mailer type = %T, want *email.StubMailer", server.Mailer)
	}
	portalMailer, ok := mailer.Mailer.(*PortalMailer)
	if !ok {
		t.Fatalf("mailer type = %T, want *PortalMailer", mailer.Mailer)
	}
	originalSender := portalMailer.Mailer
	originalPortalURL := portalMailer.PortalURL
	messageSender := &inviteMessageSender{messages: make(chan *email.Message, 1)}
	portalMailer.Mailer = messageSender
	portalMailer.PortalURL = "https://portal.example.test"
	t.Cleanup(func() {
		portalMailer.Mailer = originalSender
		portalMailer.PortalURL = originalPortalURL
	})

	invitedEmail := strings.ToLower(t.Name()) + "@privatecaptcha.com"
	form := url.Values{
		common.ParamCSRFToken: {server.XSRF.Token(strconv.Itoa(int(owner.ID)))},
		common.ParamEmail:     {invitedEmail},
	}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/members", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(ownerCookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("invite status = %d, want 200", w.Code)
	}

	var inviteMessage *email.Message
	select {
	case inviteMessage = <-messageSender.messages:
	case <-time.After(time.Second):
		t.Fatal("organization invite email was not sent")
	}
	portalMailer.Mailer = originalSender
	portalMailer.PortalURL = originalPortalURL
	if inviteMessage.EmailTo != invitedEmail {
		t.Fatalf("invite recipient = %q, want %q", inviteMessage.EmailTo, invitedEmail)
	}

	const linkPrefix = "following this link: "
	linkStart := strings.Index(inviteMessage.TextBody, linkPrefix)
	if linkStart == -1 {
		t.Fatalf("invite email does not contain registration link: %s", inviteMessage.TextBody)
	}
	inviteLink := inviteMessage.TextBody[linkStart+len(linkPrefix):]
	if linkEnd := strings.IndexByte(inviteLink, '\n'); linkEnd != -1 {
		inviteLink = inviteLink[:linkEnd]
	}
	inviteURL, err := url.Parse(strings.TrimSpace(inviteLink))
	if err != nil {
		t.Fatalf("parse invite link: %v", err)
	}
	invitePathStart := strings.Index(inviteURL.Path, "/"+common.OrgInviteEndpoint+"/")
	if invitePathStart == -1 {
		t.Fatalf("invite link path = %q, want organization invite path", inviteURL.Path)
	}
	inviteURL.Path = inviteURL.Path[invitePathStart:]

	req = httptest.NewRequest(http.MethodGet, inviteURL.RequestURI(), nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("invite registration page status = %d, want 200", w.Code)
	}
	doc := portal_tests.ParseHTML(t, w.Body)
	emailInput := doc.Find(fmt.Sprintf("input[name=%q]", common.ParamEmail))
	if value, exists := emailInput.Attr("value"); !exists || value != invitedEmail {
		t.Fatalf("registration email = %q, want %q", value, invitedEmail)
	}
	if _, exists := emailInput.Attr("readonly"); !exists {
		t.Fatal("invite registration email is not readonly")
	}
	inviteID, exists := doc.Find(fmt.Sprintf("input[name=%q]", common.ParamID)).Attr("value")
	if !exists || inviteID == "" {
		t.Fatal("invite registration form does not contain invite ID")
	}

	form = url.Values{
		common.ParamCSRFToken:      {server.XSRF.Token("")},
		common.ParamEmail:          {invitedEmail},
		common.ParamID:             {inviteID},
		common.ParamName:           {"Invited User"},
		common.ParamTerms:          {"true"},
		common.ParamPortalSolution: {"captchaSolution"},
	}
	req = httptest.NewRequest(http.MethodPost, "/"+common.RegisterEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("registration status = %d, want 200", w.Code)
	}
	registrationCookieIndex := slices.IndexFunc(w.Result().Cookies(), func(c *http.Cookie) bool {
		return c.Name == server.Sessions.CookieName
	})
	if registrationCookieIndex == -1 {
		t.Fatal("registration response does not contain a session cookie")
	}
	registrationCookie := w.Result().Cookies()[registrationCookieIndex]

	code, err := portal_tests.TwoFactorCodeFromEmail(invitedEmail)
	if err != nil {
		t.Fatal(err)
	}
	resp := twoFactorSuite(srv, invitedEmail, server.XSRF.Token(invitedEmail), code, registrationCookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("two-factor status = %d, want redirect", resp.StatusCode)
	}
	expectedPath := fmt.Sprintf("/org/%s", server.IDHasher.Encrypt(int(org.ID)))
	location, err := resp.Location()
	if err != nil || location.String() != expectedPath {
		t.Fatalf("registration redirect = (%v, %v), want %s", location, err, expectedPath)
	}
	authCookieIndex := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool {
		return c.Name == server.Sessions.CookieName
	})
	if authCookieIndex == -1 {
		t.Fatal("two-factor response does not contain an authenticated session cookie")
	}

	req = httptest.NewRequest(http.MethodGet, location.String(), nil)
	req.AddCookie(resp.Cookies()[authCookieIndex])
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("invited organization status = %d, want 200", w.Code)
	}
}

func TestPostRegisterRunsRegistrationCheckAsynchronously(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	job := &blockingRegistrationCheckJob{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	originalJobs := server.Jobs
	server.Jobs = &blockingRegistrationJobs{UserJobs: originalJobs, job: job}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(job.release) }) }
	t.Cleanup(func() {
		release()
		server.Jobs = originalJobs
	})

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	response := make(chan *http.Response, 1)
	go func() {
		response <- registerSuite(srv, "Async User", t.Name()+"@privatecaptcha.com", server.XSRF.Token(""))
	}()

	select {
	case <-job.started:
	case <-time.After(5 * time.Second):
		t.Fatal("registration check did not start")
	}
	select {
	case resp := <-response:
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("registration status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	case <-time.After(time.Second):
		release()
		<-response
		t.Fatal("registration response waited for the registration check")
	}

	release()
	select {
	case <-job.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("registration check did not finish")
	}
}

func TestPostRegisterWarmsOrgInviteCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatal(err)
	}
	invitedEmail := strings.ToLower(t.Name()) + "@privatecaptcha.com"
	var inviteID int32
	if err := store.Pool.QueryRow(ctx, `
		INSERT INTO backend.organization_users (org_id, email, level)
		VALUES ($1, $2, 'invited')
		RETURNING id`, org.ID, invitedEmail).Scan(&inviteID); err != nil {
		t.Fatal(err)
	}

	originalStore := server.Store
	freshStore := db.NewBusiness(store.Pool)
	server.Store = freshStore
	t.Cleanup(func() { server.Store = originalStore })

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	form := url.Values{
		common.ParamCSRFToken:      {server.XSRF.Token("")},
		common.ParamEmail:          {invitedEmail},
		common.ParamID:             {server.IDHasher.Encrypt(int(inviteID))},
		common.ParamName:           {"Invited User"},
		common.ParamTerms:          {"true"},
		common.ParamPortalSolution: {"captchaSolution"},
	}
	req := httptest.NewRequest(http.MethodPost, "/"+common.RegisterEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("registration status = %d, want 200", w.Code)
	}

	deadline := time.Now().Add(time.Second)
	for {
		invite, cacheErr := freshStore.Impl().GetCachedOrgInviteByID(ctx, inviteID)
		if cacheErr == nil {
			if invite.ID != inviteID {
				t.Fatalf("cached invite ID = %d, want %d", invite.ID, inviteID)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("organization invite was not cached: %v", cacheErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRegistrationInvalidCSRFDoesNotConsumeChallenge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	email := t.Name() + "@privatecaptcha.com"
	resp := registerSuite(srv, "Foo Bar", email, server.XSRF.Token(""))
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find registration session cookie")
	}
	cookie := resp.Cookies()[idx]
	code, err := portal_tests.TwoFactorCodeFromEmail(email)
	if err != nil {
		t.Fatal(err)
	}

	resp = twoFactorSuite(srv, email, server.XSRF.Token("wrong-email@example.com"), code, cookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("invalid CSRF status = %d, want redirect", resp.StatusCode)
	}
	location, err := resp.Location()
	if err != nil || location.Path != "/"+common.ExpiredEndpoint {
		t.Fatalf("invalid CSRF redirect = (%v, %v), want /%s", location, err, common.ExpiredEndpoint)
	}
	if _, err := store.Impl().FindUserByEmail(t.Context(), email); err == nil {
		t.Fatal("invalid CSRF created an account")
	}

	resp = twoFactorSuite(srv, email, server.XSRF.Token(email), code, cookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("valid CSRF retry status = %d, want redirect", resp.StatusCode)
	}
	if _, err := store.Impl().FindUserByEmail(t.Context(), email); err != nil {
		t.Fatalf("valid CSRF retry did not create an account: %v", err)
	}
}

func TestRegistrationCheckRequiresVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	staleUserSID := t.Name() + "-stale-user"
	staleRegistrationSID := t.Name() + "-stale-registration"
	clearSpammerState := func() {
		if _, err := store.Pool.Exec(context.Background(), "DELETE FROM backend.sessions WHERE challenge_email = $1", spammerEmail); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Pool.Exec(context.Background(), "DELETE FROM backend.users WHERE email = $1", spammerEmail); err != nil {
			t.Fatal(err)
		}
	}
	clearSpammerState()
	t.Cleanup(clearSpammerState)

	var staleUserID int32
	if err := store.Pool.QueryRow(t.Context(), "INSERT INTO backend.users (name, email) VALUES ($1, $2) RETURNING id", "Stale Spammer", spammerEmail).Scan(&staleUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool.Exec(t.Context(), `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at)
		VALUES ($1, 'authenticated', $2, $3, NOW() + INTERVAL '1 hour')
	`, staleUserSID, staleUserID, []byte("stale")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool.Exec(t.Context(), `
		INSERT INTO backend.sessions (
			session_id, state, data, expires_at,
			challenge_kind, challenge_code, challenge_email, challenge_expires_at
		) VALUES ($1, 'pending', $2, NOW() + INTERVAL '1 hour', 'registration', '111111', $3, NOW() + INTERVAL '15 minutes')
	`, staleRegistrationSID, []byte("stale"), spammerEmail); err != nil {
		t.Fatal(err)
	}
	clearSpammerState()
	var staleSessions int
	if err := store.Pool.QueryRow(t.Context(), "SELECT COUNT(*) FROM backend.sessions WHERE session_id IN ($1, $2)", staleUserSID, staleRegistrationSID).Scan(&staleSessions); err != nil {
		t.Fatal(err)
	}
	if staleSessions != 0 {
		t.Fatalf("stale spammer sessions = %d, want 0", staleSessions)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	resp := registerSuite(srv, "Spam User", spammerEmail, server.XSRF.Token(""))
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find registration session cookie")
	}
	cookie := resp.Cookies()[idx]
	code, err := portal_tests.TwoFactorCodeFromEmail(spammerEmail)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(cookie)
		sess, resolveErr := server.Sessions.Get(req)
		if resolveErr == nil {
			authority, ok := sess.Authority()
			if ok && authority.VerifyRegistration {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("registration screening did not update cached Authority: %v", resolveErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp = twoFactorSuite(srv, spammerEmail, server.XSRF.Token(spammerEmail), code, cookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("verification-required status = %d, want redirect", resp.StatusCode)
	}
	location, err := resp.Location()
	if err != nil || location.Path != "/"+common.AccountVerifyEndpoint {
		t.Fatalf("verification-required redirect = (%v, %v), want /%s", location, err, common.AccountVerifyEndpoint)
	}
	if _, err := store.Impl().FindUserByEmail(t.Context(), spammerEmail); err == nil {
		t.Fatal("verification-required registration created an account")
	}
}

func TestGetRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	req := httptest.NewRequest("GET", "/"+common.RegisterEndpoint, nil)
	w := httptest.NewRecorder()

	viewModel, err := server.getRegister(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != loginTemplate {
		t.Errorf("Expected view to be %s, got %s", loginTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*loginRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *loginRenderContext, got %T", viewModel.Model)
	}

	if !renderCtx.IsRegister {
		t.Error("Expected IsRegister to be true")
	}

	if len(renderCtx.Token) == 0 {
		t.Error("Expected CSRF token to be populated")
	}
}

func TestPostRegisterEmptyName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Empty name
	email := t.Name() + "@privatecaptcha.com"
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, "")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	// Should show error about name being too short
	body := w.Body.String()
	if !strings.Contains(body, "name") && !strings.Contains(body, "longer") {
		t.Error("Expected error message about name")
	}
}

func TestPostRegisterShortName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Very short name (less than 3 chars)
	email := t.Name() + "@privatecaptcha.com"
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, "AB")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "longer") {
		t.Error("Expected error message about name being too short")
	}
}

func TestPostRegisterInvalidNameChars(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Name with invalid characters
	email := t.Name() + "@privatecaptcha.com"
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, "Test@User#123")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "invalid") {
		t.Error("Expected error message about invalid name characters")
	}
}

func TestPostRegisterMalformedEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Malformed email
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, "not-an-email")
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "not valid") {
		t.Error("Expected error message about invalid email")
	}
}

func TestPostRegisterMissingTerms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// No terms accepted
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, t.Name()+"@privatecaptcha.com")
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamPortalSolution, "captchaSolution")
	// No terms

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Missing terms should redirect to bad request
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status, got %v", w.Code)
	}
}

func TestPostRegisterMissingCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// No captcha solution
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, t.Name()+"@privatecaptcha.com")
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")
	// No captcha solution

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "captcha") {
		t.Error("Expected error message about captcha")
	}
}

func TestPostRegisterPreservesInviteIDOnValidationError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	inviteID := server.IDHasher.Encrypt(42)
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, t.Name()+"@privatecaptcha.com")
	form.Add(common.ParamID, inviteID)
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")

	req := httptest.NewRequest(http.MethodPost, "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	input := portal_tests.ParseHTML(t, w.Body).Find(fmt.Sprintf("input[name=%q]", common.ParamID))
	if value, exists := input.Attr("value"); !exists || value != inviteID {
		t.Fatalf("invite ID after validation error = %q, want %q", value, inviteID)
	}
}

func TestPostRegisterRejectsInvalidInviteID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	for _, inviteID := range []string{"invalid", server.IDHasher.Encrypt(int(math.MaxInt32) + 1)} {
		form := url.Values{}
		form.Add(common.ParamID, inviteID)
		form.Add(common.ParamTerms, "true")
		req := httptest.NewRequest(http.MethodPost, "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("invalid invite ID %q status = %d, want redirect", inviteID, w.Code)
		}
	}
}

func TestPostRegisterExistingEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create an existing user first
	existingUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_existing", testPlan)
	if err != nil {
		t.Fatalf("Failed to create existing account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to register with the same email
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, existingUser.Email)
	form.Add(common.ParamName, "Another User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	// Check for specific error message constant
	if !strings.Contains(body, emailAlreadyRegisteredError) {
		t.Errorf("Expected error message '%s', got body: %s", emailAlreadyRegisteredError, body)
	}
}

func TestPostRegisterExistingEmailCaseInsensitive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create an existing user first
	existingUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_EXIsTiNG", testPlan)
	if err != nil {
		t.Fatalf("Failed to create existing account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to register with the same email but different case
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, strings.ToUpper(existingUser.Email))
	form.Add(common.ParamName, "Another User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	// Check for specific error message constant
	if !strings.Contains(body, emailAlreadyRegisteredError) {
		t.Errorf("Expected error message '%s', got body: %s", emailAlreadyRegisteredError, body)
	}
}

func TestGetRegisterDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Temporarily disable registration
	server.canRegister.Store(false)
	defer server.canRegister.Store(true)

	req := httptest.NewRequest("GET", "/"+common.RegisterEndpoint, nil)
	w := httptest.NewRecorder()

	viewModel, err := server.getRegister(w, req)

	if err != errRegistrationDisabled {
		t.Errorf("Expected errRegistrationDisabled, got: %v", err)
	}

	if viewModel != nil {
		t.Error("Expected nil ViewModel when registration is disabled")
	}
}

func TestPostRegisterDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Temporarily disable registration
	server.canRegister.Store(false)
	defer server.canRegister.Store(true)

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to register
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, "disabled-registration@privatecaptcha.com")
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should be redirected to an error page when registration is disabled
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status when registration disabled, got %v", w.Code)
	}
}

func TestPostRegisterInvalidCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Temporarily make the puzzle engine return a failed verify result
	originalResult := server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result
	server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result = &puzzle.VerifyResult{Error: puzzle.InvalidSolutionError}
	defer func() {
		server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result = originalResult
	}()

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, t.Name()+"@privatecaptcha.com")
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "invalid-captcha-solution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, captchaVerificationFailed) {
		t.Errorf("Expected captcha verification failed error message, got: %s", body)
	}
}
