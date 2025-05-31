package db

import (
	"bytes"
	"io"
	"io/fs"
	"text/template"
)

// templateFS is a wrapper around fs.FS that executes templates
type templateFS struct {
	fs   fs.FS
	data interface{}
}

// NewTemplateFS creates a new TemplateFS
func NewTemplateFS(filesystem fs.FS, data interface{}) *templateFS {
	return &templateFS{
		fs:   filesystem,
		data: data,
	}
}

// Open implements fs.FS interface
func (tfs *templateFS) Open(name string) (fs.File, error) {
	file, err := tfs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	return &templateFile{file: file, data: tfs.data}, nil
}

// ReadDir implements fs.ReadDirFS interface
func (tfs *templateFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if rdfs, ok := tfs.fs.(fs.ReadDirFS); ok {
		return rdfs.ReadDir(name)
	}
	// Fallback to manual implementation if fs.ReadDirFS is not implemented
	dir, err := tfs.Open(name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return fs.ReadDir(tfs, name)
}

// templateFile is a wrapper around fs.File that executes templates
type templateFile struct {
	file fs.File
	data interface{}
	buf  *bytes.Buffer
}

// Read implements io.Reader interface
func (tf *templateFile) Read(p []byte) (n int, err error) {
	if tf.buf == nil {
		content, err := io.ReadAll(tf.file)
		if err != nil {
			return 0, err
		}

		tmpl, err := template.New("file").Parse(string(content))
		if err != nil {
			return 0, err
		}

		tf.buf = &bytes.Buffer{}
		err = tmpl.Execute(tf.buf, tf.data)
		if err != nil {
			return 0, err
		}
	}

	return tf.buf.Read(p)
}

// Close implements io.Closer interface
func (tf *templateFile) Close() error {
	return tf.file.Close()
}

// Stat implements fs.File interface
func (tf *templateFile) Stat() (fs.FileInfo, error) {
	return tf.file.Stat()
}
