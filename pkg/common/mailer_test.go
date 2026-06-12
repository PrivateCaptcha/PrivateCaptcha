package common

import (
	"context"
	"testing"
)

func TestEmailTemplateRenderHTMLInvalidTemplate(t *testing.T) {
	t.Parallel()

	// Template with invalid syntax - unclosed action
	et := NewEmailTemplate("invalid", "Hello {{.Name", "", nil)

	ctx := context.Background()
	result, err := et.RenderHTML(ctx, struct{ Name string }{"Test"})

	// With invalid template syntax, parsedHTML should be nil
	// RenderHTML should return empty string without error in this case
	if result != "" {
		t.Errorf("Expected empty result for unparsed template, got %q", result)
	}

	if err != nil {
		t.Errorf("Expected no error when template is not parsable, got %v", err)
	}
}

func TestEmailTemplateRenderHTMLMissingField(t *testing.T) {
	t.Parallel()

	// Template referencing non-existent field
	et := NewEmailTemplate("missing_field", "Hello {{.NonExistent.Field}}", "", nil)

	ctx := context.Background()
	_, err := et.RenderHTML(ctx, struct{ Name string }{"Test"})

	// Executing template with missing field should return an error
	if err == nil {
		t.Error("Expected error when template references non-existent field")
	}
}

func TestEmailTemplateRenderTextInvalidTemplate(t *testing.T) {
	t.Parallel()

	// Template with invalid syntax
	et := NewEmailTemplate("invalid", "", "Hello {{.Name", nil)

	ctx := context.Background()
	result, err := et.RenderText(ctx, struct{ Name string }{"Test"})

	// With invalid template syntax, parsedText should be nil
	// RenderText should return empty string without error in this case
	if result != "" {
		t.Errorf("Expected empty result for unparsed template, got %q", result)
	}

	if err != nil {
		t.Errorf("Expected no error when template is not parsable, got %v", err)
	}
}

func TestEmailTemplateRenderTextMissingField(t *testing.T) {
	t.Parallel()

	// Template referencing non-existent field
	et := NewEmailTemplate("missing_field", "", "Hello {{.NonExistent.Field}}", nil)

	ctx := context.Background()
	_, err := et.RenderText(ctx, struct{ Name string }{"Test"})

	// Executing template with missing field should return an error
	if err == nil {
		t.Error("Expected error when template references non-existent field")
	}
}

func TestEmailTemplateRenderHTMLValid(t *testing.T) {
	t.Parallel()

	et := NewEmailTemplate("valid", "Hello {{.Name}}!", "", nil)

	ctx := context.Background()
	result, err := et.RenderHTML(ctx, struct{ Name string }{"World"})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expected := "Hello World!"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestEmailTemplateRenderTextValid(t *testing.T) {
	t.Parallel()

	et := NewEmailTemplate("valid", "", "Hello {{.Name}}!", nil)

	ctx := context.Background()
	result, err := et.RenderText(ctx, struct{ Name string }{"World"})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expected := "Hello World!"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestEmailTemplateHash(t *testing.T) {
	t.Parallel()

	et := NewEmailTemplate("test", "HTML content", "", nil)
	hash := et.Hash()

	if hash == "" {
		t.Error("Expected non-empty hash")
	}

	// Hash should be consistent
	hash2 := et.Hash()
	if hash != hash2 {
		t.Error("Hash should be consistent across calls")
	}
}

func TestEmailTemplateHashWithTextOnly(t *testing.T) {
	t.Parallel()

	et := NewEmailTemplate("test", "", "Text content", nil)
	hash := et.Hash()

	if hash == "" {
		t.Error("Expected non-empty hash")
	}
}

func TestEmailTemplateHashWithNameOnly(t *testing.T) {
	t.Parallel()

	et := NewEmailTemplate("test-name", "", "", nil)
	hash := et.Hash()

	if hash == "" {
		t.Error("Expected non-empty hash")
	}
}

func TestEmailTemplateEnsureParsedOnlyOnce(t *testing.T) {
	t.Parallel()

	et := NewEmailTemplate("test", "{{.Name}}", "{{.Name}}", nil)

	ctx := context.Background()

	// Call multiple times to ensure parseOnce works
	_, _ = et.RenderHTML(ctx, struct{ Name string }{"Test1"})
	_, _ = et.RenderHTML(ctx, struct{ Name string }{"Test2"})
	_, _ = et.RenderText(ctx, struct{ Name string }{"Test3"})

	// No panic or error means parseOnce is working correctly
}

func TestEmailTemplateHashCollisionWithDifferentText(t *testing.T) {
	t.Parallel()

	tpl1 := NewEmailTemplate("test", "<html>same html content</html>", "text version 1", nil)
	tpl2 := NewEmailTemplate("test", "<html>same html content</html>", "text version 2 with different content", nil)

	hash1 := tpl1.Hash()
	hash2 := tpl2.Hash()

	if hash1 == hash2 {
		t.Errorf("Hash collision: different text content produced same hash.\nHash1: %s\nHash2: %s\nText1: %q\nText2: %q", hash1, hash2, "text version 1", "text version 2 with different content")
	}
}
