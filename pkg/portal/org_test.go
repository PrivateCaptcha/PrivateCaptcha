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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user1.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, userMember1.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
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
	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
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

func TestOrgEndpointsInvalidPathArg(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
	}{
		{"GetOrgDashboardInvalidOrg", "GET", "/org/invalid-id/tab/dashboard", http.StatusSeeOther},
		{"GetOrgMembersInvalidOrg", "GET", "/org/invalid-id/tab/members", http.StatusSeeOther},
		{"GetOrgSettingsInvalidOrg", "GET", "/org/invalid-id/tab/settings", http.StatusSeeOther},
		{"GetOrgAuditLogsInvalidOrg", "GET", "/org/invalid-id/tab/events", http.StatusSeeOther},
		{"GetOrgPropertiesInvalidOrg", "GET", "/org/invalid-id/properties", http.StatusSeeOther},
		{"GetNewPropertyInvalidOrg", "GET", "/org/invalid-id/property/new", http.StatusSeeOther},
		{"GetNewFormInvalidOrg", "GET", "/org/invalid-id/form/new", http.StatusSeeOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestOrgEndpointsWrongOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	_, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_intruder", testPlan)
	if err != nil {
		t.Fatalf("Failed to create intruder account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	org1ID := server.IDHasher.Encrypt(int(org1.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user2.ID)))

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		useCSRF  bool
		formBody url.Values
	}{
		{"GetOrgMembersWrongOwner", "GET", fmt.Sprintf("/org/%s/tab/members", org1ID), http.StatusSeeOther, false, nil},
		{"GetOrgSettingsWrongOwner", "GET", fmt.Sprintf("/org/%s/tab/settings", org1ID), http.StatusSeeOther, false, nil},
		{"GetOrgAuditLogsWrongOwner", "GET", fmt.Sprintf("/org/%s/tab/events", org1ID), http.StatusSeeOther, false, nil},
		{"PutOrgWrongOwner", "PUT", fmt.Sprintf("/org/%s/edit", org1ID), http.StatusSeeOther, true, url.Values{common.ParamName: {"NewName"}}},
		{"DeleteOrgWrongOwner", "DELETE", fmt.Sprintf("/org/%s/delete", org1ID), http.StatusSeeOther, true, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.formBody != nil {
				tc.formBody.Set(common.ParamCSRFToken, csrfToken)
				body = strings.NewReader(tc.formBody.Encode())
			} else {
				body = strings.NewReader("")
			}

			req := httptest.NewRequest(tc.method, tc.path, body)
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
			if tc.useCSRF {
				req.Header.Set(common.HeaderCSRFToken, csrfToken)
			}

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestOrgEndpointsMissingSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewBareAccount(ctx, store, t.Name())
	if err != nil {
		t.Fatalf("Failed to create account without subscription: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	t.Run("PostNewOrgMissingSubscription", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "NewOrgNoSubscription")

		req := httptest.NewRequest("POST", "/org/new", strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK with re-rendered form, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "You need an active subscription") {
			t.Error("Expected response to contain subscription requirement message")
		}
	})
}

func TestOrgEndpointsInvalidFormArgs(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	t.Run("PutOrgEmptyName", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "")

		req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/edit", orgID), strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d", w.Code)
		}
	})

	t.Run("PutOrgShortName", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "ab")

		req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/edit", orgID), strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d", w.Code)
		}
	})
}

func TestOrgMemberEndpointsInvalidForm(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		formBody url.Values
		checkErr string
	}{
		{
			name: "InviteSelfToOrg",
			formBody: url.Values{
				common.ParamEmail: {user.Email},
			},
			checkErr: "already a member",
		},
		{
			name: "InviteInvalidEmail",
			formBody: url.Values{
				common.ParamEmail: {"invalid-email"},
			},
			checkErr: "not valid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.formBody.Set(common.ParamCSRFToken, csrfToken)

			req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/members", orgID), strings.NewReader(tc.formBody.Encode()))
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			body := w.Body.String()
			if !strings.Contains(body, tc.checkErr) {
				t.Errorf("%s: expected response to contain '%s'", tc.name, tc.checkErr)
			}
		})
	}
}

