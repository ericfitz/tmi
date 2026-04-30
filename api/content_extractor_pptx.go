package api

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// PPTX MIME type — application/vnd.openxmlformats-officedocument.presentationml.presentation
const pptxContentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"

// PresentationML and DrawingML namespaces used by PPTX parts.
const (
	pptNS = "http://schemas.openxmlformats.org/presentationml/2006/main"
	aNS   = "http://schemas.openxmlformats.org/drawingml/2006/main"
	rNS   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
)

// Repeated XML local names shared by DOCX and PPTX extractors. Pulled out
// as constants to satisfy goconst once both extractors started referencing
// them.
const (
	xmlLocalTitle = "title"
	xmlLocalTbl   = "tbl"
)

// PPTXExtractor extracts Markdown-flavored text from a PPTX (OOXML) archive.
type PPTXExtractor struct {
	limits ooxmlLimits
}

// NewPPTXExtractor returns an extractor configured with the given limits.
func NewPPTXExtractor(limits ooxmlLimits) *PPTXExtractor {
	return &PPTXExtractor{limits: limits}
}

// Name returns the extractor name as registered with the registry.
func (e *PPTXExtractor) Name() string { return "pptx" }

// CanHandle returns true iff contentType is the PPTX OOXML MIME type.
func (e *PPTXExtractor) CanHandle(contentType string) bool {
	return strings.EqualFold(contentType, pptxContentType)
}

// Bounded marks PPTXExtractor as needing a wall-clock deadline.
func (e *PPTXExtractor) Bounded() bool { return true }

// Extract parses a PPTX archive and produces Markdown-flavored text. Slides
// appear in document order under "## Slide N: <title>" headings; hidden
// slides are skipped but still consume slide numbers.
func (e *PPTXExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	opener := newOOXMLOpener(e.limits)
	arch, err := opener.open(data)
	if err != nil {
		return ExtractedContent{}, err
	}

	slidePaths, err := pptxResolveSlideOrder(arch, e.limits)
	if err != nil {
		return ExtractedContent{}, err
	}

	mb := newMarkdownBuilder(e.limits.MarkdownSizeBytes)
	var title string
	first := true

	for i, slidePath := range slidePaths {
		if !first {
			if _, err := mb.WriteString("\n\n"); err != nil {
				return ExtractedContent{}, err
			}
		}
		emitted, slideTitle, err := pptxRenderSlide(arch, slidePath, i+1, mb, e.limits)
		if err != nil {
			return ExtractedContent{}, err
		}
		if emitted {
			first = false
			if title == "" {
				title = slideTitle
			}
		}
	}

	if title == "" {
		if t, terr := pptxLoadCoreTitle(arch, e.limits); terr == nil && t != "" {
			title = t
		}
	}

	return ExtractedContent{
		Text:        strings.TrimRight(mb.String(), "\n"),
		Title:       title,
		ContentType: contentType,
	}, nil
}

// pptxResolveSlideOrder reads ppt/presentation.xml for the sldIdLst order
// and ppt/_rels/presentation.xml.rels for r:id -> path mapping. Returns
// slide paths in document order, prefixed with "ppt/".
func pptxResolveSlideOrder(arch *ooxmlArchive, limits ooxmlLimits) ([]string, error) {
	ridOrder, err := pptxReadSlideOrder(arch, limits)
	if err != nil {
		return nil, err
	}
	rmap, err := pptxReadPresentationRels(arch, limits)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(ridOrder))
	for _, rid := range ridOrder {
		target, ok := rmap[rid]
		if !ok {
			return nil, fmt.Errorf("%w: presentation rel %q has no target", ErrMalformed, rid)
		}
		// Targets are relative to ppt/_rels/, which means relative to ppt/.
		paths = append(paths, "ppt/"+target)
	}
	return paths, nil
}

// pptxReadSlideOrder walks ppt/presentation.xml and returns the ordered list
// of r:id values from <p:sldIdLst><p:sldId/></p:sldIdLst>.
func pptxReadSlideOrder(arch *ooxmlArchive, limits ooxmlLimits) ([]string, error) {
	rc, err := arch.openMember("ppt/presentation.xml")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	dec := newBoundedXMLDecoder(rc, limits.MaxXMLElementDepth)
	var ridOrder []string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Space != pptNS || start.Name.Local != "sldId" {
			continue
		}
		for _, a := range start.Attr {
			if a.Name.Space == rNS && a.Name.Local == "id" {
				ridOrder = append(ridOrder, a.Value)
			}
		}
	}
	return ridOrder, nil
}

