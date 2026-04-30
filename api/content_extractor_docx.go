package api

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// DOCX MIME type — application/vnd.openxmlformats-officedocument.wordprocessingml.document
const docxContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// wordMLNS is the WordprocessingML XML namespace used by all w:* elements.
const wordMLNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// wpMLNS is the WordprocessingDrawing XML namespace used by wp:* elements
// (wp:inline, wp:anchor, wp:docPr, etc.).
const wpMLNS = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"

// DOCXExtractor extracts Markdown-flavored text from a DOCX (OOXML) archive.
type DOCXExtractor struct {
	limits ooxmlLimits
}

// NewDOCXExtractor returns an extractor configured with the given limits.
func NewDOCXExtractor(limits ooxmlLimits) *DOCXExtractor {
	return &DOCXExtractor{limits: limits}
}

// Name returns the extractor name as registered with the registry.
func (e *DOCXExtractor) Name() string { return "docx" }

// CanHandle returns true iff contentType is the DOCX OOXML MIME type.
func (e *DOCXExtractor) CanHandle(contentType string) bool {
	return strings.EqualFold(contentType, docxContentType)
}

// Bounded marks DOCXExtractor as needing a wall-clock deadline.
func (e *DOCXExtractor) Bounded() bool { return true }

// Extract parses a DOCX archive and produces Markdown-flavored text.
func (e *DOCXExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	opener := newOOXMLOpener(e.limits)
	arch, err := opener.open(data)
	if err != nil {
		return ExtractedContent{}, err
	}
	rdr, err := arch.openMember("word/document.xml")
	if err != nil {
		return ExtractedContent{}, err
	}
	defer func() { _ = rdr.Close() }()

	mb := newMarkdownBuilder(e.limits.MarkdownSizeBytes)
	d := newBoundedXMLDecoder(rdr, e.limits.MaxXMLElementDepth)

	st := &docxState{mb: mb, archive: arch, limits: e.limits}
	if err := docxRenderBody(d, st); err != nil {
		return ExtractedContent{}, err
	}

	if len(st.footnoteRefs) > 0 {
		if err := docxRenderFootnotes(st); err != nil {
			return ExtractedContent{}, err
		}
	}

	if st.title == "" {
		if err := docxLoadCoreTitle(st); err != nil {
			return ExtractedContent{}, err
		}
	}

	return ExtractedContent{
		Text:        strings.TrimRight(st.mb.String(), "\n"),
		Title:       st.title,
		ContentType: contentType,
	}, nil
}

// docxRenderFootnotes loads word/footnotes.xml, walks each w:footnote, and
// emits a `### Footnotes` section listing each referenced footnote.
// Unreferenced footnotes (including the system separator/continuation
// footnotes typically at id 0 and -1) are skipped.
func docxRenderFootnotes(st *docxState) error {
	rc, err := st.archive.openMember("word/footnotes.xml")
	if err != nil {
		// Missing footnotes.xml: refs already emitted as [^N], just skip the section.
		if errors.Is(err, ErrMalformed) {
			return nil
		}
		return err
	}
	defer func() { _ = rc.Close() }()

	wanted := map[string]bool{}
	for _, id := range st.footnoteRefs {
		wanted[id] = true
	}

	textByID := map[string]string{}
	dec := newBoundedXMLDecoder(rc, st.limits.MaxXMLElementDepth)
	var (
		curID      string
		curBuf     strings.Builder
		inFootnote bool
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wordMLNS {
				continue
			}
			switch t.Name.Local {
			case "footnote":
				curID = ""
				for _, a := range t.Attr {
					if a.Name.Local == "id" {
						curID = a.Value
						break
					}
				}
				inFootnote = wanted[curID]
				curBuf.Reset()
			case "t":
				var text string
				if err := dec.DecodeElement(&text, &t); err != nil {
					return err
				}
				if inFootnote {
					curBuf.WriteString(text)
				}
			case "p":
				if inFootnote && curBuf.Len() > 0 {
					curBuf.WriteByte(' ')
				}
			}
		case xml.EndElement:
			if t.Name.Space == wordMLNS && t.Name.Local == "footnote" && inFootnote {
				textByID[curID] = strings.TrimSpace(curBuf.String())
				inFootnote = false
			}
		}
	}

	// Emit the section. Always lead with a blank line because body content
	// preceded us (we only got here if footnoteRefs is non-empty).
	if _, err := st.mb.WriteString("\n\n### Footnotes"); err != nil {
		return err
	}
	for _, id := range st.footnoteRefs {
		text := textByID[id]
		if text == "" {
			continue
		}
		if _, err := st.mb.WriteString("\n\n[^" + id + "]: " + text); err != nil {
			return err
		}
	}
	return nil
}

