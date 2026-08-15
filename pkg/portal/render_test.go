package portal

import (
	"fmt"
	randv2 "math/rand/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
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
	enterpriseOnly := new(bool)
	*enterpriseOnly = true
	hostileOrg := stubOrg("123")
	hostileOrg.Name = "'-alert(1)-'"
	previewOrg := stubOrg("123")
	invitedOrg := stubOrgEx("123", dbgen.AccessLevelInvited)
	memberOrg := stubOrgEx("456", dbgen.AccessLevelMember)
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
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardTemplate,
			model: &propertyDashboardRenderContext{
				CsrfRenderContext: stubToken(),
				Property:          stubProperty("Foo", "123"),
				Org:               stubOrg("123"),
				CanEdit:           true,
			},
		},
		// same as above, but property integrations _template_
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardIntegrationsTemplate,
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
		// same as above, but property settings _template_
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
			selector: "",
			matches:  []string{},
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
				"Matches Chrome or Firefox versions more than this many major versions behind the latest release.",
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

			t.Run(fmt.Sprintf("render_%s_%s", version, strings.Join(tc.path, "_")), func(t *testing.T) {
				platformCtx := &PlatformRenderContext{
					GitCommit:      "qwerty123",
					Enterprise:     enterprise,
					licenseService: server.LicenseService,
				}

				path := server.RelURL(strings.Join(tc.path, "/"))
				buf, err := server.RenderResponse(t.Context(), tc.template, tc.model, &RequestContext{Path: server.RelURL(path)}, platformCtx)
				if err != nil {
					t.Fatal(err)
				}

				if len(tc.selector) > 0 {
					document := portal_tests.ParseHTML(t, buf)
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
