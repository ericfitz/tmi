package api

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const pptxMIME = "application/vnd.openxmlformats-officedocument.presentationml.presentation"

// buildPPTX builds a minimal in-memory PPTX with N slides given as slide bodies.
// Each entry is the inner content of <p:sld><p:cSld><p:spTree>...</p:spTree></p:cSld></p:sld>.
// The Nth slide gets path ppt/slides/slide{N+1}.xml.
func buildPPTX(t *testing.T, slides ...string) []byte {
	t.Helper()
	parts := map[string][]byte{}
	pres := `<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst>`
	rels := `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	for i := range slides {
		rid := fmt.Sprintf("rId%d", i+10)
		path := fmt.Sprintf("slides/slide%d.xml", i+1)
		pres += fmt.Sprintf(`<p:sldId id="%d" r:id="%s"/>`, 256+i, rid)
		rels += fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="%s"/>`, rid, path)
		parts["ppt/"+path] = []byte(`<?xml version="1.0"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree>` + slides[i] + `</p:spTree></p:cSld></p:sld>`)
	}
	pres += `</p:sldIdLst></p:presentation>`
	rels += `</Relationships>`
	parts["ppt/presentation.xml"] = []byte(pres)
	parts["ppt/_rels/presentation.xml.rels"] = []byte(rels)
	return buildZip(t, parts)
}

func TestPPTXExtractor_BoundedAndCanHandle(t *testing.T) {
	e := NewPPTXExtractor(defaultOOXMLLimits())
	assert.True(t, e.Bounded())
	assert.True(t, e.CanHandle(pptxMIME))
	assert.False(t, e.CanHandle("application/pdf"))
}

func TestPPTXExtractor_SingleSlideWithTitle(t *testing.T) {
	slide := `<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>My Title</a:t></a:r></a:p></p:txBody></p:sp>
              <p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Body content</a:t></a:r></a:p></p:txBody></p:sp>`
	data := buildPPTX(t, slide)
	e := NewPPTXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, pptxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "## Slide 1: My Title")
	assert.Contains(t, out.Text, "Body content")
	assert.Equal(t, "My Title", out.Title)
}

func TestPPTXExtractor_TwoSlideOrdering(t *testing.T) {
	s1 := `<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>First</a:t></a:r></a:p></p:txBody></p:sp>`
	s2 := `<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Second</a:t></a:r></a:p></p:txBody></p:sp>`
	data := buildPPTX(t, s1, s2)
	e := NewPPTXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, pptxMIME)
	assert.NoError(t, err)
	i1 := strings.Index(out.Text, "Slide 1: First")
	i2 := strings.Index(out.Text, "Slide 2: Second")
	assert.True(t, i1 >= 0 && i2 > i1, "slides must appear in declared order")
}

func TestPPTXExtractor_MissingPresentationXMLIsMalformed(t *testing.T) {
	data := buildZip(t, map[string][]byte{"ppt/_rels/presentation.xml.rels": []byte("<x/>")})
	e := NewPPTXExtractor(defaultOOXMLLimits())
	_, err := e.Extract(data, pptxMIME)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMalformed))
}

func TestPPTXExtractor_NoTitleShape(t *testing.T) {
	// Slide with body content but no title placeholder
	slide := `<p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Body only</a:t></a:r></a:p></p:txBody></p:sp>`
	data := buildPPTX(t, slide)
	e := NewPPTXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, pptxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "## Slide 1")     // fallback title
	assert.NotContains(t, out.Text, "## Slide 1:") // no colon when no title
	assert.Equal(t, "", out.Title)
}

func TestPPTXExtractor_TripsMarkdownSize(t *testing.T) {
	// Build a presentation that produces enough markdown to exceed a small cap.
	slides := []string{}
	for i := 0; i < 50; i++ {
		slides = append(slides, fmt.Sprintf(`<p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>%s</a:t></a:r></a:p></p:txBody></p:sp>`, strings.Repeat("x", 200)))
	}
	data := buildPPTX(t, slides...)
	limits := defaultOOXMLLimits()
	limits.MarkdownSizeBytes = 200
	e := NewPPTXExtractor(limits)
	_, err := e.Extract(data, pptxMIME)
	assert.Error(t, err)
	var le *extractionLimitError
	if errors.As(err, &le) {
		assert.Equal(t, "markdown_size", le.Kind)
	}
}
