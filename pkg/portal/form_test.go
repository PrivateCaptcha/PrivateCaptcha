package portal

import (
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PuerkitoBio/goquery"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestWebhookPrefixFromURL(t *testing.T) {
	testCases := []struct {
		name string
		url  string
		want string
	}{
		{name: "FirstPathSegment", url: "https://hooks.example.com/submit/form", want: "hooks.example.com/submit"},
		{name: "LongSegmentTrimmed", url: "https://hooks.example.com/abcdefghijklmnop/rest", want: "hooks.example.com/abcdefghijkl"},
		{name: "NoPath", url: "https://hooks.example.com", want: "hooks.example.com"},
		{name: "InvalidURLFallsBack", url: "not a valid url", want: "not a valid url"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webhookPrefixFromURL(tc.url); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFormToUserForm(t *testing.T) {
	hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "test-salt"))
	property := &dbgen.Property{ID: 12, Name: "Newsletter Signup"}
	form := &dbgen.Form{
		ID:      34,
		OrgID:   pgtype.Int4{Int32: 56, Valid: true},
		URL:     "https://hooks.example.com/submit/form",
		Enabled: true,
	}

	userForm := formToUserForm(form, property, hasher)
	if userForm == nil {
		t.Fatal("expected user form")
	}
	if userForm.ID != hasher.Encrypt(int(form.ID)) {
		t.Fatalf("expected encrypted form ID %q, got %q", hasher.Encrypt(int(form.ID)), userForm.ID)
	}
	if userForm.OrgID != hasher.Encrypt(int(form.OrgID.Int32)) {
		t.Fatalf("expected encrypted org ID %q, got %q", hasher.Encrypt(int(form.OrgID.Int32)), userForm.OrgID)
	}
	if userForm.Name != property.Name {
		t.Fatalf("expected property name %q, got %q", property.Name, userForm.Name)
	}
	if userForm.WebhookPrefix != "hooks.example.com/submit" {
		t.Fatalf("expected webhook prefix %q, got %q", "hooks.example.com/submit", userForm.WebhookPrefix)
	}
	if !userForm.Enabled {
		t.Fatal("expected enabled form")
	}
}

func TestRenderFormsPaginationControls(t *testing.T) {
	platformCtx := &PlatformRenderContext{
		GitCommit:      "qwerty123",
		Enterprise:     true,
		licenseService: server.LicenseService,
	}

	buf, err := server.RenderResponse(t.Context(), "portal/forms.html", &orgFormsRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{CurrentOrg: stubOrg("123")},
		PaginationRenderContext: PaginationRenderContext{From: 1, To: 30, Count: 31, Page: 0, PerPage: 30},
		Forms:                   []*userForm{stubForm("Newsletter Signup", "123")},
	}, &RequestContext{Path: server.RelURL("/org/123/forms")}, platformCtx)
	if err != nil {
		t.Fatal(err)
	}

	document := portal_tests.ParseHTML(t, buf)
	buttons := document.Find("button[hx-target=\"#forms\"]")
	if buttons.Length() != 2 {
		t.Fatalf("expected 2 pagination buttons, got %d", buttons.Length())
	}
	buttons.Each(func(i int, s *goquery.Selection) {
		if s.AttrOr("hx-get", "") != "/org/123/forms" {
			t.Fatalf("expected pagination button to use forms endpoint, got %q", s.AttrOr("hx-get", ""))
		}
	})
}
