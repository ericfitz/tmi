package api

import (
	"archive/zip"
	"bytes"
	"testing"
)

// docxContentType is the DOCX MIME type, used by the api-package pipeline
// and poller integration tests. The extractor logic itself lives in
// pkg/extract; this constant is re-declared here only for test wiring.
const docxContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// minimalDocxBody is a tiny, well-formed word/document.xml body used by the
// api-package integration tests to build a valid DOCX archive.
const minimalDocxBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Title</w:t></w:r></w:p>
    <w:p><w:r><w:t>Hello</w:t></w:r></w:p>
  </w:body>
</w:document>`

// mockExtractor is a configurable ContentExtractor stub used by the
// api-package pipeline tests. The real extractor implementations live in
// pkg/extract; this stub stays here for test wiring.
type mockExtractor struct {
	name      string
	canHandle bool
	result    ExtractedContent
	err       error
}

func (m *mockExtractor) Name() string                      { return m.name }
func (m *mockExtractor) CanHandle(contentType string) bool { return m.canHandle }
func (m *mockExtractor) Extract(data []byte, ct string) (ExtractedContent, error) {
	return m.result, m.err
}

// buildZip builds an in-memory OOXML-shaped archive from a name -> bytes
// map. Used by the api-package pipeline and poller integration tests.
func buildZip(t *testing.T, parts map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip.Create(%s): %v", name, err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatalf("zip write(%s): %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
