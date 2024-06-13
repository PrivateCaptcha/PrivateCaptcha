package portal

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// courtesy of https://martinfowler.com/articles/tdd-html-templates.html
func assertWellFormedHTML(t *testing.T, buf bytes.Buffer) {
	data := buf.Bytes()
	// special handling for Alpine.js, otherwise we get XML parsing error "attribute expected"
	data = bytes.ReplaceAll(data, []byte(" @click="), []byte(" click="))
	data = bytes.ReplaceAll(data, []byte(" hx-on::"), []byte(" hx-on-"))

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity
	for {
		token, err := decoder.Token()
		switch err {
		case io.EOF:
			return // We're done, it's valid!
		case nil:
			// do nothing
		default:
			fmt.Println(buf.String())
			t.Fatalf("Error parsing html: %s, %v", err, token)
		}
	}
}

func parseHTML(t *testing.T, buf bytes.Buffer) *goquery.Document {
	assertWellFormedHTML(t, buf)
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		// if parsing fails, we stop the test here with t.FatalF
		t.Fatalf("Error rendering template %s", err)
	}
	return document
}

func text(node *html.Node) string {
	// A little mess due to the fact that goquery has
	// a .Text() method on Selection but not on html.Node
	sel := goquery.Selection{Nodes: []*html.Node{node}}
	return strings.TrimSpace(sel.Text())
}

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

func stubBillingPlan(id string) *billing.Plan {
	return &billing.Plan{
		Name:                 "Stub plan " + id,
		PaddleProductID:      id,
		PaddlePriceIDMonthly: "price" + id,
		PaddlePriceIDYearly:  "price" + id,
		PriceMonthly:         9,
		PriceYearly:          90,
		Version:              1,
		RequestsLimit:        100,
		PropertiesLimit:      10,
		OrgsLimit:            1,
	}
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
			path:     []string{common.LoginEndpoint},
			template: loginTemplate,
			model:    &loginRenderContext{Token: server.XSRF.Token("", actionLogin)},
		},
		{
			path:     []string{common.TwoFactorEndpoint},
			template: twofactorTemplate,
			model:    &twoFactorRenderContext{Token: server.XSRF.Token("foo@bar.com", actionVerify), Email: "foo@bar.com"},
		},
		{
			path:     []string{common.OrgEndpoint, common.NewEndpoint},
			template: orgWizardTemplate,
			model:    &orgWizardRenderContext{Token: server.XSRF.Token("foo@bar.com", actionNewOrg)},
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
				alertRenderContext: alertRenderContext{
					SuccessMessage: "Test",
				},
				CurrentOrg: stubOrg("123"),
				Token:      "123",
				Members:    []*orgUser{stubUser("foo", dbgen.AccessLevelMember), stubUser("bar", dbgen.AccessLevelInvited)},
				CanEdit:    true,
			},
			selector: "p.member-name",
			matches:  []string{"foo", "bar"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.TabEndpoint, common.SettingsEndpoint},
			template: orgSettingsTemplate,
			model: &orgSettingsRenderContext{
				CurrentOrg: stubOrg("123"),
				Token:      "123",
				CanEdit:    true,
			},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, common.NewEndpoint},
			template: propertyWizardTemplate,
			model:    &propertyWizardRenderContext{CurrentOrg: stubOrg("123"), Token: "qwerty"},
		},
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardTemplate,
			model: &propertyDashboardRenderContext{
				Property: stubProperty("Foo", "123"),
				Org:      stubOrg("123"),
				Token:    "qwerty",
				CanEdit:  true,
			},
		},
		// same as above, but property integrations _template_
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardIntegrationsTemplate,
			model: &propertyDashboardRenderContext{
				Property: stubProperty("Foo", "123"),
				Org:      stubOrg("123"),
				Token:    "qwerty",
				CanEdit:  true,
			},
		},
		// same as above, but property settings _template_
		{
			path:     []string{common.OrgEndpoint, "123", common.PropertyEndpoint, "456"},
			template: propertyDashboardSettingsTemplate,
			model: &propertyDashboardRenderContext{
				alertRenderContext: alertRenderContext{
					SuccessMessage: "Test",
				},
				Property: stubProperty("Foo", "123"),
				Org:      stubOrg("123"),
				Token:    "qwerty",
				CanEdit:  true,
			},
		},
		{
			path:     []string{common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint},
			template: settingsGeneralTemplate,
			model: &settingsGeneralRenderContext{
				alertRenderContext: alertRenderContext{
					SuccessMessage: "Test",
				},
				settingsCommonRenderContext: settingsCommonRenderContext{
					Email: "foo@bar.com",
				},
				Token: "qwerty",
				Name:  "User",
			},
		},
		{
			path:     []string{common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint},
			template: settingsAPIKeysTemplate,
			model: &settingsAPIKeysRenderContext{
				Token: "qwerty",
				Keys:  []*userAPIKey{stubAPIKey("foo"), stubAPIKey("bar")},
			},
			selector: "p.apikey-name",
			matches:  []string{"foo", "bar"},
		},
		{
			path:     []string{common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint},
			template: settingsBillingTemplate,
			model: &settingsBillingRenderContext{
				alertRenderContext: alertRenderContext{
					WarningMessage: "Test warning!",
				},
				Plans:         []*billing.Plan{stubBillingPlan("123"), stubBillingPlan("456")},
				CurrentPlan:   stubBillingPlan("123"),
				YearlyBilling: false,
				IsSubscribed:  true,
				PreviewOpen:   true,
				PreviewCharge: 123,
				PreviewPlan:   "Plan",
				PreviewPeriod: "monthly",
			},
			selector: "span.billing-plan-name",
			matches:  []string{"Stub plan 123", "Stub plan 456"},
		},
		{
			path:     []string{common.SupportEndpoint},
			template: supportTemplate,
			model: &supportRenderContext{
				alertRenderContext: alertRenderContext{
					SuccessMessage: "Message sent",
				},
				Token:    "123",
				Message:  "test",
				Category: "problem",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("render_%s", strings.Join(tc.path, "_")), func(t *testing.T) {
			ctx := context.TODO()
			path := server.relURL(strings.Join(tc.path, "/"))
			buf, err := server.renderResponse(ctx, tc.template, tc.model, &requestContext{Path: server.relURL(path)})
			if err != nil {
				t.Fatal(err)
			}

			if len(tc.selector) > 0 {
				document := parseHTML(t, buf)
				selection := document.Find(tc.selector)
				if len(tc.matches) != len(selection.Nodes) {
					t.Fatalf("Expected %v matches, but got %v", len(tc.matches), len(selection.Nodes))
				}
				for i, node := range selection.Nodes {
					nodeText := text(node)
					if tc.matches[i] != nodeText {
						t.Errorf("Expected match %v at %v, but got %v", tc.matches[i], i, nodeText)
					}
				}
			} else {
				assertWellFormedHTML(t, buf)
			}
		})
	}
}
