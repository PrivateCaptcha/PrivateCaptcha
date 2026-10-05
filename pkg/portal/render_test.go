package portal

import (
	"fmt"
	randv2 "math/rand/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func stubProperty(name, orgID string) *userProperty {
	return &userProperty{
		ID:      "1",
		OrgID:   orgID,
		Name:    name,
		Domain:  "example.com",
		Level:   1,
		Growth:  2,
		Enabled: true,
	}
}

func stubForm(name, orgID string) *userForm {
	return &userForm{
		ID:            "1",
		OrgID:         orgID,
		Name:          name,
		URL:           "https://hooks.example.com/submit/form",
		Method:        http.MethodPost,
		WebhookPrefix: "hooks.example.com/submit",
		Enabled:       true,
		Active:        true,
	}
}

func stubOrgEx(orgID string, level dbgen.AccessLevel) *UserOrg {
	return &UserOrg{
		Name:  "My Org " + orgID,
		ID:    orgID,
		Level: string(level),
	}
}

func stubOrg(orgID string) *UserOrg {
	return stubOrgEx(orgID, dbgen.AccessLevelOwner)
}

func stubToken() CsrfRenderContext {
	return CsrfRenderContext{Token: "token"}
}

func stubUser(name string, level dbgen.AccessLevel) *orgUser {
	return &orgUser{
		Name:      name,
		ID:        "123",
		Level:     string(level),
		CreatedAt: common.JSONTimeNow().String(),
	}
}

func stubAPIKey(name string) *userAPIKey {
	return &userAPIKey{
		ID:          "123",
		Name:        name,
		ExpiresAt:   common.JSONTimeNowAdd(1 * time.Hour).String(),
		Secret:      "",
		ExpiresSoon: false,
	}
}

func stubAuditLogs() []*UserAuditLog {
	tables := []string{
		db.TableNameOrgUsers,
		db.TableNameAPIKeys,
		db.TableNameProperties,
		db.TableNameEdgeSettings,
		db.TableNameOrgs,
		db.TableNameUsers,
		db.TableNameAuditLogs,
	}

	actions := []dbgen.AuditLogAction{
		dbgen.AuditLogActionUnknown,
		dbgen.AuditLogActionCreate,
		dbgen.AuditLogActionUpdate,
		dbgen.AuditLogActionSoftDelete,
		dbgen.AuditLogActionDelete,
		dbgen.AuditLogActionRecover,
		dbgen.AuditLogActionLogin,
		dbgen.AuditLogActionLogout,
		dbgen.AuditLogActionAccess,
	}

	sources := []dbgen.AuditLogSource{
		dbgen.AuditLogSourcePortal,
		dbgen.AuditLogSourceApi,
	}

	result := make([]*UserAuditLog, 0)

	for _, table := range tables {
		for _, action := range actions {
			result = append(result, &UserAuditLog{
				UserName:  "User Name",
				UserEmail: "foo@bar.com",
				Action:    string(action),
				Source:    string(sources[randv2.IntN(len(sources))]),
				Property:  "Property",
				Resource:  "Resource",
				Value:     "Value",
				TableName: table,
				Time:      time.Now().Format(auditLogTimeFormat),
			})
		}
	}

	return result
}

func ruleNames(rules []*DifficultyRuleModel) []string {
	result := make([]string, 0, len(rules))
	for _, r := range rules {
		result = append(result, r.Name)
	}
	return result
}