// docxState carries cross-element rendering state through the streaming pass.
type docxState struct {
	mb           *markdownBuilder
	title        string
	archive      *ooxmlArchive     // used for lazy loading auxiliary parts (rels, footnotes)
	limits       ooxmlLimits       // extractor limits for bounded sub-decoders
	rels         map[string]string // hyperlink rels (id -> target); nil = not yet loaded; non-nil = loaded
	imageCounter int               // increments on each emitted image placeholder
	footnoteRefs []string          // ordered list of referenced footnote ids
	footnoteSeen map[string]bool   // dedupe set for footnoteRefs
}

// docxParaState tracks state for a single paragraph being assembled.
type docxParaState struct {
	headingLevel int             // 0 = not a heading, 1-6 = H1-H6
	runText      strings.Builder // accumulated text for this paragraph
	isListItem   bool            // true when paragraph carries a w:numPr (list item)
	listIndent   int             // ilvl value: 0 = top-level, 1 = nested, etc.
}

// docxLoadRels loads word/_rels/document.xml.rels into st.rels. Idempotent;
// on any failure (missing file, parse error) st.rels is left as an empty map.
// Subsequent calls see st.rels != nil and skip re-loading.
func docxLoadRels(st *docxState) {
	if st.rels != nil {
		return
	}
	st.rels = map[string]string{}
	if st.archive == nil {
		return
	}
	rc, err := st.archive.openMember("word/_rels/document.xml.rels")
	if err != nil {
		// Missing rels file is fine — links just render as plain text.
		return
	}
	defer func() { _ = rc.Close() }()
	dec := newBoundedXMLDecoder(rc, st.limits.MaxXMLElementDepth)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			// Parse error: leave st.rels as the (possibly partial) map and stop.
			return
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local != "Relationship" {
			continue
		}
		var id, target string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			}
		}
		if id != "" && target != "" {
			st.rels[id] = target
		}
	}
}

// dcNS is the Dublin Core elements namespace used in docProps/core.xml.
const dcNS = "http://purl.org/dc/elements/1.1/"

// docxLoadCoreTitle reads docProps/core.xml and extracts the dc:title element,
// setting st.title if found. Missing file or empty title are silently ignored.
func docxLoadCoreTitle(st *docxState) error {
	if st.archive == nil {
		return nil
	}
	rc, err := st.archive.openMember("docProps/core.xml")
	if err != nil {
		// Missing core.xml is fine — leave title empty.
		return nil
	}
	defer func() { _ = rc.Close() }()
	dec := newBoundedXMLDecoder(rc, st.limits.MaxXMLElementDepth)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Space == dcNS && se.Name.Local == "title" {
			var text string
			if err := dec.DecodeElement(&text, &se); err != nil {
				return err
			}
			st.title = strings.TrimSpace(text)
			return nil
		}
	}
}

// docxTableState accumulates rows and cells while inside a w:tbl.
type docxTableState struct {
	rows    [][]string      // completed rows
	curRow  []string        // current row's cells
	curCell strings.Builder // accumulating text for current cell
	inCell  bool
}

// docxHyperlinkFrame buffers text inside a w:hyperlink so it can be wrapped
// as a markdown link on the closing tag.
type docxHyperlinkFrame struct {
	buf    strings.Builder
	target string
}

// docxRenderCtx carries the per-walk mutable state for docxRenderBody. Pulled
// out of the function so that handlers can be split into methods, keeping
// per-method cyclomatic complexity manageable.
type docxRenderCtx struct {
	d               *boundedXMLDecoder
	st              *docxState
	p               *docxParaState
	tbl             *docxTableState
	hyperlinks      []*docxHyperlinkFrame
	first           bool
	prevWasListItem bool
}

// docxRenderBody walks word/document.xml in a single streaming pass and
// writes markdown into st.mb. It handles paragraphs, headings, bullet/numbered
// lists, tables, hyperlinks, embedded drawings (alt text only), and footnote
// references. Header/footer/comment parts are not opened, so their text is
// excluded by construction.
func docxRenderBody(d *boundedXMLDecoder, st *docxState) error {
	c := &docxRenderCtx{d: d, st: st, first: true}
	for {
		tok, err := c.d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := c.handleStart(t); err != nil {
				return err
			}
		case xml.EndElement:
			if err := c.handleEnd(t); err != nil {
				return err
			}
		}
	}
}

