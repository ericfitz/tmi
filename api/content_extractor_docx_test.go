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

func TestDOCXExtractor_BulletList(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
		<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Item 1</w:t></w:r></w:p>
		<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Item 2</w:t></w:r></w:p>
		<w:p><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Nested</w:t></w:r></w:p>
	</w:body></w:document>`
	data := buildZip(t, map[string][]byte{"word/document.xml": []byte(body)})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "- Item 1")
	assert.Contains(t, out.Text, "- Item 2")
	assert.Contains(t, out.Text, "  - Nested")
}

func TestDOCXExtractor_Table(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
		<w:tbl>
			<w:tr><w:tc><w:p><w:r><w:t>H1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>H2</w:t></w:r></w:p></w:tc></w:tr>
			<w:tr><w:tc><w:p><w:r><w:t>D1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>D2</w:t></w:r></w:p></w:tc></w:tr>
		</w:tbl>
	</w:body></w:document>`
	data := buildZip(t, map[string][]byte{"word/document.xml": []byte(body)})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "| H1 | H2 |")
	assert.Contains(t, out.Text, "| --- | --- |")
	assert.Contains(t, out.Text, "| D1 | D2 |")
}

func TestDOCXExtractor_Hyperlink(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
		<w:p><w:r><w:t>before </w:t></w:r><w:hyperlink r:id="rId1"><w:r><w:t>link</w:t></w:r></w:hyperlink><w:r><w:t> after</w:t></w:r></w:p>
	</w:body></w:document>`
	rels := `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com" TargetMode="External"/>
	</Relationships>`
	data := buildZip(t, map[string][]byte{
		"word/document.xml":            []byte(body),
		"word/_rels/document.xml.rels": []byte(rels),
	})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "[link](https://example.com)")
}

func TestDOCXExtractor_DrawingWithAltText(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><w:body>
		<w:p><w:r><w:drawing><wp:inline><wp:docPr id="1" name="Picture 1" descr="A diagram"/></wp:inline></w:drawing></w:r></w:p>
	</w:body></w:document>`
	data := buildZip(t, map[string][]byte{"word/document.xml": []byte(body)})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "![A diagram](image-1)")
}

func TestDOCXExtractor_DrawingWithoutAltText(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><w:body>
		<w:p><w:r><w:drawing><wp:inline><wp:docPr id="1" name="Picture 1"/></wp:inline></w:drawing></w:r></w:p>
	</w:body></w:document>`
	data := buildZip(t, map[string][]byte{"word/document.xml": []byte(body)})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	assert.NotContains(t, out.Text, "image-")
}

func TestDOCXExtractor_Footnotes(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
		<w:p><w:r><w:t>See note</w:t></w:r><w:r><w:footnoteReference w:id="2"/></w:r></w:p>
	</w:body></w:document>`
	footnotes := `<?xml version="1.0"?><w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
		<w:footnote w:id="0"/>
		<w:footnote w:id="1"/>
		<w:footnote w:id="2"><w:p><w:r><w:t>The footnote text</w:t></w:r></w:p></w:footnote>
	</w:footnotes>`
	data := buildZip(t, map[string][]byte{
		"word/document.xml":  []byte(body),
		"word/footnotes.xml": []byte(footnotes),
	})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "[^2]")
	assert.Contains(t, out.Text, "### Footnotes")
	assert.Contains(t, out.Text, "[^2]: The footnote text")
}

func TestDOCXExtractor_HeaderFooterCommentsExcluded(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
		<w:p><w:r><w:t>body text</w:t></w:r></w:p>
		<w:sectPr><w:headerReference w:id="rId1" w:type="default"/></w:sectPr>
	</w:body></w:document>`
	header := `<?xml version="1.0"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>HEADER TEXT</w:t></w:r></w:p></w:hdr>`
	data := buildZip(t, map[string][]byte{
		"word/document.xml": []byte(body),
		"word/header1.xml":  []byte(header),
	})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, docxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "body text")
	assert.NotContains(t, out.Text, "HEADER TEXT")
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