func TestOrgMemberEndpointsInvalidPathArg(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
	}{
		{"DeleteOrgMemberInvalidUser", "DELETE", fmt.Sprintf("/org/%s/members/invalid-user-id", orgID), http.StatusSeeOther},
		{"JoinOrgInvalidOrg", "PUT", "/org/invalid-org-id/members", http.StatusSeeOther},
		{"LeaveOrgInvalidOrg", "DELETE", "/org/invalid-org-id/members", http.StatusSeeOther},
		{"GetOrgNewRuleInvalidOrg", "GET", "/org/invalid-org-id/rules/new", http.StatusSeeOther},
		{"PostOrgNewRuleInvalidOrg", "POST", "/org/invalid-org-id/rules/new", http.StatusSeeOther},
		{"GetOrgEditRuleInvalidRule", "GET", fmt.Sprintf("/org/%s/rules/invalid-rule/edit", orgID), http.StatusSeeOther},
		{"PostOrgEditRuleInvalidRule", "POST", fmt.Sprintf("/org/%s/rules/invalid-rule/edit", orgID), http.StatusSeeOther},
		{"PostOrgMoveRuleInvalidRule", "POST", fmt.Sprintf("/org/%s/rules/invalid-rule/move", orgID), http.StatusSeeOther},
		{"DeleteOrgRuleInvalidRule", "DELETE", fmt.Sprintf("/org/%s/rules/invalid-rule/delete", orgID), http.StatusSeeOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderCSRFToken, csrfToken)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user1.Email, srv, server.XSRF, server.Sessions)
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

	// The HTTP 200 response indicates the invite was created successfully
	// The actual invite is verified by the org members page response
}

func TestOrgInviteRegisterInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Test with invalid ID - URL pattern: /orginvite/{id}/signup
	req := httptest.NewRequest("GET", "/orginvite/invalid-id/signup", nil)

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

func TestOrgInviteRegisterNonExistentID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Test with valid format but non-existent ID - should still show register page (security: don't reveal if invite exists)
	nonExistentID := server.IDHasher.Encrypt(999999)
	req := httptest.NewRequest("GET", "/orginvite/"+nonExistentID+"/signup", nil)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Per security requirements, we show register page even if invite doesn't exist
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK (security: don't reveal if invite exists), got status code %v", resp.StatusCode)
	}
}

func TestOrgInviteRegisterAlreadyLinked(t *testing.T) {
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

	// Create a user who is NOT in the org yet - we'll use them to link the email invite
	user3, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_3", testPlan)
	if err != nil {
		t.Fatalf("Failed to create user3 account: %v", err)
	}

	// Create an email invite and then link it to user3
	testEmail := "linked-" + t.Name() + "@example.com"
	inviteRecord, _, err := store.Impl().InviteEmailToOrg(ctx, user1, org1, testEmail)
	if err != nil {
		t.Fatalf("Failed to create email invite: %v", err)
	}

	// Link the email invite to user3 (who is not yet in the org)
	err = store.Impl().LinkOrgInviteToUser(ctx, inviteRecord.ID, user3)
	if err != nil {
		t.Fatalf("Failed to link invite to user: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to access org invite register with an already linked invite
	req := httptest.NewRequest("GET", "/orginvite/"+server.IDHasher.Encrypt(int(inviteRecord.ID))+"/signup", nil)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected OK (login page), got status code %v", resp.StatusCode)
	}
}

func TestOrgInviteRegisterValidEmailInvite(t *testing.T) {
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

	// Access org invite register with a valid email invite
	req := httptest.NewRequest("GET", "/orginvite/"+server.IDHasher.Encrypt(int(inviteRecord.ID))+"/signup", nil)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should return 200 with the register page
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for valid email invite, got status code %v", resp.StatusCode)
	}
}