// writeText routes a text fragment to the right buffer based on the current
// nesting context (hyperlink > table cell > paragraph).
func (c *docxRenderCtx) writeText(s string) {
	if n := len(c.hyperlinks); n > 0 {
		c.hyperlinks[n-1].buf.WriteString(s)
		return
	}
	if c.tbl != nil && c.tbl.inCell {
		c.tbl.curCell.WriteString(s)
		return
	}
	if c.p != nil {
		c.p.runText.WriteString(s)
	}
}

// emitPara flushes the current paragraph (if any) to the markdown buffer.
func (c *docxRenderCtx) emitPara() error {
	if c.p == nil {
		return nil
	}
	text := strings.TrimSpace(c.p.runText.String())
	if text == "" {
		c.p = nil
		return nil
	}
	// List items separate from each other with a single newline; everything
	// else gets a blank line.
	if !c.first {
		sep := "\n\n"
		if c.prevWasListItem && c.p.isListItem {
			sep = "\n"
		}
		if _, err := c.st.mb.WriteString(sep); err != nil {
			return err
		}
	}
	c.first = false
	var prefix string
	switch {
	case c.p.isListItem:
		prefix = strings.Repeat("  ", c.p.listIndent) + "- "
	case c.p.headingLevel > 0:
		prefix = strings.Repeat("#", c.p.headingLevel) + " "
		if c.st.title == "" && c.p.headingLevel == 1 {
			c.st.title = text
		}
	}
	if _, err := c.st.mb.WriteString(prefix + text); err != nil {
		return err
	}
	c.prevWasListItem = c.p.isListItem
	c.p = nil
	return nil
}

// emitTable renders the accumulated table state as a markdown table. The
// first row becomes the header row; subsequent rows are body rows.
func (c *docxRenderCtx) emitTable() error {
	tbl := c.tbl
	if tbl == nil || len(tbl.rows) == 0 {
		c.tbl = nil
		return nil
	}
	width := 0
	for _, r := range tbl.rows {
		if len(r) > width {
			width = len(r)
		}
	}
	if width == 0 {
		c.tbl = nil
		return nil
	}
	for i := range tbl.rows {
		for len(tbl.rows[i]) < width {
			tbl.rows[i] = append(tbl.rows[i], "")
		}
	}
	if !c.first {
		if _, err := c.st.mb.WriteString("\n\n"); err != nil {
			return err
		}
	}
	c.first = false
	if _, err := c.st.mb.WriteString("| " + strings.Join(tbl.rows[0], " | ") + " |"); err != nil {
		return err
	}
	seps := make([]string, width)
	for i := range seps {
		seps[i] = "---"
	}
	if _, err := c.st.mb.WriteString("\n| " + strings.Join(seps, " | ") + " |"); err != nil {
		return err
	}
	for _, r := range tbl.rows[1:] {
		if _, err := c.st.mb.WriteString("\n| " + strings.Join(r, " | ") + " |"); err != nil {
			return err
		}
	}
	c.prevWasListItem = false
	c.tbl = nil
	return nil
}

// handleStart dispatches start-element events. Only w:* and wp:docPr are
// handled; everything else is ignored. Returns errors from text decoding or
// markdown buffer overruns.
func (c *docxRenderCtx) handleStart(t xml.StartElement) error {
	if t.Name.Space == wpMLNS && t.Name.Local == "docPr" {
		c.handleDrawingDocPr(t)
		return nil
	}
	if t.Name.Space != wordMLNS {
		return nil
	}
	switch t.Name.Local {
	case "tbl":
		c.tbl = &docxTableState{}
	case "tr":
		if c.tbl != nil {
			c.tbl.curRow = nil
		}
	case "tc":
		if c.tbl != nil {
			c.tbl.inCell = true
			c.tbl.curCell.Reset()
		}
	case "p":
		// Inside a table cell we don't create paragraph state — text runs
		// go straight into the cell buffer.
		if c.tbl == nil {
			c.p = &docxParaState{}
		}
	case "pStyle":
		c.handlePStyle(t)
	case "numPr":
		if c.p != nil {
			c.p.isListItem = true
		}
	case "ilvl":
		c.handleIlvl(t)
	case "t":
		var text string
		if err := c.d.DecodeElement(&text, &t); err != nil {
			return err
		}
		c.writeText(text)
	case "footnoteReference":
		c.handleFootnoteReference(t)
	case "hyperlink":
		c.handleHyperlinkStart(t)
	}
	return nil
}

