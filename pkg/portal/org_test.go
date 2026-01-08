package portal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func TestGetAnotherUsersOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	_, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create another user account
	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create intruder account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/%s/%s", server.IDHasher.Encrypt(int(org1.ID)), common.TabEndpoint, common.DashboardEndpoint), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	url, _ := resp.Location()
	if path := url.String(); !strings.HasPrefix(path, "/"+common.ErrorEndpoint) {
		t.Errorf("Unexpected redirect: %s", path)
	}
}

func TestInviteUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user1, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// we create extra org to create a difference in auto-incremented IDs for users and orgs
	org1, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-actual-org", user1.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	// Create another user account
	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create invitee account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user1.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user1.ID))))
	form.Set(common.ParamEmail, user2.Email)

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/members", server.IDHasher.Encrypt(int(org1.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	members, err := store.Impl().RetrieveOrganizationUsers(ctx, org1.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(members) != 1 {
		t.Errorf("Unexpected length of members: %v", len(members))
	}

	member := members[0]
	if (member.User.ID != user2.ID) && (member.Level != dbgen.AccessLevelInvited) {
		t.Errorf("Org member is not invited user")
	}
}

func TestDeleteUserFromOrgPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	userMember1, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create 1st invitee account: %v", err)
	}

	userMember2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_3", testPlan)
	if err != nil {
		t.Fatalf("Failed to create 2nd invitee account: %v", err)
	}

	for _, user := range []*dbgen.User{userMember1, userMember2} {
		if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, user); err != nil {
			t.Fatal(err)
		}

		if _, err := store.Impl().JoinOrg(ctx, org.ID, user); err != nil {
			t.Fatal(err)
		}
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, userMember1.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	// user1 tries to delete user2 from org, despite note being the owner
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/members/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(userMember2.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(userMember1.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	url, _ := resp.Location()
	if path := url.String(); !strings.HasPrefix(path, "/"+common.ErrorEndpoint) {
		t.Errorf("Unexpected redirect: %s", path)
	}

	members, err := store.Impl().RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(members) != 2 {
		t.Errorf("Unexpected length of members: %v", len(members))
	}
}

func TestGetNewOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/org/new", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	viewModel, err := server.getNewOrg(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != orgWizardTemplate {
		t.Errorf("Expected view to be %s, got %s", orgWizardTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*orgWizardRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgWizardRenderContext, got %T", viewModel.Model)
	}

	if len(renderCtx.Token) == 0 {
		t.Error("Expected CSRF token to be populated")
	}
}

func TestGetOrgDashboard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/tab/dashboard", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getOrgDashboard(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != orgDashboardTemplate {
		t.Errorf("Expected view to be %s, got %s", orgDashboardTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*orgPropertiesRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgPropertiesRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.CurrentOrg == nil {
		t.Fatal("Expected CurrentOrg to be populated, got nil")
	}

	if renderCtx.CurrentOrg.Name != org.Name {
		t.Errorf("Expected org name to be %s, got %s", org.Name, renderCtx.CurrentOrg.Name)
	}
}

func TestGetOrgProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	if _, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org); err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/tab/properties", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getOrgProperties(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != orgPropertiesTemplate {
		t.Errorf("Expected view to be %s, got %s", orgPropertiesTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*orgPropertiesRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgPropertiesRenderContext, got %T", viewModel.Model)
	}

	if len(renderCtx.Properties) != 1 {
		t.Errorf("Expected 1 property, got %d", len(renderCtx.Properties))
	}
}

func TestGetOrgMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/tab/members", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getOrgMembers(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != orgMembersTemplate {
		t.Errorf("Expected view to be %s, got %s", orgMembersTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*orgMemberRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgMemberRenderContext, got %T", viewModel.Model)
	}

	if !renderCtx.CanEdit {
		t.Error("Expected CanEdit to be true for org owner")
	}
}

func TestGetOrgSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/tab/settings", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getOrgSettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != orgSettingsTemplate {
		t.Errorf("Expected view to be %s, got %s", orgSettingsTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*orgSettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgSettingsRenderContext, got %T", viewModel.Model)
	}

	if !renderCtx.CanEdit {
		t.Error("Expected CanEdit to be true for org owner")
	}

	if viewModel.AuditEvent == nil {
		t.Error("Expected AuditEvent to be populated")
	}
}

