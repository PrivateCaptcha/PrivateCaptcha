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
