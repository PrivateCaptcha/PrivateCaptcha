package widget

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := staticFiles.ReadFile("static/js/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			Static("hash").ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Body.String() != string(want) {
				t.Fatalf("GET %s: status = %d, body matches %s = %t", tt.url, w.Code, tt.file, w.Body.String() == string(want))
			}
		})
	}
}