func TestGetOrgAuditLogs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/tab/events", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getOrgAuditLogs(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != orgAuditLogsTemplate {
		t.Errorf("Expected view to be %s, got %s", orgAuditLogsTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*orgAuditLogsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgAuditLogsRenderContext, got %T", viewModel.Model)
	}

	if !renderCtx.CanView {
		t.Error("Expected CanView to be true for org owner")
	}
}

func TestPutOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	newName := t.Name() + "-updated"
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, newName)

	req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/edit", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.putOrg(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != orgSettingsTemplate {
		t.Errorf("Expected view to be %s, got %s", orgSettingsTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*orgSettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgSettingsRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.CurrentOrg.Name != newName {
		t.Errorf("Expected org name to be %s, got %s", newName, renderCtx.CurrentOrg.Name)
	}

	if viewModel.AuditEvent == nil {
		t.Error("Expected AuditEvent to be populated")
	}
}

func TestPostNewOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Use admin plan with higher org limits to allow creating additional orgs
	adminPlan := server.PlanService.GetInternalAdminPlan()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), adminPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	orgName := t.Name() + "-new-org"
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, orgName)

	req := httptest.NewRequest("POST", "/org/new", strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect response but got error: %v", err)
	}

	if !strings.HasPrefix(location.String(), "/org/") {
		t.Errorf("Unexpected redirect path: %s", location.String())
	}
}

func TestJoinOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create user account: %v", err)
	}

	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, user); err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/members", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	members, err := store.Impl().RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}

	hasUser := false
	for _, m := range members {
		if m.User.ID == user.ID && m.Level == dbgen.AccessLevelMember {
			hasUser = true
			break
		}
	}

	if !hasUser {
		t.Error("User should be a member of the org after joining")
	}
}

func TestLeaveOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create user account: %v", err)
	}

	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, user); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Impl().JoinOrg(ctx, org.ID, user); err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/members", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	members, err := store.Impl().RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range members {
		if m.User.ID == user.ID && m.Level == dbgen.AccessLevelMember {
			t.Error("User should have left the org (level should change from member to invited)")
		}
	}
}

func TestDeleteOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Use admin plan to allow creating extra org
	adminPlan := server.PlanService.GetInternalAdminPlan()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), adminPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-delete-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/delete", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	orgs, err := store.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, o := range orgs {
		if o.Organization.ID == org.ID {
			t.Error("Org should have been deleted")
		}
	}
}

func TestTransferOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create the owner account
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create the new owner account
	newOwner, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_new_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create new owner account: %v", err)
	}

	// Invite and join the new owner as a member
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, newOwner); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Impl().JoinOrg(ctx, org.ID, newOwner); err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(owner.ID))))
	form.Set(common.ParamUser, server.IDHasher.Encrypt(int(newOwner.ID)))

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/transfer", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	// Verify the organization has been transferred
	newOwnerOrgs, err := store.Impl().RetrieveUserOrganizations(ctx, newOwner.ID)
	if err != nil {
		t.Fatal(err)
	}

	hasOrgAsOwner := false
	for _, o := range newOwnerOrgs {
		if o.Organization.ID == org.ID && o.Level == dbgen.AccessLevelOwner {
			hasOrgAsOwner = true
			break
		}
	}

	if !hasOrgAsOwner {
		t.Error("New owner should be the owner of the transferred org")
	}

	// Verify old owner no longer owns the org
	oldOwnerOrgs, err := store.Impl().RetrieveUserOrganizations(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, o := range oldOwnerOrgs {
		if o.Organization.ID == org.ID && o.Level == dbgen.AccessLevelOwner {
			t.Error("Old owner should no longer be the owner of the transferred org")
		}
	}
}

func TestTransferOrgInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	// Test with invalid user parameter
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(owner.ID))))
	form.Set(common.ParamUser, "invalid-id")

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/transfer", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect for invalid params, got status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}
}

func TestTransferOrgNotOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a non-owner member
	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	// Invite and join member
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Authenticate as the member (not the owner)
	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	// Try to transfer org as non-owner
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(member.ID))))
	form.Set(common.ParamUser, server.IDHasher.Encrypt(int(owner.ID)))

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/transfer", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect for unauthorized user, got status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}
}

