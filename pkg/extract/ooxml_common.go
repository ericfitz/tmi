package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Repeated XML local names shared by DOCX and PPTX extractors. Pulled out
// as constants to satisfy goconst once both extractors started referencing
// them.
const (
	xmlLocalTitle        = "title"
	xmlLocalTbl          = "tbl"
	xmlLocalRelationship = "Relationship"
	xmlAttrTarget        = "Target"
)

// dcNS is the Dublin Core elements namespace used in docProps/core.xml.
// Used by ooxmlLoadCoreTitle to identify the dc:title element.
const dcNS = "http://purl.org/dc/elements/1.1/"

// markdownBuilder wraps bytes.Buffer with a hard cap. Any write that would
// push Len() past max returns *extractionLimitError{Kind:"markdown_size"}.
// The buffer state is left as it was before the failing write — no partial
// output beyond the cap.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: size-capped buffer that accumulates markdown output and rejects writes exceeding the limit
type markdownBuilder struct {
	buf bytes.Buffer
	max int64
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: build a markdownBuilder with the given byte capacity (pure)
func newMarkdownBuilder(maxBytes int64) *markdownBuilder { return &markdownBuilder{max: maxBytes} }

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: append a string to the markdown buffer, returning an error if the cap would be exceeded (pure)
func (m *markdownBuilder) WriteString(s string) (int, error) {
	if int64(m.buf.Len()+len(s)) > m.max {
		return 0, &extractionLimitError{
			Kind:     "markdown_size",
			Limit:    m.max,
			Observed: int64(m.buf.Len() + len(s)),
		}
	}
	return m.buf.WriteString(s)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: append a single byte to the markdown buffer, returning an error if the cap would be exceeded (pure)
func (m *markdownBuilder) WriteByte(b byte) error {
	if int64(m.buf.Len()+1) > m.max {
		return &extractionLimitError{
			Kind:     "markdown_size",
			Limit:    m.max,
			Observed: int64(m.buf.Len() + 1),
		}
	}
	return m.buf.WriteByte(b)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: return the current byte length of the markdown buffer (pure)
func (m *markdownBuilder) Len() int       { return m.buf.Len() }
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: return accumulated markdown content as a string (pure)
func (m *markdownBuilder) String() string { return m.buf.String() }

// ooxmlOpener wraps archive/zip with limit enforcement and security
// checks. It refuses oversize inputs up front, rejects path traversal /
// absolute paths / backslashes, and gates per-member reads through
// boundedReader so that streaming decoders trip mid-read on overrun.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: OOXML archive opener that enforces size limits and rejects path traversal before decompression
type ooxmlOpener struct{ limits Limits }

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: build an ooxmlOpener with the given extraction limits (pure)
func newOOXMLOpener(l Limits) *ooxmlOpener { return &ooxmlOpener{limits: l} }

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: open OOXML archive handle tracking cumulative decompressed bytes and extraction context
type ooxmlArchive struct {
	zr       *zip.Reader
	limits   Limits
	consumed int64 // running cumulative decompressed bytes across all members
	ctx      context.Context
}

// WithContext sets the context used by all subsequent boundedReaders
// returned by openMember to abort their reads on cancellation. Used by
// ContextAwareExtractor implementations to wire wall-clock cancellation
// through to in-flight I/O. A nil ctx disables cooperative cancellation
// (the default).
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: attach a cancellation context to the archive for cooperative read abort (mutates shared state)
func (a *ooxmlArchive) WithContext(ctx context.Context) { a.ctx = ctx }

// open performs up-front compressed-size + path-shape checks and returns an
// archive handle. It does not decompress yet — that happens member-by-member.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: validate and open an OOXML ZIP archive, rejecting oversized or path-unsafe entries (pure)
func (o *ooxmlOpener) open(data []byte) (*ooxmlArchive, error) {
	if int64(len(data)) > o.limits.CompressedSizeBytes {
		return nil, &extractionLimitError{
			Kind: "compressed_size", Limit: o.limits.CompressedSizeBytes, Observed: int64(len(data)),
		}
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: zip read: %w", ErrMalformed, err)
	}
	for _, f := range zr.File {
		name := f.Name
		if strings.Contains(name, `\`) {
			return nil, &extractionLimitError{Kind: "zip_path", Limit: 0, Observed: 0, Detail: "backslash: " + name}
		}
		if strings.HasPrefix(name, "/") {
			return nil, &extractionLimitError{Kind: "zip_path", Limit: 0, Observed: 0, Detail: "absolute: " + name}
		}
		// path-traversal check: any segment ".." rejected
		for _, seg := range strings.Split(name, "/") {
			if seg == ".." {
				return nil, &extractionLimitError{Kind: "zip_path", Limit: 0, Observed: 0, Detail: "traversal: " + name}
			}
		}
	}
	return &ooxmlArchive{zr: zr, limits: o.limits}, nil
}

// clampToInt64 converts a uint64 to int64, clamping to math.MaxInt64 on
// overflow. Used only for error-reporting fields where a saturated value is
// more useful than a negative or panicking conversion.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: convert a uint64 to int64, saturating at MaxInt64 to avoid overflow (pure)
func clampToInt64(v uint64) int64 {
	const maxInt64 = 1<<63 - 1
	if v > maxInt64 {
		return maxInt64
	}
	return int64(v)
}

// openMember opens a single member by exact name, returning a reader that
// enforces per-part + cumulative + ratio limits. Returns ErrMalformed-wrapped
// error if the member doesn't exist. Returns *extractionLimitError if the
// member is a nested zip (sniffed by header).
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: open a named ZIP member enforcing per-part size, compression-ratio, and nested-zip limits
func (a *ooxmlArchive) openMember(name string) (io.ReadCloser, error) {
	for _, f := range a.zr.File {
		if f.Name != name {
			continue
		}
		// Compare as uint64 to avoid int64 overflow; limits are non-negative.
		if f.UncompressedSize64 > uint64(a.limits.PartSizeBytes) { //nolint:gosec // PartSizeBytes is always non-negative
			return nil, &extractionLimitError{
				Kind:     "part_size",
				Limit:    a.limits.PartSizeBytes,
				Observed: clampToInt64(f.UncompressedSize64),
				Detail:   name,
			}
		}
		// Compression-ratio sanity: only enforce when we have a non-zero
		// compressed size to compare against.
		if f.CompressedSize64 > 0 {
			ratio := f.UncompressedSize64 / f.CompressedSize64
			if ratio > uint64(a.limits.MaxCompressionRatio) { //nolint:gosec // MaxCompressionRatio is always non-negative
				return nil, &extractionLimitError{
					Kind:     "compression_ratio",
					Limit:    a.limits.MaxCompressionRatio,
					Observed: clampToInt64(ratio),
					Detail:   name,
				}
			}
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: open %s: %w", ErrMalformed, name, err)
		}
		// Sniff header for nested-zip refusal.
		header := make([]byte, 4)
		n, _ := io.ReadFull(rc, header)
		if n == 4 && bytes.Equal(header[:4], []byte{0x50, 0x4b, 0x03, 0x04}) {
			_ = rc.Close()
			return nil, &extractionLimitError{Kind: "zip_nested", Limit: 0, Observed: 0, Detail: name}
		}
		return &boundedReader{
			under:         io.MultiReader(bytes.NewReader(header[:n]), rc),
			closer:        rc,
			archive:       a,
			partCap:       a.limits.PartSizeBytes,
			partRead:      0,
			memberID:      name,
			extractionCtx: a.ctx,
		}, nil
	}
	return nil, fmt.Errorf("%w: missing required part %q", ErrMalformed, name)
}

// boundedReader enforces per-part and cumulative-decompressed limits as it
// streams. archive.consumed is updated on every Read so that subsequent
// member opens see the running total.
//
// extractionCtx, when non-nil, is checked at the end of every Read for
// cancellation; if the context is done, Read returns ctx.Err() so any
// streaming consumer (XML decoder, io.ReadAll, etc.) unblocks promptly
// once the wall-clock deadline fires.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: streaming reader that enforces per-part and cumulative decompressed size limits with context cancellation
type boundedReader struct {
	under         io.Reader
	closer        io.Closer
	archive       *ooxmlArchive
	partCap       int64
	partRead      int64
	memberID      string
	extractionCtx context.Context
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: read bytes while enforcing part and archive size caps, aborting on context cancellation (pure)
func (b *boundedReader) Read(p []byte) (int, error) {
	n, err := b.under.Read(p)
	b.partRead += int64(n)
	b.archive.consumed += int64(n)
	if b.partRead > b.partCap {
		return n, &extractionLimitError{Kind: "part_size", Limit: b.partCap, Observed: b.partRead, Detail: b.memberID}
	}
	if b.archive.consumed > b.archive.limits.DecompressedSizeBytes {
		return n, &extractionLimitError{
			Kind: "decompressed_size", Limit: b.archive.limits.DecompressedSizeBytes, Observed: b.archive.consumed,
		}
	}
	if b.extractionCtx != nil {
		if cerr := b.extractionCtx.Err(); cerr != nil {
			return n, cerr
		}
	}
	return n, err
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: close the underlying ZIP member reader (pure)
func (b *boundedReader) Close() error {
	if b.closer != nil {
		return b.closer.Close()
	}
	return nil
}

// boundedXMLDecoder wraps encoding/xml.Decoder with a depth ceiling enforced
// on tokens observed via Token(). It increments depth on each StartElement
// returned by Token() and trips ErrExtractionLimit{Kind:"xml_depth"} when
// the resulting depth exceeds maxDepth.
//
// Limitation: DecodeElement consumes a subtree internally without routing
// inner StartElements through Token(), so depth inside a DecodeElement-
// consumed subtree is not bounded by this wrapper. For well-formed OOXML
// the schema constrains nesting to a known shallow ceiling within any
// element a caller would consume via DecodeElement, so this gap is
// acceptable in practice. Callers needing absolute bounds on adversarial
// input must avoid DecodeElement entirely and walk via Token().
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: XML decoder that trips an error when nesting depth exceeds a configured maximum
type boundedXMLDecoder struct {
	dec      *xml.Decoder
	depth    int
	maxDepth int
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: build a boundedXMLDecoder with the given nesting depth ceiling (pure)
func newBoundedXMLDecoder(r io.Reader, maxDepth int) *boundedXMLDecoder {
	return &boundedXMLDecoder{dec: xml.NewDecoder(r), maxDepth: maxDepth}
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: return the next XML token and enforce the depth ceiling on start elements (pure)
func (b *boundedXMLDecoder) Token() (xml.Token, error) {
	tok, err := b.dec.Token()
	if err != nil {
		return tok, err
	}
	switch tok.(type) {
	case xml.StartElement:
		b.depth++
		if b.depth > b.maxDepth {
			return nil, &extractionLimitError{
				Kind: "xml_depth", Limit: int64(b.maxDepth), Observed: int64(b.depth),
			}
		}
	case xml.EndElement:
		b.depth--
	}
	return tok, nil
}

// DecodeElement is a convenience wrapper that delegates to the embedded
// decoder. It decrements the depth counter on success because the matching
// EndElement for `start` is consumed internally by the underlying decoder
// without passing through our Token() wrapper. Callers who mix Token() and
// DecodeElement would otherwise accumulate +1 drift per DecodeElement call,
// which would falsely trip the depth limit after enough sibling elements.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: decode an XML element subtree and correct the depth counter for internally consumed end elements (pure)
func (b *boundedXMLDecoder) DecodeElement(v any, start *xml.StartElement) error {
	err := b.dec.DecodeElement(v, start)
	if err == nil {
		b.depth-- // compensate for the EndElement consumed internally
	}
	return err
}

// ooxmlLoadCoreTitle reads docProps/core.xml from the OOXML archive and
// returns the trimmed text of the dc:title element. Used by DOCX and PPTX
// extractors as a title fallback when no in-document title heading is found.
//
// Returns ("", nil) if:
//   - arch is nil (e.g., extractor never opened the archive)
//   - docProps/core.xml is absent
//   - the file exists but contains no dc:title element
//   - dc:title text is empty after trimming
//
// Returns a non-nil error only on streaming-decoder failures (XML parse error,
// limit trip such as xml_depth or part_size).
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: fetch the dc:title text from docProps/core.xml in an OOXML archive, returning empty string if absent
func ooxmlLoadCoreTitle(arch *ooxmlArchive, limits Limits) (string, error) {
	if arch == nil {
		return "", nil
	}
	rc, err := arch.openMember("docProps/core.xml")
	if err != nil {
		// Missing core.xml is fine — title remains empty.
		if errors.Is(err, ErrMalformed) {
			return "", nil
		}
		return "", err
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

// renderMarkdownTable writes rows as a GitHub-flavored markdown pipe table.
// The first row is treated as the header; subsequent rows are body rows.
// Rows are padded with empty strings so every row has the same number of
// cells (the maximum across all input rows). Cell text is written verbatim:
// callers are expected to have already backslash-escaped any literal `|`
// characters in cell content (DOCX and PPTX do this at cell-close time).
//
// If shapeComment is non-empty (e.g. "<!-- shape: table -->"), it is
// written on its own line immediately before the table header row.
//
// renderMarkdownTable does not emit any leading separator before the table
// (or before the optional shape comment). Callers are responsible for
// inserting "\n\n" between this table and any preceding output. The
// rationale: callers track their own paragraph-vs-list-vs-table spacing
// state and can position the table precisely.
//
// Errors are propagated from the markdownBuilder; specifically, an
// extractionLimitError with Kind=="markdown_size" if the output cap is
// exceeded part-way through emission.
//
// Returns nil and writes nothing when rows is empty or every row is empty
// (max width == 0).
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: format rows as a GFM pipe table into a markdownBuilder, padding to uniform column count (pure)
func renderMarkdownTable(mb *markdownBuilder, rows [][]string, shapeComment string) error {
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
	if shapeComment != "" {
		if _, err := mb.WriteString(shapeComment + "\n"); err != nil {
			return err
		}
	}
	if _, err := mb.WriteString("| " + strings.Join(rows[0], " | ") + " |"); err != nil {
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

// ExtractWithDeadline runs an extraction function under a wall-clock budget,
// returning early if the budget elapses. It is the entry point the extractor
// worker uses to bound CPU/memory-heavy extractors on adversarial input.
// On timeout it returns context.DeadlineExceeded; on parent cancellation it
// returns ctx.Err(). The wrapped fn receives the deadline-bearing context so
// cooperative cancellation is possible.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: run an extractor function under a wall-clock budget, cancelling on timeout or parent context
func ExtractWithDeadline(ctx context.Context, budget time.Duration, fn func(context.Context) (ExtractedContent, error)) (ExtractedContent, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: internal result carrier for ExtractWithDeadline goroutine communication (pure)
	type result struct {
		c ExtractedContent
		e error
	}
	ch := make(chan result, 1)
	go func() {
		c, e := fn(ctx)
		ch <- result{c, e}
	}()
	select {
	case r := <-ch:
		return r.c, r.e
	case <-ctx.Done():
		return ExtractedContent{}, ctx.Err()
	}
}

// ctxReader wraps an io.Reader so that wall-clock cancellation aborts
// in-flight reads. Used by extractors when streaming large parts.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: io.Reader wrapper that aborts reads when the context is cancelled
type ctxReader struct {
	r   io.Reader
	ctx context.Context
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: build a context-aware reader wrapping the given io.Reader (pure)
func newCtxReader(ctx context.Context, r io.Reader) *ctxReader { return &ctxReader{r: r, ctx: ctx} }

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: read bytes from the underlying reader, returning context error if cancelled (pure)
func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
