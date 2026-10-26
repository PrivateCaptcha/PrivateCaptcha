package widget

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticWidgetVariant(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		file       string
		whitelabel bool
	}{
		{
			name: "Default",
			url:  "/js/privatecaptcha.js",
			file: "privatecaptcha.js",
		},
		{
			name: "Extended",
			url:  "/js/privatecaptcha.js?v=ext",
			file: "privatecaptcha-ext.js",
		},
		{
			name: "ExtendedWithOptions",
			url:  "/js/privatecaptcha.js?render=explicit&v=ext",
			file: "privatecaptcha-ext.js",
		},
		{
			name: "OtherVersion",
			url:  "/js/privatecaptcha.js?v=other",
			file: "privatecaptcha.js",
		},
		{
			name: "WhitelabelVersion",
			url:  "/js/privatecaptcha.js?v=whitelabel",
			file: "privatecaptcha.js",
		},
		{
			name:       "BrandingNone",
			url:        "/js/privatecaptcha.js?branding=none",
			file:       "privatecaptcha.js",
			whitelabel: true,
		},
		{
			name:       "ExtendedBrandingNone",
			url:        "/js/privatecaptcha.js?v=ext&branding=none",
			file:       "privatecaptcha-ext.js",
			whitelabel: true,
		},
		{
			name:       "DirectExtendedWhitelabel",
			url:        "/js/privatecaptcha-ext.js?branding=none",
			file:       "privatecaptcha-ext.js",
			whitelabel: true,
		},
		{
			name:       "WhitelabelSourceMap",
			url:        "/js/privatecaptcha.js.map?branding=none",
			file:       "privatecaptcha.js.map",
			whitelabel: true,
		},
		{
			name: "BrandingDefault",
			url:  "/js/privatecaptcha.js?branding=default",
			file: "privatecaptcha.js",
		},
		{
			name: "BrandingOther",
			url:  "/js/privatecaptcha.js?branding=false",
			file: "privatecaptcha.js",
		},
		{
			name: "BrandingEmpty",
			url:  "/js/privatecaptcha.js?branding",
			file: "privatecaptcha.js",
		},
		{
			name: "WhitelabelOptionIgnored",
			url:  "/js/privatecaptcha.js?whitelabel=true",
			file: "privatecaptcha.js",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := "static"
			cacheControl := "public, max-age=300, must-revalidate"
			if enterpriseBuild && tt.whitelabel {
				root = "whitelabel"
				cacheControl = "private, no-cache, must-revalidate"
			}
			want, err := StaticFiles().ReadFile(root + "/js/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			Static("hash").ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Body.String() != string(want) {
				t.Fatalf("GET %s: status = %d, body matches %s = %t", tt.url, w.Code, tt.file, w.Body.String() == string(want))
			}
			if got := w.Header().Get("Cache-Control"); got != cacheControl {
				t.Fatalf("Cache-Control = %q, want %q", got, cacheControl)
			}
		})
	}
}

func TestStaticWhitelabelCacheValidation(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/js/privatecaptcha.js?v=ext&branding=none", nil)
			r.Header.Set("If-None-Match", "hash")
			w := httptest.NewRecorder()
			Static("hash").ServeHTTP(w, r)
			if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
				t.Fatalf("status = %d, body length = %d", w.Code, w.Body.Len())
			}
			cacheControl := "public, max-age=300, must-revalidate"
			if enterpriseBuild {
				cacheControl = "private, no-cache, must-revalidate"
			}
			if got := w.Header().Get("Cache-Control"); got != cacheControl {
				t.Fatalf("Cache-Control = %q, want %q", got, cacheControl)
			}
			if w.Header().Get("ETag") != "hash" || w.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Fatalf("Missing ETag or CORS headers: %v", w.Header())
			}
		})
	}
}

func TestStaticDoesNotServeWhitelabelPaths(t *testing.T) {
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
					Static("hash").ServeHTTP(w, r)
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
		Static("hash").ServeHTTP(w, r)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "whitelabel") {
			t.Fatalf("GET %s: status = %d, body = %s", url, w.Code, w.Body.String())
		}
	}
}