// TestOrgMembersShowsEmailInvites tests that after inviting someone by email,
// the invited email appears in the org members list along with existing user invites
func TestOrgMembersShowsEmailInvites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create another user with an existing account and invite them
	existingUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_existinguser", testPlan)
	if err != nil {
		t.Fatalf("Failed to create existing user account: %v", err)
	}

	_, err = store.Impl().InviteUserToOrg(ctx, owner, org, existingUser)
	if err != nil {
		t.Fatalf("Failed to invite existing user: %v", err)
	}

	// Invite someone by email who doesn't have an account
	emailInvite := "email-only-" + t.Name() + "@example.com"
	_, _, err = store.Impl().InviteEmailToOrg(ctx, owner, org, emailInvite)
	if err != nil {
		t.Fatalf("Failed to create email invite: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
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

	renderCtx, ok := viewModel.Model.(*orgMemberRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgMemberRenderContext, got %T", viewModel.Model)
	}

	// Should have 2 members: existing user invite + email invite
	if len(renderCtx.Members) != 2 {
		t.Errorf("Expected 2 members (1 user invite + 1 email invite), got %d", len(renderCtx.Members))
	}

	// Check that both invites are present
	foundExistingUser := false
	foundEmailInvite := false
	for _, m := range renderCtx.Members {
		if m.Email == common.MaskEmail(existingUser.Email, '*') {
			foundExistingUser = true
		}
		if m.Email == common.MaskEmail(emailInvite, '*') {
			foundEmailInvite = true
		}
	}

	if !foundExistingUser {
		t.Errorf("Expected to find existing user %s in members list", existingUser.Email)
	}
	if !foundEmailInvite {
		t.Errorf("Expected to find email invite %s in members list", emailInvite)
	}
}

// TestOrgMemberBecomesMemberAfterJoining tests that after a user joins via email invitation,
// they appear as a full member (not invited) in the owner's list
func TestOrgMemberBecomesMemberAfterJoining(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create an email-only invite
	testEmail := "joining-user-" + t.Name() + "@example.com"
	inviteRecord, _, err := store.Impl().InviteEmailToOrg(ctx, owner, org, testEmail)
	if err != nil {
		t.Fatalf("Failed to create email invite: %v", err)
	}

	// Create a new user (simulating registration after receiving the email invite)
	newUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_newuser", testPlan)
	if err != nil {
		t.Fatalf("Failed to create new user account: %v", err)
	}

	// Link the email invite to the new user (simulating the onboard job)
	err = store.Impl().LinkOrgInviteToUser(ctx, inviteRecord.ID, newUser)
	if err != nil {
		t.Fatalf("Failed to link invite to user: %v", err)
	}

	// User joins the org (changes status from 'invited' to 'member')
	_, err = store.Impl().JoinOrg(ctx, org.ID, newUser)
	if err != nil {
		t.Fatalf("Failed to join org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
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

	renderCtx, ok := viewModel.Model.(*orgMemberRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *orgMemberRenderContext, got %T", viewModel.Model)
	}

	// Should have 1 member (the user who joined)
	if len(renderCtx.Members) != 1 {
		t.Errorf("Expected 1 member, got %d", len(renderCtx.Members))
	}

	// Check that the user appears as 'member' not 'invited'
	if len(renderCtx.Members) > 0 {
		member := renderCtx.Members[0]
		if member.Level != string(dbgen.AccessLevelMember) {
			t.Errorf("Expected member level to be 'member', got '%s'", member.Level)
		}
		if member.Name != newUser.Name {
			t.Errorf("Expected member name to be '%s', got '%s'", newUser.Name, member.Name)
		}
	}
}

func TestDeleteOrgMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create owner account
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create member account
	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	// Invite member to org
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member: %v", err)
	}

	// Member joins org
	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatalf("Failed to join org: %v", err)
	}

	// Verify member is in org
	members, err := store.Impl().RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve members: %v", err)
	}

	foundMember := false
	for _, m := range members {
		if m.User.ID == member.ID {
			foundMember = true
			break
		}
	}
	if !foundMember {
		t.Fatal("Member should be in org before deletion")
	}

	// Setup server
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Authenticate as owner
	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Delete member from org
	orgID := server.IDHasher.Encrypt(int(org.ID))
	memberID := server.IDHasher.Encrypt(int(member.ID))

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/members/%s", orgID, memberID), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(owner.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	// Verify member is no longer in org
	members, err = store.Impl().RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve members after deletion: %v", err)
	}

	for _, m := range members {
		if m.User.ID == member.ID {
			t.Error("Member should no longer be in org after deletion")
		}
	}
}

