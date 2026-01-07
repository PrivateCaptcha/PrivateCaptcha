package db

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestTemplateFS(t *testing.T) {
	// Create a simple in-memory filesystem with template files
	testFS := fstest.MapFS{
		"simple.txt": &fstest.MapFile{
			Data: []byte("Hello, {{.Name}}!"),
		},
		"novar.txt": &fstest.MapFile{
			Data: []byte("This is plain text."),
		},
		"multivar.txt": &fstest.MapFile{
			Data: []byte("User: {{.Name}}, Age: {{.Age}}"),
		},
	}

	type templateData struct {
		Name string
		Age  int
	}

	data := templateData{Name: "World", Age: 30}

	tfs := NewTemplateFS(testFS, data)

	tests := []struct {
		name     string
		filename string
		expected string
	}{
		{
			name:     "simple template with one variable",
			filename: "simple.txt",
			expected: "Hello, World!",
		},
		{
			name:     "plain text without variables",
			filename: "novar.txt",
			expected: "This is plain text.",
		},
		{
			name:     "template with multiple variables",
			filename: "multivar.txt",
			expected: "User: World, Age: 30",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := tfs.Open(tc.filename)
			if err != nil {
				t.Fatalf("Failed to open file: %v", err)
			}
			defer file.Close()

			content, err := io.ReadAll(file)
			if err != nil {
				t.Fatalf("Failed to read file: %v", err)
			}

			if string(content) != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, string(content))
			}
		})
	}
}

func TestTemplateFSOpenNonExistent(t *testing.T) {
	testFS := fstest.MapFS{
		"exists.txt": &fstest.MapFile{
			Data: []byte("content"),
		},
	}

	tfs := NewTemplateFS(testFS, struct{}{})

	_, err := tfs.Open("nonexistent.txt")
	if err == nil {
		t.Error("Expected error when opening non-existent file")
	}
}

func TestTemplateFSReadDir(t *testing.T) {
	testFS := fstest.MapFS{
		"dir/file1.txt": &fstest.MapFile{
			Data: []byte("file1"),
		},
		"dir/file2.txt": &fstest.MapFile{
			Data: []byte("file2"),
		},
	}

	tfs := NewTemplateFS(testFS, struct{}{})

	entries, err := tfs.ReadDir("dir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}
}

func TestTemplateFSStat(t *testing.T) {
	testFS := fstest.MapFS{
		"test.txt": &fstest.MapFile{
			Data: []byte("Hello, {{.Name}}!"),
		},
	}

	data := struct{ Name string }{Name: "Test"}
	tfs := NewTemplateFS(testFS, data)

	file, err := tfs.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "test.txt" {
		t.Errorf("Expected filename test.txt, got %s", info.Name())
	}
}

func TestTemplateFSInvalidTemplate(t *testing.T) {
	testFS := fstest.MapFS{
		"invalid.txt": &fstest.MapFile{
			Data: []byte("Hello, {{.Name"),
		},
	}

	tfs := NewTemplateFS(testFS, struct{ Name string }{Name: "Test"})

	file, err := tfs.Open("invalid.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	_, err = io.ReadAll(file)
	if err == nil {
		t.Error("Expected error when parsing invalid template")
	}
}

func TestTemplateFileStat(t *testing.T) {
	testFS := fstest.MapFS{
		"stats.txt": &fstest.MapFile{
			Data: []byte("content {{.Value}}"),
		},
	}

	data := struct{ Value string }{Value: "test"}
	tfs := NewTemplateFS(testFS, data)

	file, err := tfs.Open("stats.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if stat == nil {
		t.Error("Expected non-nil FileInfo")
	}
}

func TestTemplateFSImplementsFSInterface(t *testing.T) {
	testFS := fstest.MapFS{}
	tfs := NewTemplateFS(testFS, struct{}{})

	// Verify that templateFS implements fs.FS
	var _ fs.FS = tfs
}

func TestTemplateFSMultipleReads(t *testing.T) {
	testFS := fstest.MapFS{
		"data.txt": &fstest.MapFile{
			Data: []byte("Value: {{.Number}}"),
		},
	}

	data := struct{ Number int }{Number: 42}
	tfs := NewTemplateFS(testFS, data)

	// First read
	file1, _ := tfs.Open("data.txt")
	content1, _ := io.ReadAll(file1)
	file1.Close()

	// Second read
	file2, _ := tfs.Open("data.txt")
	content2, _ := io.ReadAll(file2)
	file2.Close()

	if string(content1) != string(content2) {
		t.Errorf("Expected consistent reads, got %q and %q", content1, content2)
	}

	if string(content1) != "Value: 42" {
		t.Errorf("Expected 'Value: 42', got %q", content1)
	}
}
