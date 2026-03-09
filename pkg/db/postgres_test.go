package db

import (
	"log/slog"
	"strings"
	"testing"
)

func TestTruncatedArgsShort(t *testing.T) {
	args := truncatedArgs{1, "hello", true}
	result := args.LogValue()
	if result.Kind() != slog.KindString {
		t.Fatalf("expected string kind, got %v", result.Kind())
	}
	s := result.String()
	if strings.HasSuffix(s, "...") {
		t.Fatalf("short args should not be truncated, got %q", s)
	}
	if !strings.Contains(s, "hello") {
		t.Fatalf("expected args to contain 'hello', got %q", s)
	}
}

func TestTruncatedArgsLong(t *testing.T) {
	longStr := strings.Repeat("a", 300)
	args := truncatedArgs{longStr}
	result := args.LogValue()
	if result.Kind() != slog.KindString {
		t.Fatalf("expected string kind, got %v", result.Kind())
	}
	s := result.String()
	if !strings.HasSuffix(s, "...") {
		t.Fatalf("long args should be truncated, got %q", s)
	}
	// maxArgsLogLength (200) + "..." (3) = 203
	if len(s) != maxArgsLogLength+3 {
		t.Fatalf("expected length %d, got %d", maxArgsLogLength+3, len(s))
	}
}

func TestTruncatedArgsEmpty(t *testing.T) {
	args := truncatedArgs{}
	result := args.LogValue()
	s := result.String()
	if s != "[]" {
		t.Fatalf("expected '[]', got %q", s)
	}
}