func TestRenderHTML(t *testing.T) {
	zeroBudget := "0"
	missingBudget := ""
	enterpriseOnly := new(bool)
	*enterpriseOnly = true
	hostileOrg := stubOrg("123")
	hostileOrg.Name = "'-alert(1)-'"
	previewOrg := stubOrg("123")
	invitedOrg := stubOrgEx("123", dbgen.AccessLevelInvited)
	memberOrg := stubOrgEx("456", dbgen.AccessLevelMember)
	blakeProperty := stubProperty("Foo", "123")
	blakeProperty.Challenge = string(dbgen.ChallengeTypeBlake2b)
	argonProperty := stubProperty("Foo", "123")
	argonProperty.Challenge = string(dbgen.ChallengeTypeArgon2ID)
	edgeProperty := stubProperty("Foo", "123")
	edgeProperty.HasDomain = true
	edgeProperty.EdgeTokenValidityInterval = 4
	integrationForm := stubForm("Contact", "123")
	integrationForm.ExternalID = "form-uuid"
	formWizardModel := &formWizardRenderContext{
		CurrentOrg:        stubOrg("123"),
		CsrfRenderContext: stubToken(),
	}
	formDashboardModel := &formDashboardRenderContext{
		CsrfRenderContext: stubToken(),
		Form:              integrationForm,
		Org:               stubOrg("123"),
		Tab:               formReportsTabIndex,
	}
	formIntegrationsModel := &formDashboardIntegrationsRenderContext{
		formDashboardRenderContext: *formDashboardModel,
		Sitekey:                    "qwerty",
	}
	formAuditLogsModel := &formAuditLogsRenderContext{
		formDashboardRenderContext: *formDashboardModel,
		AuditLogsRenderContext: AuditLogsRenderContext{
			SeeMore: true,
		},
	}
	formAuditLogsModel.Tab = formAuditLogsTabIndex
	formsPaginationModel := &orgFormsRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{
			CurrentOrg: stubOrg("123"),
		},
		PaginationRenderContext: PaginationRenderContext{
			From:    1,
			To:      30,
			Count:   31,
			Page:    0,
			PerPage: 30,
		},
		Forms: []*userForm{stubForm("Newsletter Signup", "123")},
	}
	usageStatsModel := &settingsUsageRenderContext{
		OrganizationStats: []*organizationUsageStats{
			{
				ID:         "123",
				Name:       "My Org 123",
				Members:    3,
				Properties: 2,
				Forms:      1,
				Rules:      2,
			},
		},
	}
	hostileOrgModel := &orgDashboardRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{
			Orgs:       []*UserOrg{hostileOrg},
			CurrentOrg: hostileOrg,
		},
	}
	previewModel := &orgDashboardRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{
			Orgs:       []*UserOrg{previewOrg},
			CurrentOrg: previewOrg,
			Search: &OrgSearchRenderContext{
				CurrentOrg: previewOrg,
				SearchTerm: "Searchable",
				SearchResults: []*OrgSearchResult{
					{ID: "property-1", Type: "property", Name: "Searchable Property", Description: "search-domain.example.com"},
					{ID: "form-1", Type: "form", Name: "Searchable Form", Description: "https://hooks.example.com/search-target"},
				},
				NextOffset: 10,
				HasMore:    true,
			},
		},
	}
	moreSearchModel := &OrgSearchRenderContext{
		CurrentOrg: stubOrg("123"),
		SearchTerm: "Searchable",
		NextOffset: 10,
		HasMore:    true,
	}
	invitedModel := &orgDashboardRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{
			Orgs:       []*UserOrg{invitedOrg, memberOrg},
			CurrentOrg: invitedOrg,
		},
		Properties: []*userProperty{stubProperty("1", "123"), stubProperty("2", "123")},
	}

	testCases := []struct {
		path       []string
		template   string
		model      interface{}
		selector   string
		enterprise *bool
		budget     *string
		matches    []string
	}{
		{
			path:     []string{common.ErrorEndpoint, "404"},
			template: errorTemplate,
			model:    &errorRenderContext{ErrorCode: 404, ErrorMessage: http.StatusText(404)},
			selector: "",
			matches:  []string{},
		},
		{
			path:     []string{common.LoginEndpoint},
			template: loginTemplate,
			model: &loginRenderContext{
				CsrfRenderContext:    stubToken(),
				CaptchaRenderContext: CaptchaRenderContext{},
				Email:                "foo@bar.com",
				EmailError:           "Something is wrong",
				CodeError:            "Code is not OK",
				NameError:            "Name is no good",
				CanRegister:          true,
				IsRegister:           false,
			},
			selector: "",
			matches:  []string{},
		},
		{
			path:     []string{common.LoginEndpoint},
			template: twofactorContentsTemplate,
			model:    &loginRenderContext{CsrfRenderContext: stubToken(), Email: "foo@bar.com"},
		},
		{
			path:     []string{common.RegisterEndpoint},
			template: loginTemplate,
			model:    &loginRenderContext{CsrfRenderContext: stubToken(), Email: "foo@bar.com", IsRegister: true},
		},
		// technically this is not needed (copy of the above), but it's an insurance against typos in case IsRegister will change
		{
			path:     []string{common.RegisterEndpoint},
			template: registerContentsTemplate,
			model:    &loginRenderContext{CsrfRenderContext: stubToken(), IsRegister: true},
		},
		{
			path:     []string{common.OrgEndpoint, common.NewEndpoint},
			template: orgWizardTemplate,
			model:    &orgWizardRenderContext{CsrfRenderContext: stubToken(), NameError: "Name is no good"},
			selector: "",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123"},
			template: portalTemplate,
			model: &orgDashboardRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					Orgs:       []*UserOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
					CurrentOrg: stubOrgEx("123", dbgen.AccessLevelOwner),
				},
				Properties: []*userProperty{stubProperty("1", "123"), stubProperty("2", "123")},
				PaginationRenderContext: PaginationRenderContext{
					From:    1,
					To:      10,
					Count:   123,
					Page:    2,
					PerPage: 10,
				},
			},
			selector: "p.property-name",
			matches:  []string{"1", "2"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", "property-order"},
			template: portalTemplate,
			model: &orgDashboardRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					CurrentOrg: stubOrg("123"),
				},
				Properties: []*userProperty{
					stubProperty("Zulu", "123"),
					stubProperty("Middle", "123"),
					stubProperty("Alpha", "123"),
				},
				Sort: db.OrgPropertiesSortNameDescending,
			},
			selector: "p.property-name",
			matches:  []string{"Zulu", "Middle", "Alpha"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", "search-safe-data"},
			template: portalTemplate,
			model:    hostileOrgModel,
			selector: `[aria-labelledby="search-modal-title"][data-org-name="'-alert(1)-'"][x-init="$store.search.initialize($el.dataset.orgId, $el.dataset.orgName)"] h2`,
			matches:  []string{""},
		},
		{
			path:     []string{common.OrgEndpoint, "123", "search-preview-results"},
			template: portalTemplate,
			model:    previewModel,
			selector: "#searchResults li p",
			matches:  []string{"Searchable Property", "search-domain.example.com", "Searchable Form", "https://hooks.example.com/search-target"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", "search-preview-pagination"},
			template: portalTemplate,
			model:    previewModel,
			selector: "#searchMore button.pc-form-link",
			matches:  []string{"Show more"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.SearchEndpoint},
			template: orgSearchTemplate,
			model: &OrgSearchRenderContext{
				CurrentOrg: stubOrg("123"),
				SearchResults: []*OrgSearchResult{
					{ID: "property-1", Type: "property", Name: "Searchable Property", Description: "search-domain.example.com"},
				},
			},
			selector: "li p",
			matches:  []string{"Searchable Property", "search-domain.example.com"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.SearchEndpoint},
			template: orgSearchTemplate,
			model: &OrgSearchRenderContext{
				CurrentOrg: stubOrg("123"),
				SearchResults: []*OrgSearchResult{
					{ID: "form-1", Type: "form", Name: "Searchable Form", Description: "https://hooks.example.com/search-target"},
				},
			},
			selector: "li p",
			matches:  []string{"Searchable Form", "https://hooks.example.com/search-target"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.SearchEndpoint, "more-link"},
			template: orgSearchTemplate,
			model:    moreSearchModel,
			selector: "#searchMore button.pc-form-link",
			matches:  []string{"Show more"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.SearchEndpoint, "more-oob"},
			template: orgSearchTemplate,
			model:    moreSearchModel,
			selector: `#searchMore[hx-swap-oob="innerHTML"] button`,
			matches:  []string{"Show more"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.SearchEndpoint, "more-append"},
			template: orgSearchTemplate,
			model:    moreSearchModel,
			selector: `#searchMore button[hx-target="#searchResults > li:last-child"][hx-swap="afterend"]`,
			matches:  []string{"Show more"},
		},
		// same as above, but when Invited, we don't show properties
		{
			path:     []string{common.OrgEndpoint, "123"},
			template: portalTemplate,
			model:    invitedModel,
			selector: "p.property-name",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", "invited-search-trigger"},
			template: portalTemplate,
			model:    invitedModel,
			selector: "button.pc-setup-button",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", "invited-search-modal"},
			template: portalTemplate,
			model:    invitedModel,
			selector: `[aria-labelledby="search-modal-title"]`,
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", "invited-org-switch"},
			template: portalTemplate,
			model:    invitedModel,
			selector: `a[href$="/org/456"]`,
			matches:  []string{"My Org 456"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertiesEndpoint},
			template: orgPropertiesTemplate,
			model: &orgPropertiesRenderContext{
				PaginationRenderContext: PaginationRenderContext{From: 1, To: 1, Count: 2, Page: 0, PerPage: 30},
				CurrentOrg:              stubOrg("123"),
				Properties:              []*userProperty{stubProperty("Property", "123")},
				Sort:                    db.OrgPropertiesSortNameDescending,
			},
			selector: "button[hx-vals*=\"name_desc\"]",
			matches:  []string{"Previous", "Next"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.TabEndpoint, common.ReportsEndpoint},
			template: orgReportsTemplate,
			model: &portalBaseRenderContext{
				CurrentOrg: stubOrg("123"),
			},
			selector: `#orgChart[data-stats-url="/org/123/stats"]`,
			matches:  []string{""},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.TabEndpoint, common.MembersEndpoint},
			template: orgMembersTemplate,
			model: &orgMemberRenderContext{
				AlertRenderContext: AlertRenderContext{
					SuccessMessage: "Test",
				},
				portalBaseRenderContext: portalBaseRenderContext{
					CurrentOrg:        stubOrg("123"),
					CsrfRenderContext: stubToken(),
					CanEdit:           true,
				},
				Members: []*orgUser{stubUser("foo", dbgen.AccessLevelMember), stubUser("bar", dbgen.AccessLevelInvited)},
			},
			selector: "p.member-name",
			matches:  []string{"foo", "bar"},
		},
		{
			path:     []string{common.OrgEndpoint, "123"},
			template: portalTemplate,
			model: &orgFormsRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					Orgs:       []*UserOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
					CurrentOrg: stubOrgEx("123", dbgen.AccessLevelOwner),
					Tab:        1,
				},
				Forms: []*userForm{},
			},
			selector: "a[href=\"/org/123/form/new\"]",
			matches:  []string{"Add New Form"},
		},
		{
			path:     []string{common.OrgEndpoint, "123"},
			template: portalTemplate,
			model: &orgFormsRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					Orgs:       []*UserOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
					CurrentOrg: stubOrgEx("123", dbgen.AccessLevelOwner),
					Tab:        1,
				},
				Forms: []*userForm{stubForm("Newsletter Signup", "123"), stubForm("Contact Us", "123")},
			},
			selector: "span.form-name",
			matches:  []string{"Newsletter Signup", "Contact Us"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormsEndpoint},
			template: orgFormsListTemplate,
			model: &orgFormsRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					CurrentOrg: stubOrgEx("123", dbgen.AccessLevelOwner),
				},
				PaginationRenderContext: PaginationRenderContext{
					From:    1,
					To:      2,
					Count:   2,
					Page:    0,
					PerPage: 30,
				},
				Forms: []*userForm{stubForm("Newsletter Signup", "123"), stubForm("Contact Us", "123")},
			},
			selector: "span.form-name",
			matches:  []string{"Newsletter Signup", "Contact Us"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormsEndpoint, "pagination"},
			template: orgFormsListTemplate,
			model:    formsPaginationModel,
			selector: `button[hx-target="#forms"]`,
			matches:  []string{"Previous", "Next"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormsEndpoint, "pagination-targets"},
			template: orgFormsListTemplate,
			model:    formsPaginationModel,
			selector: `button[hx-target="#forms"]:not([hx-get="/org/123/forms"])`,
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormsEndpoint, "partial"},
			template: orgFormsListTemplate,
			model:    formsPaginationModel,
			selector: `label[for="org-tabs-select"], #org-tabs-select`,
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormsEndpoint, "empty"},
			template: orgFormsTemplate,
			model: &orgFormsRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					CurrentOrg: stubOrg("123"),
				},
			},
			selector: `h1.pc-page-title, a[href="/org/123/form/new"]`,
			matches:  []string{"No forms", "Add New Form"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormsEndpoint, "populated"},
			template: orgFormsTemplate,
			model:    formsPaginationModel,
			selector: `span.form-name, .pc-property-card p.truncate, h1.pc-page-title`,
			matches:  []string{"Newsletter Signup", "hooks.example.com/submit"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.TabEndpoint, common.SettingsEndpoint},
			template: orgSettingsTemplate,
			model: &orgSettingsRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					CurrentOrg:        stubOrg("123"),
					CsrfRenderContext: stubToken(),
					CanEdit:           true,
				},
				Members: []*orgUser{stubUser("foo", dbgen.AccessLevelMember), stubUser("bar", dbgen.AccessLevelInvited)},
			},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.TabEndpoint, common.EventsEndpoint},
			template: orgAuditLogsTemplate,
			model: &orgAuditLogsRenderContext{
				AuditLogsRenderContext: AuditLogsRenderContext{
					AuditLogs: stubAuditLogs(),
					Count:     12345,
					Page:      10,
					PerPage:   25,
				},
				portalBaseRenderContext: portalBaseRenderContext{
					CurrentOrg: stubOrg("123"),
				},
				CanView: true,
			},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, common.NewEndpoint},
			template: propertyWizardTemplate,
			model:    &propertyWizardRenderContext{CurrentOrg: stubOrg("123"), CsrfRenderContext: stubToken(), NameError: "Name error"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, common.NewEndpoint},
			template: formWizardTemplate,
			model:    &formWizardRenderContext{CurrentOrg: stubOrg("123"), CsrfRenderContext: stubToken(), NameError: "Name error"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, common.NewEndpoint, "steps"},
			template: formWizardTemplate,
			model:    formWizardModel,
			selector: `.pc-wizard-step-label-current, .pc-wizard-step-label-upcoming`,
			matches:  []string{"Create new form proxy", "Website integration"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, common.NewEndpoint, "url-and-cancel"},
			template: formWizardTemplate,
			model:    formWizardModel,
			selector: `input[type="url"][name="url"], a.pc-internal-form-button[href="/org/123?tab=forms"]`,
			matches:  []string{"", "Cancel"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "1", "tabs"},
			template: formDashboardTemplate,
			model:    formDashboardModel,
			selector: `#tabs option, #form-tabs a.pc-tab`,
			matches:  []string{"Reports", "Integrations", "Settings", "Audit logs", "Reports", "Integrations", "Settings", "Audit logs"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "1", "reports-heading"},
			template: formDashboardTemplate,
			model:    formDashboardModel,
			selector: `#form-tabs p.text-base.font-bold`,
			matches:  []string{"Form Requests"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "1", "logout"},
			template: formDashboardTemplate,
			model:    formDashboardModel,
			selector: `button[type="button"][hx-post="/logout"][hx-swap="none"], a[href="/logout"], form[action="/logout"]`,
			matches:  []string{"Sign out", "Sign out"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "1", "csrf-header"},
			template: formDashboardTemplate,
			model:    formDashboardModel,
			selector: fmt.Sprintf(`body[hx-headers='{"%s": "token"}'] #chart`, common.HeaderCSRFToken),
			matches:  []string{""},
		},
		{
			path:     []string{common.ErrorEndpoint, "500", "logout"},
			template: "errors/header-signed-in",
			model:    &errorRenderContext{CsrfRenderContext: stubToken()},
			selector: `button[type="button"][hx-post="/logout"][hx-swap="none"], a[href="/logout"], form[action="/logout"]`,
			matches:  []string{"Sign out", "Sign out"},
		},
		{
			path:     []string{common.ErrorEndpoint, "500", "csrf-header"},
			template: errorTemplate,
			model: &errorRenderContext{
				CsrfRenderContext: stubToken(),
				ErrorCode:         http.StatusInternalServerError,
				ErrorMessage:      http.StatusText(http.StatusInternalServerError),
			},
			selector: fmt.Sprintf(`body[hx-headers='{"%s": "token"}'] h1`, common.HeaderCSRFToken),
			matches:  []string{http.StatusText(http.StatusInternalServerError)},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "1", common.EventsEndpoint},
			template: formDashboardAuditLogsTemplate,
			model:    formAuditLogsModel,
			selector: `.pc-tab-active, a[href="/auditlogs"]`,
			matches:  []string{"Audit logs", "See all Audit Logs"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "456", common.TestEndpoint},
			template: formTestTemplate,
			model: &formSettingsRenderContext{
				formDashboardRenderContext: formDashboardRenderContext{
					CsrfRenderContext: stubToken(),
					Form:              stubForm("Contact", "123"),
					Org:               stubOrg("123"),
					CanEdit:           true,
				},
				TestBody: "email=test@example.com",
			},
			selector: "input[type=\"hidden\"][name=\"url\"], input[type=\"hidden\"][name=\"method\"]",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "1", common.TabEndpoint, common.ReportsEndpoint},
			template: formDashboardReportsTemplate,
			model: &formDashboardRenderContext{
				Form: stubForm("Contact", "123"),
				Org:  stubOrg("123"),
			},
			selector: `#chart[data-stats-url="/org/123/form/1/stats"]`,
			matches:  []string{""},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardTemplate,
			model: &propertyDashboardRenderContext{
				CsrfRenderContext: stubToken(),
				Property:          stubProperty("Foo", "123"),
				Org:               stubOrg("123"),
				CanEdit:           true,
			},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "1", common.TabEndpoint, common.ReportsEndpoint},
			template: propertyDashboardReportsTemplate,
			model: &propertyDashboardRenderContext{
				Property:     stubProperty("Foo", "123"),
				Org:          stubOrg("123"),
				IncludeRules: true,
			},
			selector:   `#chart[data-stats-url="/org/123/property/1/stats"], #ruleChart[data-stats-url="/org/123/property/1/rulestats"]`,
			matches:    []string{"", ""},
			enterprise: enterpriseOnly,
		},
		// same as above, but property integrations _template_
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardIntegrationsTemplate,
			model: &propertyIntegrationsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					CsrfRenderContext: stubToken(),
					Property:          blakeProperty,
					Org:               stubOrg("123"),
					CanEdit:           true,
				},
				Sitekey: "qwerty",
			},
			selector: "#snippet",
			matches: []string{`<!-- Add this inside the <head> of your website -->
<script defer src="https:/widget/js/privatecaptcha.js"></script>

<!-- Add this inside your form -->
<div class="private-captcha" data-sitekey="qwerty"></div>`},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.IntegrationsEndpoint},
			template: propertyDashboardIntegrationsTemplate,
			model: &propertyIntegrationsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					CsrfRenderContext: stubToken(),
					Property:          argonProperty,
					Org:               stubOrg("123"),
					CanEdit:           true,
				},
				Sitekey: "qwerty",
			},
			selector: "#snippet",
			matches: []string{`<!-- Add this inside the <head> of your website -->
<script defer src="https:/widget/js/privatecaptcha.js?v=ext"></script>

<!-- Add this inside your form -->
<div class="private-captcha" data-sitekey="qwerty"></div>`},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "456", common.TabEndpoint, common.IntegrationsEndpoint},
			template: formDashboardIntegrationsTemplate,
			model: &formDashboardIntegrationsRenderContext{
				formDashboardRenderContext: formDashboardRenderContext{Form: integrationForm, Org: stubOrg("123")},
				Sitekey:                    "qwerty",
				Challenge:                  string(dbgen.ChallengeTypeBlake2b),
			},
			selector: "#snippet",
			matches: []string{`<!-- Add this inside the <head> of your website -->
<script defer src="https:/widget/js/privatecaptcha.js"></script>

<!-- Use this instead of your existing form -->
<form method="POST" action="https:/form/form-uuid">
  <!-- Existing form fields... -->
  <div class="private-captcha" data-sitekey="qwerty"></div>
  <!-- <input type="submit" disabled /> -->
</form>`},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "456", common.IntegrationsEndpoint, "sections"},
			template: formDashboardIntegrationsTemplate,
			model:    formIntegrationsModel,
			selector: `h3.pc-subsection-title`,
			matches:  []string{"Form proxy snippet", "HTML/JS Snippet"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.FormEndpoint, "456", common.TabEndpoint, common.IntegrationsEndpoint},
			template: formDashboardIntegrationsTemplate,
			model: &formDashboardIntegrationsRenderContext{
				formDashboardRenderContext: formDashboardRenderContext{Form: integrationForm, Org: stubOrg("123")},
				Sitekey:                    "qwerty",
				Challenge:                  string(dbgen.ChallengeTypeArgon2ID),
			},
			selector: "#snippet",
			matches: []string{`<!-- Add this inside the <head> of your website -->
<script defer src="https:/widget/js/privatecaptcha.js?v=ext"></script>

<!-- Use this instead of your existing form -->
<form method="POST" action="https:/form/form-uuid">
  <!-- Existing form fields... -->
  <div class="private-captcha" data-sitekey="qwerty"></div>
  <!-- <input type="submit" disabled /> -->
</form>`},
		},
		// same as above, but client setup wizard step
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.ClientSetupEndpoint},
			template: propertyWizardClientSetupTemplate,
			model: &propertyIntegrationsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					CsrfRenderContext: stubToken(),
					Property:          stubProperty("Foo", "123"),
					Org:               stubOrg("123"),
					CanEdit:           true,
				},
				Sitekey: "qwerty",
			},
		},
		// same as above, but server setup wizard step
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.ServerSetupEndpoint},
			template: propertyWizardServerSetupTemplate,
			model: &propertyIntegrationsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					CsrfRenderContext: stubToken(),
					Property:          stubProperty("Foo", "123"),
					Org:               stubOrg("123"),
					CanEdit:           true,
				},
				Sitekey: "qwerty",
			},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.EditEndpoint},
			template: propertySettingsBasicFormTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					Property: edgeProperty, Org: stubOrg("123"), CanEdit: true,
					AlertRenderContext: AlertRenderContext{SuccessMessage: "Settings were updated"},
				},
				difficultyLevelsRenderContext: createDifficultyLevelsRenderContext(),
			},
			selector: `form[data-unsaved-changes-key="property-settings"][data-unsaved-changes-initial="clean"] div.col-span-full:first-child p`,
			matches:  []string{"Settings were updated"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.EdgeEndpoint},
			template: propertySettingsEdgeFormTemplate,
			model: &edgePropertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					Property: edgeProperty, Org: stubOrg("123"), CanEdit: true,
					AlertRenderContext: AlertRenderContext{ErrorMessage: "Invalid edge widget start mode."},
				},
				EdgeWidgetStartMode: "load",
			},
			selector: `form[data-unsaved-changes-key="property-edge-settings"][data-unsaved-changes-initial="dirty"] div.col-span-full:first-child p`,
			matches:  []string{"Invalid edge widget start mode."},
		},
		// same as above, but property settings _template_
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: edgeProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			selector:   `form[hx-put="/org/123/property/1/edge"] label`,
			matches:    []string{"Verified access duration", "Widget start mode"},
			enterprise: enterpriseOnly,
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: edgeProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			selector:   `form[data-unsaved-changes-key="property-edge-settings"]`,
			enterprise: new(false),
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: edgeProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
				EdgeWidgetStartMode:            "load",
			},
			selector:   `form[data-unsaved-changes-key="property-edge-settings"] select[name="edge_widget_start_mode"] option[selected]`,
			matches:    []string{"On page load"},
			enterprise: enterpriseOnly,
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: edgeProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			selector: `div.pc-advanced-settings > h4 > button > span:first-child`,
			matches:  []string{"Advanced"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: &userProperty{Domain: "any domain (*)", HasDomain: false}, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			selector: `div.pc-advanced-settings > h4 > button > span:first-child`,
			matches:  []string{"Advanced"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: edgeProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			selector:   `select[name="edge_token_validity_interval"] option[selected]`,
			matches:    []string{"1 hour"},
			enterprise: enterpriseOnly,
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: blakeProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			selector: `select[name="challenge"] option[selected]`,
			matches:  []string{"Compute-hard (default)"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: argonProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			selector: `select[name="challenge"] option[selected]`,
			matches:  []string{"Memory-hard (experimental)"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: argonProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			budget:   &zeroBudget,
			selector: `select[name="challenge"] option[value="argon2id"][disabled]`,
			matches:  []string{"Memory-hard (disabled)"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.TabEndpoint, common.SettingsEndpoint},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{Property: blakeProperty, Org: stubOrg("123"), CanEdit: true},
				difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
			},
			budget:   &missingBudget,
			selector: `select[name="challenge"] option[selected]`,
			matches:  []string{"Compute-hard (default)"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardSettingsTemplate,
			model: &propertySettingsRenderContext{
				difficultyLevelsRenderContext: createDifficultyLevelsRenderContext(),
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					AlertRenderContext: AlertRenderContext{
						SuccessMessage: "Test",
					},
					CsrfRenderContext: stubToken(),
					Property:          stubProperty("Foo", "123"),
					Org:               stubOrg("123"),
					CanEdit:           true,
					NameError:         common.StatusPropertyNameEmptyError.String(),
				},
				Orgs:     []*UserOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
				MinLevel: int(common.MinDifficultyLevel),
				MaxLevel: int(common.MaxDifficultyLevel),
			},
		},
		// same as above, but property audit logs _template_
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardAuditLogsTemplate,
			model: &propertyAuditLogsRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					AlertRenderContext: AlertRenderContext{
						SuccessMessage: "Test",
					},
					CsrfRenderContext: stubToken(),
					Property:          stubProperty("Foo", "123"),
					Org:               stubOrg("123"),
					CanEdit:           true,
				},
				AuditLogsRenderContext: AuditLogsRenderContext{
					AuditLogs: stubAuditLogs(),
					Count:     12345,
					Page:      10,
					PerPage:   25,
				},
			},
		},
		{
			path:     []string{common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint},
			template: settingsGeneralTemplatePrefix + "page.html",
			model: &settingsGeneralRenderContext{
				SettingsCommonRenderContext: SettingsCommonRenderContext{
					AlertRenderContext: AlertRenderContext{
						SuccessMessage: "Test",
					},
					CsrfRenderContext: stubToken(),
					Email:             "foo@bar.com",
					ActiveTabID:       common.GeneralEndpoint,
					Tabs:              CreateTabViewModels(common.GeneralEndpoint, server.SettingsTabs),
				},
				Name:       "User",
				EmailError: "Email error",
				NameError:  "Name error",
			},
		},
		{
			path:     []string{common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint},
			template: settingsAPIKeysTemplatePrefix + "page.html",
			model: &settingsAPIKeysRenderContext{
				SettingsCommonRenderContext: SettingsCommonRenderContext{
					CsrfRenderContext: stubToken(),
					AlertRenderContext: AlertRenderContext{
						WarningMessage: "Test warning!",
					},
					Email:       "foo@bar.com",
					ActiveTabID: common.APIKeysEndpoint,
					Tabs:        CreateTabViewModels(common.APIKeysEndpoint, server.SettingsTabs),
				},
				Keys:       []*userAPIKey{stubAPIKey("foo"), stubAPIKey("bar")},
				Orgs:       []*UserOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
				CreateOpen: false,
			},
			selector: "p.apikey-name",
			matches:  []string{"foo", "bar"},
		},
		{
			path: []string{common.SettingsEndpoint, common.TabEndpoint, common.UsageEndpoint},
			// NOTE: we use "tab" here instead of "page" because of <script> text and JS that breaks XML parser
			template: settingsUsageTemplatePrefix + "tab.html",
			model: &settingsUsageRenderContext{
				SettingsCommonRenderContext: SettingsCommonRenderContext{
					CsrfRenderContext: stubToken(),
					AlertRenderContext: AlertRenderContext{
						WarningMessage: "Test warning!",
					},
					Email:       "foo@bar.com",
					ActiveTabID: common.UsageEndpoint,
					Tabs:        CreateTabViewModels(common.UsageEndpoint, server.SettingsTabs),
				},
				OrgsCount:               2,
				PropertiesCount:         10,
				IncludedOrgsCount:       10,
				IncludedPropertiesCount: 50,
				Limit:                   12345,
			},
			selector: `#usage-chart[data-stats-url="/user/stats"]`,
			matches:  []string{""},
		},
		{
			path:     []string{common.SettingsEndpoint, common.UsageEndpoint, "organization-headings"},
			template: settingsUsageTemplatePrefix + "tab.html",
			model:    usageStatsModel,
			selector: `table.min-w-full.border.border-pc-grey-250 thead tr.border-b.border-dashed.border-pc-grey-250.bg-pc-blue-50 th`,
			matches:  []string{"Organization", "Members", "Properties", "Forms", "Rules"},
		},
		{
			path:     []string{common.SettingsEndpoint, common.UsageEndpoint, "organization-link"},
			template: settingsUsageTemplatePrefix + "tab.html",
			model:    usageStatsModel,
			selector: `table a.pc-docs-link.underline.hover\:text-pc-green-hover[href="/org/123"]`,
			matches:  []string{"My Org 123"},
		},
		{
			path:     []string{common.SettingsEndpoint, common.TabEndpoint, common.NotificationsEndpoint},
			template: settingsNotificationsTemplatePrefix + "page.html",
			model: &settingsNotificationsRenderContext{
				SettingsCommonRenderContext: SettingsCommonRenderContext{
					CsrfRenderContext: stubToken(),
					Email:             "foo@bar.com",
					ActiveTabID:       common.NotificationsEndpoint,
					Tabs:              CreateTabViewModels(common.NotificationsEndpoint, server.SettingsTabs),
				},
				WeeklyReport:  true,
				MonthlyReport: false,
				ReportEmail:   "reports@example.com",
			},
			selector: "",
			matches:  []string{},
		},
		{
			path:     []string{common.AuditLogsEndpoint},
			template: auditLogsTemplate,
			model: &MainAuditLogsRenderContext{
				CsrfRenderContext: stubToken(),
				AuditLogsRenderContext: AuditLogsRenderContext{
					AuditLogs: stubAuditLogs(),
					Count:     12345,
					Page:      10,
					PerPage:   25,
				},
				From: 1,
				To:   10,
				Days: 365,
			},
			// TODO: Add selector tests for audit logs
			selector: "",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.RulesEndpoint, common.NewEndpoint},
			template: ruleTemplate,
			model: &RuleWizardRenderContext{
				CsrfRenderContext:  stubToken(),
				AlertRenderContext: AlertRenderContext{},
				RuleFormData: RuleFormData{
					Name:              "Name",
					NameError:         "Name not good",
					ConditionProperty: string(dbgen.RuleConditionPropertyCountryCode),
					ConditionOperator: string(dbgen.RuleConditionOperatorIn),
					ConditionValue:    "US",
					ActionProperty:    string(dbgen.RuleActionPropertyDifficultyGrowth),
					ActionValue:       string(dbgen.DifficultyGrowthFast),
					Enabled:           true,
					ConditionNegated:  false,
				},
				CurrentOrg: stubOrg("123"),
				Property:   stubProperty("my property", "123"),
				Countries:  []CountryOption{},
			},
			selector: "",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.RulesEndpoint, common.NewEndpoint},
			template: ruleTemplate,
			model: &RuleWizardRenderContext{
				CsrfRenderContext:  stubToken(),
				AlertRenderContext: AlertRenderContext{},
				RuleFormData: RuleFormData{
					Name:              "Name",
					ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
					ConditionOperator: string(dbgen.RuleConditionOperatorContains),
					ConditionValue:    "curl",
					ActionProperty:    string(dbgen.RuleActionPropertyDifficultyGrowth),
					ActionValue:       string(dbgen.DifficultyGrowthFast),
					Enabled:           true,
				},
				CurrentOrg: stubOrg("123"),
				Property: &userProperty{
					ID:        "456",
					OrgID:     "123",
					Name:      "No Domain",
					Domain:    "any domain (*)",
					HasDomain: false,
					Enabled:   true,
				},
				Countries: []CountryOption{},
			},
			selector: "option[value=\"domain\"]",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.RulesEndpoint, "789", common.EditEndpoint},
			template: ruleTemplate,
			model: &RuleWizardRenderContext{
				CsrfRenderContext:  stubToken(),
				AlertRenderContext: AlertRenderContext{},
				RuleFormData: RuleFormData{
					Name:              "Existing Rule",
					ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
					ConditionOperator: string(dbgen.RuleConditionOperatorContains),
					ConditionValue:    "curl",
					ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
					ActionValue:       "50",
					Enabled:           true,
					ConditionNegated:  false,
				},
				CurrentOrg: stubOrg("123"),
				Property:   stubProperty("my property", "123"),
				Countries:  []CountryOption{},
				RuleID:     "789",
				IsEdit:     true,
			},
			selector: "",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.RulesEndpoint, "browser-version", common.EditEndpoint},
			template: ruleTemplate,
			model: &RuleWizardRenderContext{
				CsrfRenderContext: stubToken(),
				RuleFormData: RuleFormData{
					Name:              "Outdated browser",
					ConditionProperty: string(dbgen.RuleConditionPropertyBrowserVersion),
					ConditionOperator: string(dbgen.RuleConditionOperatorMore),
					ConditionValue:    "7",
					ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
					ActionValue:       "50",
					Enabled:           true,
				},
				CurrentOrg: stubOrg("123"),
				Property:   stubProperty("my property", "123"),
				IsEdit:     true,
			},
			selector: `option[value="browser_version"][selected], label[for="browserVersionInput"], input#browserVersionInput[type="number"][min="1"][step="1"][aria-describedby="browserVersionInput-description"], #browserVersionInput-description`,
			matches: []string{
				"Browser version",
				"Major versions behind",
				"",
				"Matches supported browser versions more than this many major versions behind the latest release.",
			},
			enterprise: enterpriseOnly,
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456", common.RulesEndpoint, "edge-validity", common.EditEndpoint},
			template: ruleTemplate,
			model: &RuleWizardRenderContext{
				CsrfRenderContext: stubToken(),
				RuleFormData: RuleFormData{
					Name:              "Verified access",
					ConditionProperty: string(dbgen.RuleConditionPropertyAlways),
					ActionProperty:    string(dbgen.RuleActionPropertyEdgeTokenValidityInterval),
					ActionValue:       "4",
					Enabled:           true,
				},
				CurrentOrg: stubOrg("123"),
				Property:   edgeProperty,
				IsEdit:     true,
			},
			selector: `option[value="edge_token_validity_interval"][selected], label[for="edge_validity_select"], select#edge_validity_select[x-model="actionValueEdgeTokenValidity"] option[value="0"], select#edge_validity_select option[value="1"], select#edge_validity_select option[value="2"], select#edge_validity_select option[value="3"], select#edge_validity_select option[value="4"], select#edge_validity_select option[value="5"], select#edge_validity_select option[value="6"], select#edge_validity_select option[value="7"]`,
			matches: []string{
				"Change Verified Access Duration", "Verified access duration", "Disabled",
				"5 minutes", "10 minutes", "30 minutes", "1 hour", "6 hours", "12 hours", "1 day",
			},
			enterprise: enterpriseOnly,
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.RulesEndpoint},
			template: orgRulesTemplate,
			model: &OrgRulesRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					Orgs:       []*UserOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
					CurrentOrg: stubOrgEx("123", dbgen.AccessLevelOwner),
					CanEdit:    true,
				},
				AlertRenderContext: AlertRenderContext{},
				rulesRenderContext: rulesRenderContext{
					Rules:     stubDifficultyRules(),
					CanAddNew: true,
				},
			},
			selector:   "p.rule-name",
			matches:    ruleNames(stubDifficultyRules()),
			enterprise: enterpriseOnly,
		},
		// same as above but empty rules to check for placeholder (also doubles for enterprise and non-enterprise)
		{
			path:     []string{common.OrgEndpoint, "000", common.RulesEndpoint},
			template: orgRulesTemplate,
			model: &OrgRulesRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					Orgs:       []*UserOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
					CurrentOrg: stubOrgEx("123", dbgen.AccessLevelOwner),
					CanEdit:    true,
				},
				AlertRenderContext: AlertRenderContext{},
				rulesRenderContext: rulesRenderContext{
					Rules:     []*DifficultyRuleModel{},
					CanAddNew: true,
				},
			},
			selector: "p.rule-name",
			matches:  []string{},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertiesEndpoint, "123", common.RulesEndpoint},
			template: propertyDashboardRulesTemplate,
			model: &PropertyRulesRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					CsrfRenderContext: stubToken(),
					Property:          stubProperty("Foo", "123"),
					Org:               stubOrg("123"),
					CanEdit:           true,
				},
				rulesRenderContext: rulesRenderContext{
					Rules:     stubDifficultyRules(),
					CanAddNew: true,
				},
			},
			selector:   "p.rule-name",
			matches:    ruleNames(stubDifficultyRules()),
			enterprise: enterpriseOnly,
		},
		// same as above but empty rules to check for placeholder (also doubles for enterprise and non-enterprise)
		{
			path:     []string{common.OrgEndpoint, "000", common.PropertiesEndpoint, "000", common.RulesEndpoint},
			template: propertyDashboardRulesTemplate,
			model: &PropertyRulesRenderContext{
				propertyDashboardRenderContext: propertyDashboardRenderContext{
					CsrfRenderContext: stubToken(),
					Property:          stubProperty("Foo", "123"),
					Org:               stubOrg("123"),
					CanEdit:           true,
				},
				rulesRenderContext: rulesRenderContext{
					Rules:     []*DifficultyRuleModel{},
					CanAddNew: true,
				},
			},
			selector: "p.rule-name",
			matches:  []string{},
		},
	}

	for _, tc := range testCases {
		enterpriseArray := make([]bool, 0, 2)
		if tc.enterprise != nil {
			enterpriseArray = append(enterpriseArray, *tc.enterprise)
		} else {
			enterpriseArray = append(enterpriseArray, false)
			enterpriseArray = append(enterpriseArray, true)
		}

		for _, enterprise := range enterpriseArray {
			version := "community"
			if enterprise {
				version = "enterprise"
			}

			t.Run(fmt.Sprintf("render-%s-%s", version, strings.Join(tc.path, "-")), func(t *testing.T) {
				budget := "256"
				if tc.budget != nil {
					budget = *tc.budget
				}
				platformCtx := &PlatformRenderContext{
					GitCommit:            "qwerty123",
					Enterprise:           enterprise,
					Argon2IDMemoryBudget: config.NewStaticValue(common.Argon2IDMemoryBudgetKey, budget),
					licenseService:       server.LicenseService,
					ShowChallengeType:    true,
				}

				path := server.RelURL(strings.Join(tc.path, "/"))
				buf, err := server.RenderResponse(t.Context(), tc.template, tc.model, &RequestContext{Path: server.RelURL(path)}, platformCtx)
				if err != nil {
					t.Fatal(err)
				}

				if len(tc.selector) > 0 {
					document := portal_tests.ParseHTML(t, buf)
					if tc.template == propertySettingsBasicFormTemplate || tc.template == propertySettingsEdgeFormTemplate {
						if document.Find("form").Length() != 1 || document.Find("form[hx-select], .pc-section-heading").Length() != 0 {
							t.Fatal("settings response must contain only the requested form")
						}
					}
					selection := document.Find(tc.selector)
					if len(tc.matches) != len(selection.Nodes) {
						t.Fatalf("Expected %v matches, but got %v", len(tc.matches), len(selection.Nodes))
					}
					for i, node := range selection.Nodes {
						nodeText := portal_tests.Text(node)
						if tc.matches[i] != nodeText {
							t.Errorf("Expected match %v at %v, but got %v", tc.matches[i], i, nodeText)
						}
					}
				} else {
					portal_tests.AssertWellFormedHTML(t, buf)
				}
			})
		}
	}
}
