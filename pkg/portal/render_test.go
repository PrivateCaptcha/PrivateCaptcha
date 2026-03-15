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

func stubOrgEx(orgID string, level dbgen.AccessLevel) *userOrg {
	return &userOrg{
		Name:  "My Org " + orgID,
		ID:    orgID,
		Level: string(level),
	}
}

func stubOrg(orgID string) *userOrg {
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
					Orgs:       []*userOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
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
		// same as above, but when Invited, we don't show properties
		{
			path:     []string{common.OrgEndpoint, "123"},
			template: portalTemplate,
			model: &orgDashboardRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					Orgs:       []*userOrg{stubOrgEx("123", dbgen.AccessLevelInvited)},
					CurrentOrg: stubOrgEx("123", dbgen.AccessLevelInvited),
				},
				Properties: []*userProperty{stubProperty("1", "123"), stubProperty("2", "123")},
			},
			selector: "p.property-name",
			matches:  []string{},
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
				Orgs:     []*userOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
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
				Orgs:       []*userOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
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
			path:     []string{common.OrgEndpoint, "123", common.RulesEndpoint},
			template: orgRulesTemplate,
			model: &OrgRulesRenderContext{
				portalBaseRenderContext: portalBaseRenderContext{
					Orgs:       []*userOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
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
					Orgs:       []*userOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
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