func TestDeleteOrgMembersEmailInvite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create owner account
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create an email-only invite (invited user has not yet accepted)
	inviteEmail := "email-invite-" + t.Name() + "@example.com"
	inviteRecord, _, err := store.Impl().InviteEmailToOrg(ctx, owner, org, inviteEmail)
	if err != nil {
		t.Fatalf("Failed to create email invite: %v", err)
	}

	// Verify invite is in org members
	inviteMembers, err := store.Impl().RetrieveOrganizationUsersWithEmailInvites(ctx, org.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve members: %v", err)
	}

	foundInvite := false
	for _, m := range inviteMembers {
		if m.OrganizationUser.ID == inviteRecord.ID {
			foundInvite = true
			break
		}
	}
	if !foundInvite {
		t.Fatal("Email invite should be in org members before deletion")
	}

	// Setup server
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Authenticate as owner
	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Delete the email invite using the OrgInviteEndpoint with org prefix
	orgID := server.IDHasher.Encrypt(int(org.ID))
	inviteID := server.IDHasher.Encrypt(int(inviteRecord.ID))
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/orginvite/%s", orgID, inviteID), nil)
	req.SetPathValue(common.ParamOrg, orgID)
	req.SetPathValue(common.ParamID, inviteID)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(owner.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	// Verify invite is no longer in org members
	inviteMembers, err = store.Impl().RetrieveOrganizationUsersWithEmailInvites(ctx, org.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve members after deletion: %v", err)
	}

	for _, m := range inviteMembers {
		if m.OrganizationUser.ID == inviteRecord.ID {
			t.Error("Email invite should no longer be in org after deletion")
		}
	}
}

func TestJoinOrgNotInvited(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a user who is NOT invited to the org
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create user account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to join org without being invited
	req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/members", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should fail with error redirect since user is not invited
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}

	// Verify user is NOT in the org
	members, err := store.Impl().RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range members {
		if m.User.ID == user.ID {
			t.Error("User should NOT be a member of the org since they were not invited")
		}
	}
}

func TestLeaveOrgNotMember(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	_, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a user who is NOT a member of the org
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create user account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to leave org without being a member
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/members", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should fail with error redirect since user is not a member
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	location, _ := resp.Location()
	if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
		t.Errorf("Expected error redirect, got: %s", location.String())
	}
}

func TestDeleteOrgMemberNonExistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a random user who is NOT a member
	nonMember, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_nonmember", testPlan)
	if err != nil {
		t.Fatalf("Failed to create non-member account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Authenticate as the owner
	cookie, err := portal_tests.AuthenticateSuite(ctx, owner.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to delete a user who is not a member
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/members/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(nonMember.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(owner.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// The endpoint should return an error status since the user is not a member
	if resp.StatusCode == http.StatusOK {
		t.Error("Expected error status when trying to delete a non-member, got OK")
	}
}

func TestGetOrgSoftDeleted(t *testing.T) {
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

	// Create an org to soft delete
	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-delete-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	// Soft delete the org
	if _, err := store.Impl().SoftDeleteOrganization(ctx, org, user); err != nil {
		t.Fatalf("Failed to soft delete org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to access the soft-deleted org dashboard
	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/tab/dashboard", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should redirect to portal root or error when accessing soft-deleted org
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect for soft-deleted org, got status code %v", resp.StatusCode)
	}
}

func TestGetPropertySoftDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create a property to soft delete
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, t.Name()+".com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Soft delete the property
	if _, err := store.Impl().SoftDeleteProperty(ctx, property, org, user); err != nil {
		t.Fatalf("Failed to soft delete property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to access the soft-deleted property
	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should redirect when accessing soft-deleted property
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect for soft-deleted property, got status code %v", resp.StatusCode)
	}
}

func TestHandlerSwitchCase(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))

	testCases := []struct {
		name         string
		path         string
		expectStatus int
	}{
		{
			name:         "InvalidOrgPathArg",
			path:         "/org/invalid-id/tab/dashboard",
			expectStatus: http.StatusSeeOther, // redirect to error
		},
		{
			name:         "ValidOrgDashboard",
			path:         fmt.Sprintf("/org/%s/tab/dashboard", orgID),
			expectStatus: http.StatusOK,
		},
		{
			name:         "InvalidPropertyPathArg",
			path:         fmt.Sprintf("/org/%s/property/invalid-id", orgID),
			expectStatus: http.StatusSeeOther, // redirect to error
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.expectStatus {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.expectStatus)
			}
		})
	}
}

func TestRetrieveOrgPropertyDeletedFromCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create a property
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, t.Name()+".com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Delete property from cache using the public wrapper
	store.Impl().InvalidatePropertyCache(ctx, property)

	// Now try to retrieve the property - should still work since it's in DB
	retrievedProperty, err := store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve org property after cache delete: %v", err)
	}

	if retrievedProperty.ID != property.ID {
		t.Errorf("Retrieved property ID %d, want %d", retrievedProperty.ID, property.ID)
	}

	if retrievedProperty.Name != property.Name {
		t.Errorf("Retrieved property Name %s, want %s", retrievedProperty.Name, property.Name)
	}
}

