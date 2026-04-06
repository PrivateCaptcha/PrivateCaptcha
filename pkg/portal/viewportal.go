//go:build enterprise

package portal

import (
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

// ViewPortalPage describes a single renderable portal page for the viewportal tool.
type ViewPortalPage struct {
	Path      string
	Template  string
	ModelFunc func(alert AlertRenderContext) interface{}
}

func viewStubOrg(id string) *UserOrg {
	return &UserOrg{Name: "Acme Corp", ID: id, Level: string(dbgen.AccessLevelOwner)}
}

func viewStubProperty(name, orgID string) *userProperty {
	return &userProperty{
		ID: "prop1", OrgID: orgID, Name: name, Domain: "example.com",
		Sitekey: "sk_test_abc123", Level: int(common.DifficultyLevelMedium),
		Growth: 2, ValidityInterval: 60, MaxReplayCount: 3,
		AllowSubdomains: true, AllowReplay: true, Enabled: true,
	}
}

func viewStubAuditLogs() []*UserAuditLog {
	actions := []dbgen.AuditLogAction{
		dbgen.AuditLogActionCreate, dbgen.AuditLogActionUpdate,
		dbgen.AuditLogActionDelete, dbgen.AuditLogActionAccess, dbgen.AuditLogActionLogin,
	}
	tables := []string{"properties", "organizations", "api_keys", "users", "org_users"}
	sources := []dbgen.AuditLogSource{dbgen.AuditLogSourcePortal, dbgen.AuditLogSourceApi}

	result := make([]*UserAuditLog, 0, len(actions))
	for i, action := range actions {
		result = append(result, &UserAuditLog{
			UserName: "Jane Doe", UserEmail: "jane@example.com",
			Action: string(action), Source: string(sources[i%len(sources)]),
			Property: "Main Site", Resource: "example.com",
			TableName: tables[i%len(tables)],
			Time:      time.Now().Add(-time.Duration(i*30) * time.Minute).Format(auditLogTimeFormat),
		})
	}
	return result
}

func viewStubRules() []*DifficultyRuleModel {
	return []*DifficultyRuleModel{
		{
			ID: "r1", Name: "Block suspicious countries",
			ConditionProperty: string(dbgen.RuleConditionPropertyCountryCode),
			ConditionOperator: string(dbgen.RuleConditionOperatorIn),
			ConditionValue:    "AB, CD, EF", ActionProperty: string(dbgen.RuleActionPropertyHTTPRequest),
			ActionValue: "0", Enabled: true, CanEdit: true, Terminal: true,
		},
		{
			ID: "r2", Name: "Lower difficulty for mobile",
			ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
			ConditionOperator: string(dbgen.RuleConditionOperatorContains),
			ConditionValue:    "Mobile", ActionProperty: string(dbgen.RuleActionPropertyDifficultyLevelPercent),
			ActionValue: "-20", Enabled: true, CanEdit: true,
		},
		{
			ID: "r3", Name: "Block bots",
			ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
			ConditionOperator: string(dbgen.RuleConditionOperatorBot),
			ActionProperty:    string(dbgen.RuleActionPropertyHTTPRequest),
			ActionValue:       "0", Enabled: false, CanEdit: true,
		},
	}
}

func viewStubAPIKey(name string) *userAPIKey {
	return &userAPIKey{
		ID: "k1", Name: name,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Format("02 Jan 2006"),
		Scope:     "puzzle", RequestsPerMinute: 60, OrgName: "Acme Corp",
		LastUsedAt: time.Now().Add(-2 * time.Hour).Format("02 Jan 2006 15:04"),
	}
}

// BuildViewPortalPages returns all portal page configurations for the viewportal tool.
func BuildViewPortalPages(settingsTabs []*SettingsTab) []ViewPortalPage {
	org := viewStubOrg("org1")
	orgs := []*UserOrg{org, {Name: "Other Org", ID: "org2", Level: string(dbgen.AccessLevelMember)}}
	prop := viewStubProperty("Main Site", "org1")
	token := CsrfRenderContext{Token: "stub-csrf-token"}
	rules := viewStubRules()
	auditLogs := viewStubAuditLogs()

	baseCtx := func(tab int) portalBaseRenderContext {
		return portalBaseRenderContext{
			CsrfRenderContext: token, Orgs: orgs, CurrentOrg: org, Tab: tab, CanEdit: true,
		}
	}

	captchaCtx := CaptchaRenderContext{
		CaptchaEndpoint: "/api/puzzle", CaptchaSolutionField: "portal-solution",
		CaptchaSitekey: "sk_test_abc123", CaptchaDebug: true,
	}

	propDash := func(tab int, alert AlertRenderContext) propertyDashboardRenderContext {
		return propertyDashboardRenderContext{
			AlertRenderContext: alert, CsrfRenderContext: token,
			CaptchaRenderContext: captchaCtx, Property: prop, Org: org,
			Tab: tab, CanEdit: true, IncludeRules: true,
		}
	}

	settingsCommon := func(activeTab string, alert AlertRenderContext) SettingsCommonRenderContext {
		return SettingsCommonRenderContext{
			AlertRenderContext: alert, CsrfRenderContext: token,
			Tabs: CreateTabViewModels(activeTab, settingsTabs), ActiveTabID: activeTab,
			Email: "jane@example.com", UserID: 1,
		}
	}

	auditLogsCtx := AuditLogsRenderContext{
		AuditLogs: auditLogs, Count: len(auditLogs), Page: 0, PerPage: perPageEventLogs,
	}

	pagination := PaginationRenderContext{From: 1, To: 2, Count: 2, Page: 0, PerPage: 30}

	members := []*orgUser{
		{Name: "Jane Doe", Email: "jane@example.com", ID: "u1", Level: string(dbgen.AccessLevelOwner), CreatedAt: "01 Jan 2025"},
		{Name: "John Smith", Email: "john@example.com", ID: "u2", Level: string(dbgen.AccessLevelMember), CreatedAt: "15 Mar 2025"},
		{Name: "invite@example.com", Email: "invite@example.com", ID: "u3", Level: string(dbgen.AccessLevelInvited), CreatedAt: "01 Apr 2025", IsEmailInvite: true},
	}

	countries := []CountryOption{
		{Code: "US", Name: "United States"}, {Code: "DE", Name: "Germany"}, {Code: "JP", Name: "Japan"},
	}

	return []ViewPortalPage{
		{
			Path: "/login", Template: loginTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &loginRenderContext{
					CsrfRenderContext: token, CaptchaRenderContext: captchaCtx,
					Email: "jane@example.com", CanRegister: true,
				}
			},
		},
		{
			Path: "/register", Template: loginTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &loginRenderContext{
					CsrfRenderContext: token, CaptchaRenderContext: captchaCtx,
					CanRegister: true, IsRegister: true,
				}
			},
		},
		{
			Path: "/error/{code}", Template: errorTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &errorRenderContext{
					CsrfRenderContext: token, ErrorCode: 404,
					ErrorMessage: "Not Found", Detail: "This page does not exist.",
				}
			},
		},
		{
			Path: "/expired", Template: errorTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &errorRenderContext{
					CsrfRenderContext: token, ErrorCode: 403,
					ErrorMessage: "Session expired", Detail: "Please begin again.",
				}
			},
		},
		{
			Path: "/org/new", Template: orgWizardTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &orgWizardRenderContext{CsrfRenderContext: token, AlertRenderContext: a}
			},
		},
		{
			Path: "/org/{org}", Template: portalTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &orgDashboardRenderContext{
					portalBaseRenderContext: baseCtx(0), PaginationRenderContext: pagination,
					Properties: []*userProperty{prop, viewStubProperty("Blog", "org1")},
				}
			},
		},
		{
			Path: "/org/{org}/tab/dashboard", Template: orgDashboardTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &orgDashboardRenderContext{
					portalBaseRenderContext: baseCtx(0), PaginationRenderContext: pagination,
					Properties: []*userProperty{prop, viewStubProperty("Blog", "org1")},
				}
			},
		},
		{
			Path: "/org/{org}/tab/members", Template: orgMembersTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &orgMemberRenderContext{
					portalBaseRenderContext: baseCtx(portalMembersTabIndex), AlertRenderContext: a, Members: members,
				}
			},
		},
		{
			Path: "/org/{org}/tab/settings", Template: orgSettingsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &orgSettingsRenderContext{
					portalBaseRenderContext: baseCtx(portalSettingsTabIndex), AlertRenderContext: a,
					Members: members[:2], CanTransfer: true,
				}
			},
		},
		{
			Path: "/org/{org}/tab/events", Template: orgAuditLogsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &orgAuditLogsRenderContext{
					portalBaseRenderContext: baseCtx(portalEventsTabIndex), AlertRenderContext: a,
					AuditLogsRenderContext: auditLogsCtx, CanView: true,
				}
			},
		},
		{
			Path: "/org/{org}/tab/rules", Template: orgRulesTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &OrgRulesRenderContext{
					portalBaseRenderContext: baseCtx(portalRulesTabIndex), AlertRenderContext: a,
					rulesRenderContext: rulesRenderContext{Rules: rules, CanAddNew: true},
				}
			},
		},
		{
			Path: "/org/{org}/properties", Template: orgPropertiesTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &orgPropertiesRenderContext{
					CsrfRenderContext: token, PaginationRenderContext: pagination,
					Properties: []*userProperty{prop, viewStubProperty("Blog", "org1")},
					CurrentOrg: org,
				}
			},
		},
		{
			Path: "/org/{org}/property/new", Template: propertyWizardTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &propertyWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a, CurrentOrg: org,
				}
			},
		},
		{
			Path: "/org/{org}/property/{property}", Template: propertyDashboardTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				c := propDash(propertyReportsTabIndex, a)
				return &c
			},
		},
		{
			Path: "/org/{org}/property/{property}/tab/reports", Template: propertyDashboardReportsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				c := propDash(propertyReportsTabIndex, a)
				return &c
			},
		},
		{
			Path: "/org/{org}/property/{property}/tab/settings", Template: propertyDashboardSettingsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &propertySettingsRenderContext{
					propertyDashboardRenderContext: propDash(propertySettingsTabIndex, a),
					difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
					Orgs:                           orgs, MinLevel: int(common.MinDifficultyLevel), MaxLevel: int(common.MaxDifficultyLevel),
					CanMove: true,
				}
			},
		},
		{
			Path: "/org/{org}/property/{property}/tab/integrations", Template: propertyDashboardIntegrationsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &propertyIntegrationsRenderContext{
					propertyDashboardRenderContext: propDash(propertyIntegrationsTabIndex, a),
					Sitekey:                        "sk_test_abc123",
				}
			},
		},
		{
			Path: "/org/{org}/property/{property}/tab/events", Template: propertyDashboardAuditLogsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &propertyAuditLogsRenderContext{
					propertyDashboardRenderContext: propDash(propertyAuditLogsTabIndex, a),
					AuditLogsRenderContext:         auditLogsCtx, CanView: true,
				}
			},
		},
		{
			Path: "/org/{org}/property/{property}/tab/rules", Template: propertyDashboardRulesTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &PropertyRulesRenderContext{
					propertyDashboardRenderContext: propDash(propertyRulesTabIndex, a),
					rulesRenderContext:             rulesRenderContext{Rules: rules, CanAddNew: true},
				}
			},
		},
		{
			Path: "/settings", Template: settingsGeneralTemplatePrefix + "page.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsGeneralRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.GeneralEndpoint, a), Name: "Jane Doe",
				}
			},
		},
		{
			Path: "/settings/tab/general", Template: settingsGeneralTemplatePrefix + "page.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsGeneralRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.GeneralEndpoint, a), Name: "Jane Doe",
				}
			},
		},
		{
			Path: "/settings/tab/apikeys", Template: settingsAPIKeysTemplatePrefix + "page.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsAPIKeysRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.APIKeysEndpoint, a),
					Keys:                        []*userAPIKey{viewStubAPIKey("Production Key"), viewStubAPIKey("Staging Key")},
					Orgs:                        orgs,
				}
			},
		},
		{
			Path: "/settings/tab/usage", Template: settingsUsageTemplatePrefix + "page.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsUsageRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.UsageEndpoint, a),
					PropertiesCount:             5, OrgsCount: 2, IncludedPropertiesCount: 50,
					IncludedOrgsCount: 10, Limit: 1000000,
				}
			},
		},
		{
			Path: "/settings/tab/notifications", Template: settingsNotificationsTemplatePrefix + "page.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsNotificationsRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.NotificationsEndpoint, a),
					WeeklyReport:                true, ReportEmail: "reports@example.com", UserEmail: "jane@example.com",
				}
			},
		},
		{
			Path: "/auditlogs", Template: auditLogsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &MainAuditLogsRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					AuditLogsRenderContext: auditLogsCtx,
					From:                   1, To: len(auditLogs), Days: 14,
				}
			},
		},
		{
			Path: "/org/{org}/rules/new", Template: ruleTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &RuleWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					RuleFormData: RuleFormData{
						ConditionProperty: string(dbgen.RuleConditionPropertyCountryCode),
						ConditionOperator: string(dbgen.RuleConditionOperatorIn),
						ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
						Enabled:           true,
					},
					Countries: countries, CurrentOrg: org,
				}
			},
		},
		{
			Path: "/org/{org}/property/{property}/rules/new", Template: ruleTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &RuleWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					RuleFormData: RuleFormData{
						ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
						ConditionOperator: string(dbgen.RuleConditionOperatorContains),
						ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
						Enabled:           true,
					},
					Countries: countries, CurrentOrg: org, Property: viewStubProperty("Main Site", "org1"),
				}
			},
		},
		{
			Path: "/org/{org}/rules/{rule}/edit", Template: ruleTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &RuleWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					RuleFormData: RuleFormData{
						Name:              "Block suspicious countries",
						ConditionProperty: string(dbgen.RuleConditionPropertyCountryCode),
						ConditionOperator: string(dbgen.RuleConditionOperatorIn),
						ConditionValue:    "AB, CD", ActionProperty: string(dbgen.RuleActionPropertyHTTPRequest),
						ActionValue: "0", Enabled: true,
					},
					Countries: countries, CurrentOrg: org, RuleID: "r1", IsEdit: true,
				}
			},
		},
		{
			Path: "/org/{org}/property/{property}/rules/{rule}/edit", Template: ruleTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &RuleWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					RuleFormData: RuleFormData{
						Name:              "Lower difficulty for mobile",
						ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
						ConditionOperator: string(dbgen.RuleConditionOperatorContains),
						ConditionValue:    "Mobile", ActionProperty: string(dbgen.RuleActionPropertyDifficultyLevelPercent),
						ActionValue: "-20", Enabled: true,
					},
					Countries: countries, CurrentOrg: org, Property: viewStubProperty("Main Site", "org1"),
					RuleID: "r2", IsEdit: true,
				}
			},
		},
		{
			Path: "/org-invite/{id}/register", Template: loginTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &loginRenderContext{
					CsrfRenderContext: token, CaptchaRenderContext: captchaCtx,
					Email: "invited@example.com", CanRegister: true, IsRegister: true,
				}
			},
		},
	}
}
