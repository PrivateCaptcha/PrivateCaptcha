package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestStubGateCompleteHandler(t *testing.T) {
	for _, tc := range []struct {
		name       string
		solution   string
		wantStatus int
		wantBody   bool
	}{
		{name: "solution returns 204 no content", solution: "test-token", wantStatus: http.StatusNoContent, wantBody: false},
		{name: "missing solution returns 400", solution: "", wantStatus: http.StatusBadRequest, wantBody: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			if tc.solution != "" {
				form.Set(common.ParamPrivateCaptchaSolution, tc.solution)
			}
			r := httptest.NewRequest(http.MethodPost, api.GateCompletePath, strings.NewReader(form.Encode()))
			r.Header.Set(common.HeaderContentType, "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			stubGateCompleteHandler(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if !tc.wantBody && w.Body.Len() != 0 {
				t.Fatalf("expected empty body for %d, got %q", tc.wantStatus, w.Body.String())
			}
			if tc.wantBody && w.Body.Len() == 0 {
				t.Fatalf("expected non-empty body for %d", tc.wantStatus)
			}
		})
	}
}