func TestRetrieveOrgOwnerWithSubscriptionNonOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create a second user who is NOT the org owner
	nonOwner, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_nonowner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create non-owner account: %v", err)
	}

	// Invite and add non-owner as a member
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, nonOwner); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Impl().JoinOrg(ctx, org.ID, nonOwner); err != nil {
		t.Fatal(err)
	}

	// Try to retrieve org owner with subscription using non-owner as session user
	retrievedOwner, subscription, err := store.Impl().RetrieveOrgOwnerWithSubscription(ctx, org, nonOwner)
	if err != nil {
		t.Fatalf("RetrieveOrgOwnerWithSubscription failed: %v", err)
	}

	// Should return the actual org owner, not the non-owner
	if retrievedOwner.ID != owner.ID {
		t.Errorf("Retrieved owner ID %d, want %d (the actual org owner)", retrievedOwner.ID, owner.ID)
	}

	if subscription == nil {
		t.Error("Expected subscription to be returned for org owner")
	}
}

func TestNewOrganizationAuditLogsWithData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create some audit logs by creating properties
	_, _, err = store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "org-audit-1.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}
	_, _, err = store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "org-audit-2.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Retrieve org audit logs
	logs, err := store.Impl().RetrieveOrganizationAuditLogs(ctx, org, 100)
	if err != nil {
		t.Fatalf("Failed to retrieve org audit logs: %v", err)
	}

	if len(logs) == 0 {
		t.Skip("No audit logs found for org - skipping test")
	}

	// Test newOrganizationAuditLogs
	result := server.newOrganizationAuditLogs(ctx, user, logs)

	if len(result) == 0 {
		t.Error("Expected non-empty result from newOrganizationAuditLogs")
	}

	// Verify each log has expected fields
	for i, ul := range result {
		if ul.Time == "" {
			t.Errorf("Audit log %d: Expected Time to be set", i)
		}
		if ul.Action == "" {
			t.Errorf("Audit log %d: Expected Action to be set", i)
		}
		// UserName/UserEmail should be set (either actual name or "Unknown User")
		if ul.UserName == "" {
			t.Errorf("Audit log %d: Expected UserName to be set", i)
		}
	}
}

func TestPostNewOrgInvalidForm(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Send invalid percent-encoding that will cause ParseForm to fail
	req := httptest.NewRequest("POST", "/org/new", strings.NewReader("name=%ZZ"))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// When ParseForm fails, server redirects to error endpoint
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303), got %d", w.Code)
	}
}

