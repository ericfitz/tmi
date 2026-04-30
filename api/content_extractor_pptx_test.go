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

func TestPPTXExtractor_HiddenSlideSkipped(t *testing.T) {
	// First slide has show="0", second is normal. Only second renders.
	parts := map[string][]byte{
		"ppt/presentation.xml":            []byte(`<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId10"/><p:sldId id="257" r:id="rId11"/></p:sldIdLst></p:presentation>`),
		"ppt/_rels/presentation.xml.rels": []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId10" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/><Relationship Id="rId11" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>`),
		"ppt/slides/slide1.xml":           []byte(`<?xml version="1.0"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" show="0"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>HiddenTitle</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`),
		"ppt/slides/slide2.xml":           []byte(`<?xml version="1.0"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>VisibleTitle</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`),
	}
	data := buildZip(t, parts)
	e := NewPPTXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, pptxMIME)
	assert.NoError(t, err)
	assert.NotContains(t, out.Text, "HiddenTitle")
	assert.Contains(t, out.Text, "VisibleTitle")
	// Slide numbering: hidden is still counted (Slide 1 hidden, Slide 2 = VisibleTitle)
	assert.Contains(t, out.Text, "## Slide 2:")
}

func TestPPTXExtractor_SpeakerNotes(t *testing.T) {
	parts := map[string][]byte{
		"ppt/presentation.xml":             []byte(`<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId10"/></p:sldIdLst></p:presentation>`),
		"ppt/_rels/presentation.xml.rels":  []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId10" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`),
		"ppt/slides/slide1.xml":            []byte(`<?xml version="1.0"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Title</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`),
		"ppt/slides/_rels/slide1.xml.rels": []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdN" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide1.xml"/></Relationships>`),
		"ppt/notesSlides/notesSlide1.xml":  []byte(`<?xml version="1.0"?><p:notes xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Speaker note text</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:notes>`),
	}
	data := buildZip(t, parts)
	e := NewPPTXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, pptxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "### Notes")
	assert.Contains(t, out.Text, "Speaker note text")
}

func TestPPTXExtractor_SlideCountLimit(t *testing.T) {
	slides := []string{}
	for i := 0; i < 5; i++ {
		slides = append(slides, fmt.Sprintf(`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Slide%d</a:t></a:r></a:p></p:txBody></p:sp>`, i+1))
	}
	data := buildPPTX(t, slides...)
	limits := defaultOOXMLLimits()
	limits.PPTXSlides = 3
	e := NewPPTXExtractor(limits)
	_, err := e.Extract(data, pptxMIME)
	assert.Error(t, err)
	var le *extractionLimitError
	if errors.As(err, &le) {
		assert.Equal(t, "part_count", le.Kind)
		assert.Contains(t, le.Detail, "slide #")
	}
}

func TestPPTXExtractor_Table(t *testing.T) {
	slide := `<p:graphicFrame><p:nvGraphicFramePr/><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData><a:tbl><a:tr><a:tc><a:txBody><a:p><a:r><a:t>H1</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>H2</a:t></a:r></a:p></a:txBody></a:tc></a:tr><a:tr><a:tc><a:txBody><a:p><a:r><a:t>D1</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>D2</a:t></a:r></a:p></a:txBody></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
	data := buildPPTX(t, slide)
	e := NewPPTXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, pptxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "| H1 | H2 |")
	assert.Contains(t, out.Text, "| D1 | D2 |")
}

func TestPPTXExtractor_TableCellEscapesPipe(t *testing.T) {
	slide := `<p:graphicFrame><p:nvGraphicFramePr/><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData><a:tbl><a:tr><a:tc><a:txBody><a:p><a:r><a:t>A | B</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>C</a:t></a:r></a:p></a:txBody></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
	data := buildPPTX(t, slide)
	e := NewPPTXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, pptxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, `A \| B`)
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
