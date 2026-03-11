package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

func TestLoadTemplatesUsesEmailTemplateHash(t *testing.T) {
	t.Parallel()

	loaded := loadTemplates()

	if len(loaded) != len(email.Templates()) {
		t.Fatalf("expected %d templates, got %d", len(email.Templates()), len(loaded))
	}

	for _, tpl := range email.Templates() {
		got, ok := loaded[tpl.Name()]
		if !ok {
			t.Fatalf("template %q was not loaded", tpl.Name())
		}
		if got.TemplateID != tpl.Hash() {
			t.Fatalf("template %q: expected hash %q, got %q", tpl.Name(), tpl.Hash(), got.TemplateID)
		}
		if got.ContentHTML != tpl.ContentHTML() {
			t.Fatalf("template %q: expected HTML content to match", tpl.Name())
		}
	}
}

func TestHomepageIncludesTemplateIDNextToLink(t *testing.T) {
	prevTemplates := templates
	templates = map[string]emailTemplateView{
		"zzz": {TemplateID: "hash-zzz", ContentHTML: "<p>z</p>"},
		"aaa": {TemplateID: "hash-aaa", ContentHTML: "<p>a</p>"},
	}
	t.Cleanup(func() {
		templates = prevTemplates
	})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	homepage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `<a href="/aaa">aaa</a> <code>hash-aaa</code>`) {
		t.Fatalf("expected homepage to include template ID for aaa, body=%q", body)
	}
	if !strings.Contains(body, `<a href="/zzz">zzz</a> <code>hash-zzz</code>`) {
		t.Fatalf("expected homepage to include template ID for zzz, body=%q", body)
	}

	if strings.Index(body, `/aaa`) > strings.Index(body, `/zzz`) {
		t.Fatalf("expected homepage to sort templates by name, body=%q", body)
	}
}
