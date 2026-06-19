package extract

import (
	"context"
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
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: extractor that converts DOCX archives to Markdown-flavored text (pure)
type DOCXExtractor struct {
	limits Limits
}

// NewDOCXExtractor returns an extractor configured with the given limits.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: build a DOCXExtractor configured with the given extraction limits (pure)
func NewDOCXExtractor(limits Limits) *DOCXExtractor {
	return &DOCXExtractor{limits: limits}
}

// Name returns the extractor name as registered with the registry.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: return the registered extractor name for DOCX files (pure)
func (e *DOCXExtractor) Name() string { return "docx" }

// CanHandle returns true iff contentType is the DOCX OOXML MIME type.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: validate that the content type matches the DOCX OOXML MIME type (pure)
func (e *DOCXExtractor) CanHandle(contentType string) bool {
	return strings.EqualFold(contentType, docxContentType)
}

// Bounded marks DOCXExtractor as needing a wall-clock deadline.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: signal that this extractor requires a wall-clock deadline context (pure)
func (e *DOCXExtractor) Bounded() bool { return true }

// Extract parses a DOCX archive and produces Markdown-flavored text. This
// is the legacy entry point that delegates to ExtractCtx with a
// background context (no cooperative cancellation).
//
// On non-nil error, the returned ExtractedContent is zero and must be discarded.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parse a DOCX archive and return Markdown text using a background context
func (e *DOCXExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	return e.ExtractCtx(context.Background(), data, contentType)
}

// ExtractCtx is the context-aware extraction entry point used by the
// pipeline's ExtractWithDeadline wrapper. The supplied ctx is wired into
// the OOXML archive so that all member-level boundedReaders abort their
// reads on cancellation (wall-clock deadline or parent cancel).
//
// On non-nil error, the returned ExtractedContent is zero and must be discarded.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parse a DOCX archive with context cancellation and return Markdown text and title
func (e *DOCXExtractor) ExtractCtx(ctx context.Context, data []byte, contentType string) (ExtractedContent, error) {
	opener := newOOXMLOpener(e.limits)
	arch, err := opener.open(data)
	if err != nil {
		return ExtractedContent{}, err
	}
	arch.WithContext(ctx)
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
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: load and emit a Footnotes section for all referenced footnotes from the DOCX archive
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
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: cross-element rendering state for a streaming DOCX-to-Markdown pass (pure)
type docxState struct {
	mb           *markdownBuilder
	title        string
	archive      *ooxmlArchive     // used for lazy loading auxiliary parts (rels, footnotes)
	limits       Limits            // extractor limits for bounded sub-decoders
	rels         map[string]string // hyperlink rels (id -> target); nil = not yet loaded; non-nil = loaded
	imageCounter int               // increments on each emitted image placeholder
	footnoteRefs []string          // ordered list of referenced footnote ids
	footnoteSeen map[string]bool   // dedupe set for footnoteRefs

	// Numbering state. numberingLoaded becomes true after the first attempt to
	// load word/numbering.xml (success or failure); subsequent encounters of
	// w:numPr reuse the cached map. numIDFormats is keyed by numId and holds
	// the per-ilvl numFmt string ("bullet", "decimal", "lowerLetter", ...).
	numberingLoaded bool
	numIDFormats    map[string]map[int]string

	// Running counters per (numId, ilvl). listCounters tracks the next ordinal
	// to emit; listLastParaIdx tracks the paragraph index of the most recently
	// emitted list item at that key, so we can reset the counter when a gap
	// of more than one paragraph appears between consecutive items.
	listCounters    map[string]int
	listLastParaIdx map[string]int
	// nonListParaCount counts paragraphs that are NOT list items, including
	// headings and plain text. Counter resets at the same (numId, ilvl) key
	// fire when this count advances by more than 1 between consecutive items —
	// i.e., a non-list paragraph broke the list. Other list items (at any key)
	// don't break it; that's how nested lists resume their parent's counter.
	nonListParaCount int
}

// docxParaState tracks state for a single paragraph being assembled.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: per-paragraph assembly state for heading level, text, and list metadata (pure)
type docxParaState struct {
	headingLevel int             // 0 = not a heading, 1-6 = H1-H6
	runText      strings.Builder // accumulated text for this paragraph
	isListItem   bool            // true when paragraph carries a w:numPr (list item)
	listIndent   int             // ilvl value: 0 = top-level, 1 = nested, etc.
	listNumID    string          // w:numId value, empty if not present
}

// docxLoadRels loads word/_rels/document.xml.rels into st.rels. Idempotent;
// on any failure (missing file, parse error) st.rels is left as an empty map.
// Subsequent calls see st.rels != nil and skip re-loading.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: lazily load hyperlink relationship targets from the DOCX rels file into render state
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
		if se.Name.Local != xmlLocalRelationship {
			continue
		}
		var id, target string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case xmlAttrTarget:
				target = a.Value
			}
		}
		if id != "" && target != "" {
			st.rels[id] = target
		}
	}
}

