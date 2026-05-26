package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeExecuteRendersFormsReportData(t *testing.T) {
	req := httptest.NewRequest("GET", "/usage-report", nil)
	req.Header.Set("User-Agent", "PrivateCaptchaTest/1.0")
	rr := httptest.NewRecorder()

	if err := serveExecute(templates["usage-report"].ContentHTML, req, rr); err != nil {
		t.Fatalf("serveExecute failed: %v", err)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Total Submissions") {
		t.Fatal("expected rendered email preview to include form totals")
	}
	if !strings.Contains(body, "Top 2 forms by submissions:") {
		t.Fatal("expected rendered email preview to include top forms section")
	}
}