// pptxReadPresentationRels reads ppt/_rels/presentation.xml.rels and returns
// a map of relationship Id -> Target for slide relationships.
func pptxReadPresentationRels(arch *ooxmlArchive, limits ooxmlLimits) (map[string]string, error) {
	rc, err := arch.openMember("ppt/_rels/presentation.xml.rels")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	rmap := map[string]string{}
	dec := newBoundedXMLDecoder(rc, limits.MaxXMLElementDepth)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "Relationship" {
			continue
		}
		var id, target string
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			}
		}
		if id != "" && target != "" {
			rmap[id] = target
		}
	}
	return rmap, nil
}

// pptxShape carries one shape's role and accumulated text for a single slide.
type pptxShape struct {
	role string
	text string
}

// pptxSlideRender accumulates per-slide rendering state.
type pptxSlideRender struct {
	shapes []pptxShape
	title  string
	hidden bool
	tables [][][]string // each table rendered in spTree order, mixed with shapes is overkill; we emit tables after shapes
}

// pptxRenderSlide opens one slide, parses its shapes/tables, and writes the
// slide's markdown to mb. Returns (emitted, slideTitle, err). emitted is
// false if the slide was hidden (and no markdown was written).
func pptxRenderSlide(arch *ooxmlArchive, slidePath string, slideNum int, mb *markdownBuilder, limits ooxmlLimits) (bool, string, error) {
	slide, err := pptxParseSlide(arch, slidePath, limits)
	if err != nil {
		return false, "", err
	}
	if slide.hidden {
		return false, "", nil
	}

	if slide.title != "" {
		if _, err := mb.WriteString(fmt.Sprintf("## Slide %d: %s", slideNum, slide.title)); err != nil {
			return false, "", err
		}
	} else {
		if _, err := mb.WriteString(fmt.Sprintf("## Slide %d", slideNum)); err != nil {
			return false, "", err
		}
	}

	for _, s := range slide.shapes {
		if _, err := mb.WriteString("\n\n<!-- shape: " + s.role + " -->\n"); err != nil {
			return false, "", err
		}
		if _, err := mb.WriteString(s.text); err != nil {
			return false, "", err
		}
	}

	for _, tbl := range slide.tables {
		if err := pptxEmitTable(mb, tbl); err != nil {
			return false, "", err
		}
	}

	return true, slide.title, nil
}

// pptxParseSlide streams a slide XML part into pptxSlideRender. It captures
// shape roles + text and table rows/cells.
func pptxParseSlide(arch *ooxmlArchive, slidePath string, limits ooxmlLimits) (*pptxSlideRender, error) {
	rc, err := arch.openMember(slidePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	dec := newBoundedXMLDecoder(rc, limits.MaxXMLElementDepth)
	c := &pptxSlideCtx{slide: &pptxSlideRender{}, dec: dec}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return c.slide, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := c.handleStart(t); err != nil {
				return nil, err
			}
		case xml.EndElement:
			c.handleEnd(t)
		}
	}
}

// pptxSlideCtx carries the streaming-pass state for parsing one slide.
type pptxSlideCtx struct {
	slide *pptxSlideRender
	dec   *boundedXMLDecoder

	// Shape parse state
	inSP    bool
	curRole string
	curText strings.Builder

	// Table parse state
	tbl       *pptxTableState
	cellText  strings.Builder
	inTblCell bool
}

// pptxTableState accumulates rows/cells for an a:tbl in progress.
type pptxTableState struct {
	rows   [][]string
	curRow []string
}

// handleStart dispatches the relevant start elements for slide parsing.
func (c *pptxSlideCtx) handleStart(t xml.StartElement) error {
	switch t.Name.Space {
	case pptNS:
		c.handlePresentationStart(t)
	case aNS:
		return c.handleDrawingStart(t)
	}
	return nil
}

// handlePresentationStart handles p:* element starts: sld (hidden flag),
// sp (shape boundary), ph (placeholder role).
func (c *pptxSlideCtx) handlePresentationStart(t xml.StartElement) {
	switch t.Name.Local {
	case "sld":
		for _, a := range t.Attr {
			if a.Name.Local == "show" && a.Value == "0" {
				c.slide.hidden = true
			}
		}
	case "sp":
		c.inSP = true
		c.curRole = ""
		c.curText.Reset()
	case "ph":
		if !c.inSP {
			return
		}
		for _, a := range t.Attr {
			if a.Name.Local == "type" {
				c.curRole = a.Value
			}
		}
	}
}

