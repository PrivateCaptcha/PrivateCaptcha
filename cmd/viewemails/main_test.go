package main

import (
	"net/http/httptest"
	"testing"
)

func TestPreviewTemplatesExecute(t *testing.T) {
	for name, tpl := range templates {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)

			if err := serveExecute(tpl.ContentHTML, r, w); err != nil {
				t.Fatal(err)
			}

			if w.Body.Len() == 0 {
				t.Fatal("template rendered empty response")
			}
		})
	}
}
