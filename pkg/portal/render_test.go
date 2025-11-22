package portal

import (
	"context"
	"fmt"
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
		ID:     "1",
		OrgID:  orgID,
		Name:   name,
		Domain: "example.com",
		Level:  1,
		Growth: 2,
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

func stubAuditLogs() []*userAuditLog {
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

	result := make([]*userAuditLog, 0)

	for _, table := range tables {
		for _, action := range actions {
			result = append(result, &userAuditLog{
				UserName:  "User Name",
				UserEmail: "foo@bar.com",
				Action:    string(action),
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

func TestRenderHTML(t *testing.T) {
	testCases := []struct {
		path     []string
		template string
		model    any
		selector string
		matches  []string
	}{
		{
			path:     []string{common.ErrorEndpoint, "404"},
			template: errorTemplate,
			model:    &errorRenderContext{ErrorCode: 404, ErrorMessage: http.StatusText(404)},
		},
		{
			path:     []string{common.LoginEndpoint},
			template: loginTemplate,
			model:    &loginRenderContext{CsrfRenderContext: stubToken()},
		},
		{
			path:     []string{common.LoginEndpoint},
			template: twofactorContentsTemplate,
			model:    &loginRenderContext{CsrfRenderContext: stubToken(), Email: "foo@bar.com"},
		},
		{
			path:     []string{common.RegisterEndpoint},
			template: loginTemplate,
			model:    &loginRenderContext{CsrfRenderContext: stubToken(), IsRegister: true},
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
			model:    &orgWizardRenderContext{CsrfRenderContext: stubToken()},
		},
		{
			path:     []string{common.OrgEndpoint, "123"},
			template: portalTemplate,
			model: &orgDashboardRenderContext{
				Orgs:       []*userOrg{stubOrgEx("123", dbgen.AccessLevelOwner)},
				CurrentOrg: stubOrgEx("123", dbgen.AccessLevelOwner),
				Properties: []*userProperty{stubProperty("1", "123"), stubProperty("2", "123")},
			},
			selector: "p.property-name",
			matches:  []string{"1", "2"},
		},
		// same as above, but when Invited, we don't show properties
		{
			path:     []string{common.OrgEndpoint, "123"},
			template: portalTemplate,
			model: &orgDashboardRenderContext{
				Orgs:       []*userOrg{stubOrgEx("123", dbgen.AccessLevelInvited)},
				CurrentOrg: stubOrgEx("123", dbgen.AccessLevelInvited),
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
				CurrentOrg:        stubOrg("123"),
				CsrfRenderContext: stubToken(),
				Members:           []*orgUser{stubUser("foo", dbgen.AccessLevelMember), stubUser("bar", dbgen.AccessLevelInvited)},
				CanEdit:           true,
			},
			selector: "p.member-name",
			matches:  []string{"foo", "bar"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.TabEndpoint, common.SettingsEndpoint},
			template: orgSettingsTemplate,
			model: &orgSettingsRenderContext{
				CurrentOrg:        stubOrg("123"),
				CsrfRenderContext: stubToken(),
				CanEdit:           true,
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
				CurrentOrg: stubOrg("123"),
				CanView:    true,
			},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, common.NewEndpoint},
			template: propertyWizardTemplate,
			model:    &propertyWizardRenderContext{CurrentOrg: stubOrg("123"), CsrfRenderContext: stubToken()},
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
				},
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
				Name: "User",
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
				Limit: 12345,
			},
			selector: "",
			matches:  []string{},
		},
		{
			path:     []string{common.AuditLogsEndpoint},
			template: auditLogsTemplate,
			model: &mainAuditLogsRenderContext{
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
			selector: "",
			matches:  []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("render_%s", strings.Join(tc.path, "_")), func(t *testing.T) {
			ctx := context.TODO()
			path := server.RelURL(strings.Join(tc.path, "/"))
			buf, err := server.RenderResponse(ctx, tc.template, tc.model, &RequestContext{Path: server.RelURL(path)})
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