func TestTransferOrgToNonMember(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a user who is not a member
	nonMember, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_nonmember", testPlan)
	if err != nil {
		t.Fatalf("Failed to create non-member account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	// Try to transfer to non-member
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(owner.ID))))
	form.Set(common.ParamUser, server.IDHasher.Encrypt(int(nonMember.ID)))

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/transfer", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect for non-member, got status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}
}

func TestTransferOrgToInvitedMember(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a user who is only invited (not accepted)
	invitedUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_invited", testPlan)
	if err != nil {
		t.Fatalf("Failed to create invited user account: %v", err)
	}

	// Invite user but do NOT have them join
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, invitedUser); err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	// Try to transfer to invited (but not accepted) member
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(owner.ID))))
	form.Set(common.ParamUser, server.IDHasher.Encrypt(int(invitedUser.ID)))

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/transfer", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect for invited member, got status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}
}

func TestInviteNonExistingUserByEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user1, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	org1, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-actual-org", user1.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user1.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	// Use a non-existing email address for invitation
	nonExistingEmail := "non-existing-user-" + t.Name() + "@example.com"

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user1.ID))))
	form.Set(common.ParamEmail, nonExistingEmail)

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/members", server.IDHasher.Encrypt(int(org1.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	// Verify invite was created with email (no user_id)
	invite, err := store.Impl().Querier().GetOrgInviteByEmail(ctx, &dbgen.GetOrgInviteByEmailParams{
		OrgID: org1.ID,
		Email: db.Text(nonExistingEmail),
	})
	if err != nil {
		t.Fatalf("Failed to find email invite: %v", err)
	}

	if invite.Email.String != nonExistingEmail {
		t.Errorf("Unexpected invite email: %s", invite.Email.String)
	}

	if invite.UserID.Valid {
		t.Error("Expected invite to have NULL user_id")
	}

	if invite.Level != dbgen.AccessLevelInvited {
		t.Errorf("Expected invite level to be 'invited', got: %s", invite.Level)
	}
}

func TestRegisterInviteInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Test with invalid ID
	req := httptest.NewRequest("GET", "/signup-invite/invalid-id", nil)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect (error), got status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}
}

func TestRegisterInviteNonExistentID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Test with valid format but non-existent ID
	nonExistentID := server.IDHasher.Encrypt(999999)
	req := httptest.NewRequest("GET", "/signup-invite/"+nonExistentID, nil)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect (error), got status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}
}

func TestRegisterInviteAlreadyLinked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user1, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	org1, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-actual-org", user1.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	// Create another user and invite them (this creates an invite with user_id set)
	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create invitee account: %v", err)
	}

	_, err = store.Impl().InviteUserToOrg(ctx, user1, org1, user2)
	if err != nil {
		t.Fatalf("Failed to invite user: %v", err)
	}

	// Get the invite ID - we need to get the org users to find it
	members, err := store.Impl().RetrieveOrganizationUsers(ctx, org1.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve org users: %v", err)
	}

	// Find the invite for user2 - we need to get it via the generated querier
	invites, err := store.Impl().Querier().GetOrganizationUsersWithPending(ctx, org1.ID)
	if err != nil {
		t.Fatalf("Failed to get org users with pending: %v", err)
	}

	var inviteID int32
	for _, inv := range invites {
		if inv.UserID.Valid && inv.UserID.Int32 == user2.ID {
			inviteID = inv.ID
			break
		}
	}

	if inviteID == 0 {
		t.Fatalf("Failed to find invite ID, members: %v, invites: %v", members, invites)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to access register-invite with an already linked invite
	req := httptest.NewRequest("GET", "/signup-invite/"+server.IDHasher.Encrypt(int(inviteID)), nil)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect (error), got status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}
}

func TestRegisterInviteValidEmailInvite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user1, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	org1, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-actual-org", user1.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	// Create an email-only invite
	testEmail := "test-email-" + t.Name() + "@example.com"
	inviteRecord, _, err := store.Impl().InviteEmailToOrg(ctx, user1, org1, testEmail)
	if err != nil {
		t.Fatalf("Failed to create email invite: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Access register-invite with a valid email invite
	req := httptest.NewRequest("GET", "/signup-invite/"+server.IDHasher.Encrypt(int(inviteRecord.ID)), nil)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should return 200 with the register page
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for valid email invite, got status code %v", resp.StatusCode)
	}
}
