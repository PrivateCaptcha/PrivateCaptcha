package main

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestEmbeddedTemplateIncludesInteractiveControls(t *testing.T) {
	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	entries := []LogEntry{{
		Timestamp:            "Feb 14 09:00:00.000",
		Level:                "INFO",
		LevelClass:           "info",
		Message:              "hello",
		Extras:               "extra",
		TraceID:              "trace-1",
		TraceBackground:      `rgba(10,20,30,0.12)`,
		TraceHoverBackground: `rgba(10,20,30,0.35)`,
	}}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, entries); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	html := out.String()
	for _, needle := range []string{
		`class="trace-cell trace-clickable"`,
		`function setTraceFilter`,
		`class="details-toggle"`,
		`filterSelect.value = "DEBUG-4"`,
	} {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected rendered template to contain %q", needle)
		}
	}
}

func TestApplyTraceColorsSetsHoverBackground(t *testing.T) {
	entries := []LogEntry{{TraceID: "trace-1"}}
	applyTraceColors(entries)

	if entries[0].TraceBackground == "" {
		t.Fatal("expected trace background to be set")
	}
	if entries[0].TraceHoverBackground == "" {
		t.Fatal("expected trace hover background to be set")
	}
	if entries[0].TraceBackground == entries[0].TraceHoverBackground {
		t.Fatal("expected hover background to differ from normal background")
	}
}
