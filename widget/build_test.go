package widget_test

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/widget"
)

func TestStaticWidgetVariant(t *testing.T) {
	tests := []struct {
		name string
		url  string
		file string
	}{
		{"Default", "/js/privatecaptcha.js", "privatecaptcha.js"},
		{"Extended", "/js/privatecaptcha.js?v=ext", "privatecaptcha-ext.js"},
		{"ExtendedWithOptions", "/js/privatecaptcha.js?render=explicit&v=ext", "privatecaptcha-ext.js"},
		{"OtherVersion", "/js/privatecaptcha.js?v=other", "privatecaptcha.js"},
		{"WhitelabelVersion", "/js/privatecaptcha.js?v=whitelabel", "privatecaptcha.js"},
		{"WhitelabelOption", "/js/privatecaptcha.js?whitelabel=true", "privatecaptcha.js"},
		{"ExtendedWhitelabelOption", "/js/privatecaptcha.js?v=ext&whitelabel=true", "privatecaptcha-ext.js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := widget.StaticFiles().ReadFile("static/js/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			widget.Static("hash").ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Body.String() != string(want) {
				t.Fatalf("GET %s: status = %d, body matches %s = %t", tt.url, w.Code, tt.file, w.Body.String() == string(want))
			}
		})
	}
}

func TestWhitelabelAssetsEmbedded(t *testing.T) {
	files, err := fs.Sub(widget.StaticFiles(), "whitelabel")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"privatecaptcha.js", "privatecaptcha-ext.js", "privatecaptcha.js.map", "privatecaptcha-ext.js.map"} {
		t.Run(file, func(t *testing.T) {
			got, err := fs.ReadFile(files, "js/"+file)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile("whitelabel/js/" + file)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) == 0 || !bytes.Equal(got, want) {
				t.Fatal("Embedded asset does not match the whitelabel build")
			}
		})
	}
}

func TestStaticDoesNotServeWhitelabel(t *testing.T) {
	for _, file := range []string{"privatecaptcha.js", "privatecaptcha-ext.js", "privatecaptcha.js.map", "privatecaptcha-ext.js.map"} {
		for _, prefix := range []string{
			"/whitelabel/js/",
			"/../whitelabel/js/",
			"/%2e%2e/whitelabel/js/",
			"/js/../../whitelabel/js/",
			"/static/../whitelabel/js/",
		} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				t.Run(method+prefix+file, func(t *testing.T) {
					r := httptest.NewRequest(method, prefix+file, nil)
					w := httptest.NewRecorder()
					widget.Static("hash").ServeHTTP(w, r)
					if w.Code != http.StatusNotFound {
						t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
					}
				})
			}
		}
	}
}

func TestStaticListingsExcludeWhitelabel(t *testing.T) {
	for _, url := range []string{"/", "/js/"} {
		r := httptest.NewRequest(http.MethodGet, url, nil)
		w := httptest.NewRecorder()
		widget.Static("hash").ServeHTTP(w, r)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "whitelabel") {
			t.Fatalf("GET %s: status = %d, body = %s", url, w.Code, w.Body.String())
		}
	}
}