// handleDrawingStart handles a:* element starts: tbl/tr/tc (table state),
// p (paragraph break inside text body), t (text run).
func (c *pptxSlideCtx) handleDrawingStart(t xml.StartElement) error {
	switch t.Name.Local {
	case xmlLocalTbl:
		c.tbl = &pptxTableState{}
	case "tr":
		if c.tbl != nil {
			c.tbl.curRow = nil
		}
	case "tc":
		if c.tbl != nil {
			c.inTblCell = true
			c.cellText.Reset()
		}
	case "p":
		c.handleParaStart()
	case "t":
		return c.handleTextRun(t)
	}
	return nil
}

// handleParaStart inserts a space separator inside the current text buffer
// when a new a:p starts after a previous paragraph emitted text.
func (c *pptxSlideCtx) handleParaStart() {
	switch {
	case c.inTblCell:
		if c.cellText.Len() > 0 {
			c.cellText.WriteByte(' ')
		}
	case c.inSP:
		if c.curText.Len() > 0 {
			c.curText.WriteByte(' ')
		}
	}
}

// handleTextRun decodes an a:t element and appends its text to the active
// buffer (table cell or shape).
func (c *pptxSlideCtx) handleTextRun(t xml.StartElement) error {
	var s string
	if err := c.dec.DecodeElement(&s, &t); err != nil {
		return err
	}
	switch {
	case c.inTblCell:
		c.cellText.WriteString(s)
	case c.inSP:
		c.curText.WriteString(s)
	}
	return nil
}

// handleEnd dispatches end-element events for slide parsing.
func (c *pptxSlideCtx) handleEnd(t xml.EndElement) {
	switch {
	case t.Name.Space == pptNS && t.Name.Local == "sp":
		if c.inSP {
			role := c.curRole
			if role == "" {
				role = "text-box"
			}
			text := strings.TrimSpace(c.curText.String())
			if text != "" {
				c.slide.shapes = append(c.slide.shapes, pptxShape{role: role, text: text})
			}
			if (role == xmlLocalTitle || role == "ctr-title") && c.slide.title == "" {
				c.slide.title = text
			}
		}
		c.inSP = false
		c.curRole = ""
		c.curText.Reset()
	case t.Name.Space == aNS && t.Name.Local == "tc":
		if c.tbl != nil {
			cell := strings.ReplaceAll(strings.TrimSpace(c.cellText.String()), "|", `\|`)
			c.tbl.curRow = append(c.tbl.curRow, cell)
			c.cellText.Reset()
			c.inTblCell = false
		}
	case t.Name.Space == aNS && t.Name.Local == "tr":
		if c.tbl != nil && c.tbl.curRow != nil {
			c.tbl.rows = append(c.tbl.rows, c.tbl.curRow)
			c.tbl.curRow = nil
		}
	case t.Name.Space == aNS && t.Name.Local == xmlLocalTbl:
		if c.tbl != nil {
			c.slide.tables = append(c.slide.tables, c.tbl.rows)
			c.tbl = nil
		}
	}
}

// pptxEmitTable writes a markdown table for the given rows.
func pptxEmitTable(mb *markdownBuilder, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	if width == 0 {
		return nil
	}
	for i := range rows {
		for len(rows[i]) < width {
			rows[i] = append(rows[i], "")
		}
	}
	if _, err := mb.WriteString("\n\n| " + strings.Join(rows[0], " | ") + " |"); err != nil {
		return err
	}
	seps := make([]string, width)
	for i := range seps {
		seps[i] = "---"
	}
	if _, err := mb.WriteString("\n| " + strings.Join(seps, " | ") + " |"); err != nil {
		return err
	}
	for _, r := range rows[1:] {
		if _, err := mb.WriteString("\n| " + strings.Join(r, " | ") + " |"); err != nil {
			return err
		}
	}
	return nil
}

// pptxLoadCoreTitle reads docProps/core.xml and returns dc:title if present.
// Missing file or empty title return ("", nil).
func pptxLoadCoreTitle(arch *ooxmlArchive, limits ooxmlLimits) (string, error) {
	rc, err := arch.openMember("docProps/core.xml")
	if err != nil {
		return "", nil
	}
	defer func() { _ = rc.Close() }()
	dec := newBoundedXMLDecoder(rc, limits.MaxXMLElementDepth)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Space == dcNS && se.Name.Local == xmlLocalTitle {
			var text string
			if err := dec.DecodeElement(&text, &se); err != nil {
				return "", err
			}
			return strings.TrimSpace(text), nil
		}
	}
}