// docxLoadCoreTitle is a thin shim over ooxmlLoadCoreTitle that writes the
// recovered title back into st.title. Used as a fallback when no in-document
// heading was promoted to title during the streaming pass.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: fetch the document core title from the DOCX archive as a fallback title source
func docxLoadCoreTitle(st *docxState) error {
	title, err := ooxmlLoadCoreTitle(st.archive, st.limits)
	if err != nil {
		return err
	}
	if title != "" {
		st.title = title
	}
	return nil
}

// docxTableState accumulates rows and cells while inside a w:tbl.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: accumulates table rows and cells during streaming DOCX table parsing (pure)
type docxTableState struct {
	rows    [][]string      // completed rows
	curRow  []string        // current row's cells
	curCell strings.Builder // accumulating text for current cell
	inCell  bool
}

// docxHyperlinkFrame buffers text inside a w:hyperlink so it can be wrapped
// as a markdown link on the closing tag.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: buffer for text inside a hyperlink element pending Markdown link wrapping (pure)
type docxHyperlinkFrame struct {
	buf    strings.Builder
	target string
}

// docxRenderCtx carries the per-walk mutable state for docxRenderBody. Pulled
// out of the function so that handlers can be split into methods, keeping
// per-method cyclomatic complexity manageable.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: per-walk mutable context for docxRenderBody streaming handler dispatch (pure)
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
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: stream word/document.xml and render paragraphs, lists, tables, and links as Markdown
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
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: route a text fragment to the active hyperlink, table cell, or paragraph buffer (pure)
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
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: flush the current paragraph to the Markdown buffer with heading or list prefix
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
		prefix = strings.Repeat("  ", c.p.listIndent) + docxListMarker(c.st, c.p) + " "
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
	if !c.p.isListItem {
		c.st.nonListParaCount++
	}
	c.p = nil
	return nil
}

// docxListMarker returns the markdown prefix marker (without trailing space) for
// the current list-item paragraph. It consults the cached numbering map (lazily
// loading word/numbering.xml on first call) to decide bullet vs numbered, and
// maintains a running counter per (numId, ilvl) keyed list. Counters reset when
// more than one non-list paragraph appears between consecutive items at the
// same key. Unknown formats and missing numbering.xml fall back to "-".
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: compute the Markdown list prefix for a list-item paragraph, maintaining ordinal counters (pure)
func docxListMarker(st *docxState, p *docxParaState) string {
	docxLoadNumbering(st)
	numID := p.listNumID
	ilvl := p.listIndent
	fmtName := ""
	if numID != "" {
		if lvls, ok := st.numIDFormats[numID]; ok {
			fmtName = lvls[ilvl]
		}
	}
	if fmtName == "" || fmtName == "bullet" {
		return "-"
	}

	key := numID + "/" + itoa(ilvl)
	if st.listCounters == nil {
		st.listCounters = map[string]int{}
		st.listLastParaIdx = map[string]int{}
	}
	last, hadPrev := st.listLastParaIdx[key]
	// Reset counter if there's a gap of more than one non-list paragraph
	// between consecutive items at the same (numId, ilvl), or if this is the
	// first item. Other list items in between (siblings/children at different
	// keys) don't break the sequence — that lets nested lists resume their
	// parent's counter when the outer level resumes.
	if !hadPrev || st.nonListParaCount-last > 1 {
		st.listCounters[key] = 0
	}
	st.listCounters[key]++
	st.listLastParaIdx[key] = st.nonListParaCount
	return docxFormatOrdinal(fmtName, st.listCounters[key]) + "."
}