func TestPostNewOrgWrongName(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	// Test with empty name
	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, "")

	req := httptest.NewRequest("POST", "/org/new", strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should return 200 with error in form
	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	body := w.Body.String()
	// Check for specific error message from common.StatusOrgNameEmptyError constant
	expectedError := common.StatusOrgNameEmptyError.String()
	if !strings.Contains(body, expectedError) {
		t.Errorf("Expected error message '%s', got body: %s", expectedError, body)
	}
}

func TestPostNewOrgUserWithoutSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create a bare account without subscription
	user, _, err := db_tests.CreateNewBareAccount(ctx, store, t.Name())
	if err != nil {
		t.Fatalf("Failed to create bare account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	// Attempt to create org
	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, "Test Org Without Subscription")

	req := httptest.NewRequest("POST", "/org/new", strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should return 200 with error message about subscription
	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	body := w.Body.String()
	// Check for specific error message from activeSubscriptionForOrgError
	if !strings.Contains(body, "You need an active subscription to create new organizations") {
		t.Errorf("Expected error message about subscription requirement, got body: %s", body)
	}
}

func TestDeleteOrgMembersUnauthorized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to delete org members without being authenticated
	req := httptest.NewRequest("DELETE", "/org/test-org-id/members/test-user-id", nil)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should redirect to login/error page when unauthenticated
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303), got %d", w.Code)
	}
}

func TestDeleteOrgMembersInvalidForm(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	// Test with invalid user ID
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/members/invalid-user-id", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should redirect to error page when invalid path argument is provided
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303), got %d", w.Code)
	}
}

func TestDeleteOrgMembersMemberNotOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	// Add member to org
	_, err = store.Impl().InviteUserToOrg(ctx, owner, org, member)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Impl().JoinOrg(ctx, org.ID, member)
	if err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Authenticate as the member (not owner)
	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(member.ID)))

	// Try to remove owner (member cannot do this)
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/members/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(owner.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Member cannot delete others - should redirect to error
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303), got %d", w.Code)
	}
}

func TestInviteDisabledUserToOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user1, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	org1, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-actual-org", user1.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	// Create another user account
	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create invitee account: %v", err)
	}

	// Disable the user
	if err := db_tests.DisableUserForTest(ctx, store, user2.ID); err != nil {
		t.Fatalf("failed to disable user: %v", err)
	}

	// Clear user cache to ensure disabled status is fetched from DB
	cache.Delete(ctx, db.UserCacheKey(user2.ID))

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user1.Email, srv, server.XSRF, server.Sessions)
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

	// Verify the body contains an error about not being able to invite the user
	body := w.Body.String()
	if !strings.Contains(body, "Cannot invite") {
		t.Errorf("Expected error about not being able to invite disabled user, got: %s", body)
	}

	// Verify that the disabled user was not added to the org (only owner should be present)
	members, err := store.Impl().RetrieveOrganizationUsers(ctx, org1.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Only the owner should be a member, the disabled user should not have been invited
	for _, m := range members {
		if m.User.ID == user2.ID {
			t.Errorf("Disabled user should not be invited to org")
		}
	}
}

func TestGetPortalAllTabs(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))

	tabs := []struct {
		name string
		tab  string
		body string
	}{
		{name: "Dashboard", tab: common.DashboardEndpoint},
		{name: "Forms", tab: common.FormsEndpoint, body: "No forms"},
		{name: "Members", tab: common.MembersEndpoint},
		{name: "Settings", tab: common.SettingsEndpoint},
		{name: "Events", tab: common.EventsEndpoint},
		{name: "Default", tab: ""},
		{name: "Unknown", tab: "unknown-tab"},
	}

	for _, tc := range tabs {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/org/%s", orgID)
			if tc.tab != "" {
				path += "?" + common.ParamTab + "=" + tc.tab
			}

			req := httptest.NewRequest("GET", path, nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200 for tab '%s', got %d", tc.tab, w.Code)
			}

			if (tc.body != "") && !strings.Contains(w.Body.String(), tc.body) {
				t.Errorf("Expected response body for tab '%s' to contain %q", tc.tab, tc.body)
			}
		})
	}
}

func TestGetOrgFormsTabEndpoint(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/%s/%s", server.IDHasher.Encrypt(int(org.ID)), common.TabEndpoint, common.FormsEndpoint), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "No forms") {
		t.Fatalf("Expected forms tab endpoint body to contain %q", "No forms")
	}
}