// handleEnd dispatches end-element events for w:* elements.
func (c *docxRenderCtx) handleEnd(t xml.EndElement) error {
	if t.Name.Space != wordMLNS {
		return nil
	}
	switch t.Name.Local {
	case "p":
		return c.handleParaEnd()
	case "tc":
		if c.tbl != nil {
			cell := strings.ReplaceAll(strings.TrimSpace(c.tbl.curCell.String()), "|", `\|`)
			c.tbl.curRow = append(c.tbl.curRow, cell)
			c.tbl.curCell.Reset()
			c.tbl.inCell = false
		}
	case "tr":
		if c.tbl != nil && c.tbl.curRow != nil {
			c.tbl.rows = append(c.tbl.rows, c.tbl.curRow)
			c.tbl.curRow = nil
		}
	case "tbl":
		return c.emitTable()
	case "hyperlink":
		c.handleHyperlinkEnd()
	}
	return nil
}

func (c *docxRenderCtx) handleParaEnd() error {
	if c.tbl == nil {
		return c.emitPara()
	}
	// Multiple paragraphs in one cell — separate with a space so cell text
	// stays on one markdown row.
	if c.tbl.inCell && c.tbl.curCell.Len() > 0 {
		c.tbl.curCell.WriteByte(' ')
	}
	return nil
}

func (c *docxRenderCtx) handleDrawingDocPr(t xml.StartElement) {
	var descr string
	for _, a := range t.Attr {
		if a.Name.Local == "descr" {
			descr = a.Value
			break
		}
	}
	if descr == "" {
		return
	}
	c.st.imageCounter++
	c.writeText("![" + descr + "](image-" + itoa(c.st.imageCounter) + ")")
}

func (c *docxRenderCtx) handlePStyle(t xml.StartElement) {
	if c.p == nil {
		return
	}
	for _, a := range t.Attr {
		if a.Name.Local != "val" || !strings.HasPrefix(a.Value, "Heading") {
			continue
		}
		suffix := a.Value[len("Heading"):]
		if len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '6' {
			c.p.headingLevel = int(suffix[0] - '0')
		}
	}
}

func (c *docxRenderCtx) handleIlvl(t xml.StartElement) {
	if c.p == nil || !c.p.isListItem {
		return
	}
	for _, a := range t.Attr {
		if a.Name.Local != "val" {
			continue
		}
		lvl := 0
		for _, ch := range a.Value {
			if ch < '0' || ch > '9' {
				lvl = 0
				break
			}
			lvl = lvl*10 + int(ch-'0')
			if lvl > 8 {
				lvl = 8
				break
			}
		}
		c.p.listIndent = lvl
	}
}

func (c *docxRenderCtx) handleFootnoteReference(t xml.StartElement) {
	var id string
	for _, a := range t.Attr {
		if a.Name.Local == "id" {
			id = a.Value
			break
		}
	}
	if id == "" {
		return
	}
	if c.st.footnoteSeen == nil {
		c.st.footnoteSeen = map[string]bool{}
	}
	if !c.st.footnoteSeen[id] {
		c.st.footnoteSeen[id] = true
		c.st.footnoteRefs = append(c.st.footnoteRefs, id)
	}
	c.writeText("[^" + id + "]")
}

func (c *docxRenderCtx) handleHyperlinkStart(t xml.StartElement) {
	docxLoadRels(c.st)
	var target string
	for _, a := range t.Attr {
		if a.Name.Local == "id" {
			target = c.st.rels[a.Value]
			break
		}
	}
	c.hyperlinks = append(c.hyperlinks, &docxHyperlinkFrame{target: target})
}

func (c *docxRenderCtx) handleHyperlinkEnd() {
	n := len(c.hyperlinks)
	if n == 0 {
		return
	}
	top := c.hyperlinks[n-1]
	c.hyperlinks = c.hyperlinks[:n-1]
	inner := top.buf.String()
	rendered := inner
	if top.target != "" && inner != "" {
		rendered = "[" + inner + "](" + top.target + ")"
	}
	c.writeText(rendered)
}