// docxFormatOrdinal renders n in the requested numFmt style. Unknown formats
// fall back to decimal.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: convert an ordinal number to a string in the requested DOCX numFmt style (pure)
func docxFormatOrdinal(fmtName string, n int) string {
	switch fmtName {
	case "decimal", "ordinal", "cardinalText", "ordinalText",
		"decimalZero", "decimalEnclosedCircle", "decimalEnclosedFullstop",
		"decimalEnclosedParen":
		return itoa(n)
	case "lowerLetter":
		return docxAlphabetic(n, 'a')
	case "upperLetter":
		return docxAlphabetic(n, 'A')
	case "lowerRoman":
		return strings.ToLower(docxRoman(n))
	case "upperRoman":
		return docxRoman(n)
	default:
		return itoa(n)
	}
}

// docxAlphabetic produces an Excel-style spreadsheet column label using the
// supplied base letter ('a' or 'A'). 1->a, 2->b, ..., 26->z, 27->aa, 28->ab.
// Returns "?" when n <= 0 to keep output deterministic.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: convert a positive integer to an Excel-style alphabetic column label (pure)
func docxAlphabetic(n int, base rune) string {
	if n <= 0 {
		return "?"
	}
	var out []rune
	for n > 0 {
		n--
		out = append([]rune{base + rune(n%26)}, out...)
		n /= 26
	}
	return string(out)
}

// docxRoman renders n as upper-case Roman numerals. Falls back to decimal for
// n <= 0 or n > 3999 (outside classical Roman range).
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: convert a positive integer to upper-case Roman numeral string (pure)
func docxRoman(n int) string {
	if n <= 0 || n > 3999 {
		return itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// docxLoadNumbering lazily loads word/numbering.xml and populates
// st.numIDFormats. It walks <w:abstractNum> elements first to build a
// (abstractNumId -> ilvl -> numFmt) map, then walks <w:num> to resolve each
// numId to its abstract num via <w:abstractNumId>. On any failure (missing
// part, parse error) the map is left empty so callers fall back to bullet
// rendering. Idempotent — only the first call does work.
// docxAttrLocalName scans a list of XML attributes for the first one whose
// local name matches; returns the value or "" if not present.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: search XML attributes for a named attribute and return its value (pure)
func docxAttrLocalName(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

const docxAttrVal = "val"

// docxNumberingState carries the running parser state for word/numbering.xml.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parser state for streaming word/numbering.xml to build list format maps (pure)
type docxNumberingState struct {
	curAbstractID string
	curNumID      string
	curIlvl       int
	inAbstract    bool
	inNum         bool
	inLvl         bool
}

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: dispatch a numbering.xml start element to update abstract format and numId maps (pure)
func docxNumberingHandleStart(st *docxNumberingState, t xml.StartElement, abstractFormats map[string]map[int]string, numToAbstract map[string]string) {
	switch t.Name.Local {
	case "abstractNum":
		st.inAbstract = true
		st.curAbstractID = docxAttrLocalName(t.Attr, "abstractNumId")
		if st.curAbstractID != "" && abstractFormats[st.curAbstractID] == nil {
			abstractFormats[st.curAbstractID] = map[int]string{}
		}
	case "num":
		st.inNum = true
		st.curNumID = docxAttrLocalName(t.Attr, "numId")
	case "lvl":
		if st.inAbstract {
			st.inLvl = true
			st.curIlvl = parseNonNegInt(docxAttrLocalName(t.Attr, "ilvl"))
		}
	case "numFmt":
		if st.inAbstract && st.inLvl && st.curAbstractID != "" {
			if v := docxAttrLocalName(t.Attr, docxAttrVal); v != "" {
				abstractFormats[st.curAbstractID][st.curIlvl] = v
			}
		}
	case "abstractNumId":
		if st.inNum && st.curNumID != "" {
			if v := docxAttrLocalName(t.Attr, docxAttrVal); v != "" {
				numToAbstract[st.curNumID] = v
			}
		}
	}
}

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: dispatch a numbering.xml end element to reset active abstract/num/level state (pure)
func docxNumberingHandleEnd(st *docxNumberingState, t xml.EndElement) {
	switch t.Name.Local {
	case "abstractNum":
		st.inAbstract = false
		st.curAbstractID = ""
	case "num":
		st.inNum = false
		st.curNumID = ""
	case "lvl":
		st.inLvl = false
	}
}

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: lazily parse word/numbering.xml and populate numId-to-format maps in render state
func docxLoadNumbering(st *docxState) {
	if st.numberingLoaded {
		return
	}
	st.numberingLoaded = true
	st.numIDFormats = map[string]map[int]string{}
	if st.archive == nil {
		return
	}
	rc, err := st.archive.openMember("word/numbering.xml")
	if err != nil {
		// Missing numbering.xml is fine — fall back to bullet rendering.
		return
	}
	defer func() { _ = rc.Close() }()

	abstractFormats := map[string]map[int]string{} // abstractNumId -> ilvl -> numFmt
	numToAbstract := map[string]string{}           // numId -> abstractNumId

	dec := newBoundedXMLDecoder(rc, st.limits.MaxXMLElementDepth)
	pst := &docxNumberingState{}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wordMLNS {
				continue
			}
			docxNumberingHandleStart(pst, t, abstractFormats, numToAbstract)
		case xml.EndElement:
			if t.Name.Space != wordMLNS {
				continue
			}
			docxNumberingHandleEnd(pst, t)
		}
	}

	for numID, abstractID := range numToAbstract {
		if lvls, ok := abstractFormats[abstractID]; ok {
			st.numIDFormats[numID] = lvls
		}
	}
}

// parseNonNegInt parses a small non-negative integer. Returns 0 on any parse
// error or negative input.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parse a small non-negative integer string, returning 0 on error (pure)
func parseNonNegInt(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
		if n > 1<<20 {
			return 0
		}
	}
	return n
}