func TestGetPortalFormsTabShowsEmptyState(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s?tab=%s", orgID, common.FormsEndpoint), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "No forms") {
		t.Fatalf("Expected forms empty state in body")
	}
	if !strings.Contains(body, "Add New Form") {
		t.Fatalf("Expected add form CTA in body")
	}
	if !strings.Contains(body, fmt.Sprintf("/org/%s/%s/%s", orgID, common.FormEndpoint, common.NewEndpoint)) {
		t.Fatalf("Expected add form CTA URL in body")
	}
}

func TestGetPortalFormsTabShowsForms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "forms-tab.example.com"),
		&dbgen.CreateFormParams{Name: t.Name(), URL: "https://hooks.example.com/submit/form", Fields: []byte(`{}`), Enabled: true},
		org,
	)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	_ = form

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s?tab=%s", orgID, common.FormsEndpoint), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, form.Name) {
		t.Fatalf("Expected form name %q in body", form.Name)
	}
	if !strings.Contains(body, "hooks.example.com/submit") {
		t.Fatalf("Expected webhook prefix in body")
	}
	if strings.Contains(body, "No forms") {
		t.Fatalf("Did not expect empty state when forms exist")
	}
}

func TestGetOrgFormsPaginationEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	var firstFormName string
	var lastFormName string
	for i := range propertiesPerPage + 1 {
		form, _, _, err := store.Impl().CreateNewForm(ctx,
			db_tests.CreateNewPropertyParams(user.ID, fmt.Sprintf("forms-page-%d.example.com", i)),
			&dbgen.CreateFormParams{
				Name:    fmt.Sprintf("%s-%d", t.Name(), i),
				URL:     fmt.Sprintf("https://hooks.example.com/forms/%d", i),
				Fields:  []byte(`{}`),
				Enabled: true},
			org,
		)
		if err != nil {
			t.Fatalf("Failed to create form %d: %v", i, err)
		}
		if i == 0 {
			firstFormName = form.Name
		}
		if i == propertiesPerPage {
			lastFormName = form.Name
		}
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	testCases := []struct {
		name           string
		page           string
		mustContain    string
		mustNotContain string
	}{
		{name: "SecondPage", page: "1", mustContain: lastFormName, mustNotContain: firstFormName},
		{name: "InvalidPageFallsBack", page: "oops", mustContain: firstFormName, mustNotContain: lastFormName},
		{name: "NegativePageFallsBack", page: "-1", mustContain: firstFormName, mustNotContain: lastFormName},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/%s?%s=%s", orgID, common.FormsEndpoint, common.ParamPage, tc.page), nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status 200, got %d", w.Code)
			}

			body := w.Body.String()
			if !strings.Contains(body, tc.mustContain) {
				t.Fatalf("Expected body to contain %q", tc.mustContain)
			}
			if strings.Contains(body, tc.mustNotContain) {
				t.Fatalf("Did not expect body to contain %q", tc.mustNotContain)
			}
			if strings.Contains(body, "Select a tab") {
				t.Fatalf("Expected forms endpoint to return partial without tab chrome")
			}
		})
	}
}

func TestOrgIDValid(t *testing.T) {
	const testOrgID = 42
	encrypted := server.IDHasher.Encrypt(testOrgID)

	req := httptest.NewRequest("GET", "/", nil)
	req.SetPathValue(common.ParamOrg, encrypted)

	orgID, err := server.OrgID(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if orgID != int32(testOrgID) {
		t.Errorf("Expected org ID %d, got %d", testOrgID, orgID)
	}
}

func TestOrgIDInvalid(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.SetPathValue(common.ParamOrg, "not-a-valid-id")

	_, err := server.OrgID(req)
	if err == nil {
		t.Fatal("Expected error for invalid org ID, got nil")
	}
}

func TestOrgIDEmpty(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)

	_, err := server.OrgID(req)
	if err == nil {
		t.Fatal("Expected error for empty org ID, got nil")
	}
}
