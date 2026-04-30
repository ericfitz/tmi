package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const minimalDocxBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Title</w:t></w:r></w:p>
    <w:p><w:r><w:t>Hello</w:t></w:r></w:p>
  </w:body>
</w:document>`

const docxMIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

func TestDOCXExtractor_BasicHeadingAndParagraph(t *testing.T) {
	data := buildZip(t, map[string][]byte{
		"word/document.xml": []byte(minimalDocxBody),
	})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "# Title")
	assert.Contains(t, out.Text, "Hello")
	assert.Equal(t, "Title", out.Title)
}

func TestDOCXExtractor_AllHeadingLevels(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`
	for i := 1; i <= 6; i++ {
		body += `<w:p><w:pPr><w:pStyle w:val="Heading` + string(rune('0'+i)) + `"/></w:pPr><w:r><w:t>H` + string(rune('0'+i)) + `</w:t></w:r></w:p>`
	}
	body += `</w:body></w:document>`
	data := buildZip(t, map[string][]byte{"word/document.xml": []byte(body)})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	for i := 1; i <= 6; i++ {
		assert.Contains(t, out.Text, strings.Repeat("#", i)+" H"+string(rune('0'+i)))
	}
}

func TestDOCXExtractor_MissingDocumentXMLIsMalformed(t *testing.T) {
	data := buildZip(t, map[string][]byte{"word/styles.xml": []byte("<x/>")})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	_, err := e.Extract(data, docxMIME)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMalformed))
}

func TestDOCXExtractor_BoundedFlag(t *testing.T) {
	e := NewDOCXExtractor(defaultOOXMLLimits())
	assert.True(t, e.Bounded())
}

func TestDOCXExtractor_CanHandle(t *testing.T) {
	e := NewDOCXExtractor(defaultOOXMLLimits())
	assert.True(t, e.CanHandle(docxMIME))
	assert.False(t, e.CanHandle("application/pdf"))
}

func TestDOCXExtractor_TripsMarkdownSize(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`
	for i := 0; i < 100; i++ {
		body += `<w:p><w:r><w:t>` + strings.Repeat("x", 64) + `</w:t></w:r></w:p>`
	}
	body += `</w:body></w:document>`
	data := buildZip(t, map[string][]byte{"word/document.xml": []byte(body)})
	limits := defaultOOXMLLimits()
	limits.MarkdownSizeBytes = 200
	e := NewDOCXExtractor(limits)
	_, err := e.Extract(data, docxMIME)
	assert.Error(t, err)
	var le *extractionLimitError
	if errors.As(err, &le) {
		assert.Equal(t, "markdown_size", le.Kind)
	}
}