// emitTable renders the accumulated table state as a markdown table. The
// first row becomes the header row; subsequent rows are body rows.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: render the accumulated table rows as a Markdown table into the output buffer
func (c *docxRenderCtx) emitTable() error {
	tbl := c.tbl
	if tbl == nil || len(tbl.rows) == 0 {
		c.tbl = nil
		return nil
	}
	if !c.first {
		if _, err := c.st.mb.WriteString("\n\n"); err != nil {
			return err
		}
	}
	if err := renderMarkdownTable(c.st.mb, tbl.rows, ""); err != nil {
		return err
	}
	c.first = false
	c.prevWasListItem = false
	c.tbl = nil
	return nil
}

// handleStart dispatches start-element events. Only w:* and wp:docPr are
// handled; everything else is ignored. Returns errors from text decoding or
// markdown buffer overruns.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: dispatch a DOCX start element event to update rendering context or emit content
func (c *docxRenderCtx) handleStart(t xml.StartElement) error {
	if t.Name.Space == wpMLNS && t.Name.Local == "docPr" {
		c.handleDrawingDocPr(t)
		return nil
	}
	if t.Name.Space != wordMLNS {
		return nil
	}
	switch t.Name.Local {
	case xmlLocalTbl:
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
	case "numId":
		c.handleNumID(t)
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
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: dispatch a DOCX end element event to close paragraphs, cells, rows, or tables
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
	case xmlLocalTbl:
		return c.emitTable()
	case "hyperlink":
		c.handleHyperlinkEnd()
	}
	return nil
}

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: flush a paragraph end, emitting to Markdown or separating table cell paragraphs
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

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: emit an image placeholder with alt text from a wp:docPr drawing element (pure)
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

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parse a paragraph style element to set heading level on the current paragraph state (pure)
func (c *docxRenderCtx) handlePStyle(t xml.StartElement) {
	if c.p == nil {
		return
	}
	for _, a := range t.Attr {
		if a.Name.Local != docxAttrVal || !strings.HasPrefix(a.Value, "Heading") {
			continue
		}
		suffix := a.Value[len("Heading"):]
		if len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '6' {
			c.p.headingLevel = int(suffix[0] - '0')
		}
	}
}

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parse list indent level from XML attribute and store on current paragraph (pure)
func (c *docxRenderCtx) handleIlvl(t xml.StartElement) {
	if c.p == nil || !c.p.isListItem {
		return
	}
	for _, a := range t.Attr {
		if a.Name.Local != docxAttrVal {
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

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parse list numbering ID from XML attribute and store on current paragraph (pure)
func (c *docxRenderCtx) handleNumID(t xml.StartElement) {
	if c.p == nil || !c.p.isListItem {
		return
	}
	for _, a := range t.Attr {
		if a.Name.Local == docxAttrVal {
			c.p.listNumID = a.Value
			return
		}
	}
}

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: register a footnote reference and emit its markdown anchor inline (mutates shared state)
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

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: push a new hyperlink frame onto the stack, resolving the relationship target (mutates shared state)
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

// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: pop the hyperlink frame and emit the buffered inner text as a markdown link (mutates shared state)
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

// itoa converts an int to a decimal string without importing strconv in this file.
// Package-private copy for pkg/extract; relocated alongside the docx extractor
// from api/timmy_embedding_automation_handlers.go where the monolith keeps its own copy.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: convert an integer to its decimal string representation (pure)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
