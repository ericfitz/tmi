package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"golang.org/x/sync/semaphore"
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

// Sentinel errors returned by OOXML extractors. The pipeline uses errors.Is
// to classify outcomes; these are the stable public surface.
var (
	ErrExtractionLimit = errors.New("extraction limit exceeded")
	ErrMalformed       = errors.New("malformed document")
	ErrUnsupported     = errors.New("unsupported document subformat")
)

// extractionLimitError describes which limit tripped during extraction. The
// API surface (Kind values) is stable: the pipeline maps Kind into
// access_reason_code.
//
// Kind values: compressed_size | decompressed_size | part_size | part_count |
// markdown_size | timeout | xml_depth | zip_nested | zip_path | compression_ratio
type extractionLimitError struct {
	Kind     string
	Limit    int64
	Observed int64  // -1 if not measurable (e.g. timeout)
	Detail   string // optional context: "slide #42", "sheet 'Sales'"
}

func (e *extractionLimitError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("extraction limit exceeded: kind=%s limit=%d observed=%d detail=%q",
			e.Kind, e.Limit, e.Observed, e.Detail)
	}
	return fmt.Sprintf("extraction limit exceeded: kind=%s limit=%d observed=%d",
		e.Kind, e.Limit, e.Observed)
}

func (e *extractionLimitError) Is(target error) bool { return target == ErrExtractionLimit }
func (e *extractionLimitError) Unwrap() error        { return ErrExtractionLimit }

// markdownBuilder wraps bytes.Buffer with a hard cap. Any write that would
// push Len() past max returns *extractionLimitError{Kind:"markdown_size"}.
// The buffer state is left as it was before the failing write — no partial
// output beyond the cap.
type markdownBuilder struct {
	buf bytes.Buffer
	max int64
}

func newMarkdownBuilder(maxBytes int64) *markdownBuilder { return &markdownBuilder{max: maxBytes} }

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

func (m *markdownBuilder) Len() int       { return m.buf.Len() }
func (m *markdownBuilder) String() string { return m.buf.String() }

// ooxmlLimits is the subset of ContentExtractorsConfig that the opener and
// XML decoder care about. Decoupled from internal/config to keep the api
// package free of config imports for unit-test simplicity.
//
// PPTXSlides bounds the number of slides processed by the PPTX extractor;
// it mirrors internal/config.ContentExtractorsConfig.PPTXSlides.
// XLSXCells bounds the cumulative number of cells processed by the XLSX
// extractor across all visible sheets; it mirrors
// internal/config.ContentExtractorsConfig.XLSXCells.
type ooxmlLimits struct {
	CompressedSizeBytes   int64
	DecompressedSizeBytes int64
	PartSizeBytes         int64
	MarkdownSizeBytes     int64
	MaxXMLElementDepth    int
	MaxCompressionRatio   int64
	PPTXSlides            int
	XLSXCells             int
}

// defaultOOXMLLimits returns the design-spec default values; used by tests
// that don't care about specific limits.
func defaultOOXMLLimits() ooxmlLimits {
	return ooxmlLimits{
		CompressedSizeBytes:   20 * 1024 * 1024,
		DecompressedSizeBytes: 50 * 1024 * 1024,
		PartSizeBytes:         20 * 1024 * 1024,
		MarkdownSizeBytes:     128 * 1024,
		MaxXMLElementDepth:    100,
		MaxCompressionRatio:   100,
		PPTXSlides:            100,
		XLSXCells:             1000,
	}
}

// ooxmlOpener wraps archive/zip with limit enforcement and security
// checks. It refuses oversize inputs up front, rejects path traversal /
// absolute paths / backslashes, and gates per-member reads through
// boundedReader so that streaming decoders trip mid-read on overrun.
type ooxmlOpener struct{ limits ooxmlLimits }

func newOOXMLOpener(l ooxmlLimits) *ooxmlOpener { return &ooxmlOpener{limits: l} }

type ooxmlArchive struct {
	zr       *zip.Reader
	limits   ooxmlLimits
	consumed int64 // running cumulative decompressed bytes across all members
	ctx      context.Context
}

// WithContext sets the context used by all subsequent boundedReaders
// returned by openMember to abort their reads on cancellation. Used by
// ContextAwareExtractor implementations to wire wall-clock cancellation
// through to in-flight I/O. A nil ctx disables cooperative cancellation
// (the default).
func (a *ooxmlArchive) WithContext(ctx context.Context) { a.ctx = ctx }

// open performs up-front compressed-size + path-shape checks and returns an
// archive handle. It does not decompress yet — that happens member-by-member.
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
type boundedReader struct {
	under         io.Reader
	closer        io.Closer
	archive       *ooxmlArchive
	partCap       int64
	partRead      int64
	memberID      string
	extractionCtx context.Context
}

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
type boundedXMLDecoder struct {
	dec      *xml.Decoder
	depth    int
	maxDepth int
}

func newBoundedXMLDecoder(r io.Reader, maxDepth int) *boundedXMLDecoder {
	return &boundedXMLDecoder{dec: xml.NewDecoder(r), maxDepth: maxDepth}
}

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
func ooxmlLoadCoreTitle(arch *ooxmlArchive, limits ooxmlLimits) (string, error) {
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

// extractWithDeadline runs fn under a fresh context with the given budget.
// On timeout it returns context.DeadlineExceeded; on parent cancel it
// returns ctx.Err(). The wrapped fn receives the deadline-bearing context
// so that cooperative cancellation is possible.
func extractWithDeadline(ctx context.Context, budget time.Duration, fn func(context.Context) (ExtractedContent, error)) (ExtractedContent, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
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
type ctxReader struct {
	r   io.Reader
	ctx context.Context
}

func newCtxReader(ctx context.Context, r io.Reader) *ctxReader { return &ctxReader{r: r, ctx: ctx} }

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// ConcurrencyLimiter caps simultaneous extractions per user. Capacity is
// looked up on first acquire and cached per-user for the lifetime of the
// process (override changes don't resize the existing semaphore — known
// limitation, see design spec). The lookup callback is invoked while the
// internal mutex is held, so callers must supply a fast (cached) lookup.
type ConcurrencyLimiter struct {
	mu       sync.Mutex
	sems     map[string]*semaphore.Weighted
	lookup   func(ctx context.Context, userID string) (int, error)
	fallback int
}

// NewConcurrencyLimiter is the public constructor used by server wiring.
// fallback is the per-user concurrency cap used when no override is set;
// lookup is called on first acquire per user to fetch the override value.
// A nil lookup means "always use fallback". Values outside (0,
// config.MaxPerUserConcurrency] are clamped to the safe default of 2.
func NewConcurrencyLimiter(fallback int, lookup func(ctx context.Context, userID string) (int, error)) *ConcurrencyLimiter {
	if fallback <= 0 || fallback > config.MaxPerUserConcurrency {
		fallback = 2
	}
	return &ConcurrencyLimiter{
		sems:     map[string]*semaphore.Weighted{},
		lookup:   lookup,
		fallback: fallback,
	}
}

func (cl *ConcurrencyLimiter) acquire(ctx context.Context, userID string) (release func(), err error) {
	cl.mu.Lock()
	sem, ok := cl.sems[userID]
	if !ok {
		n := cl.fallback
		if cl.lookup != nil {
			if got, lerr := cl.lookup(ctx, userID); lerr == nil && got > 0 && got <= config.MaxPerUserConcurrency {
				n = got
			}
		}
		sem = semaphore.NewWeighted(int64(n))
		cl.sems[userID] = sem
	}
	cl.mu.Unlock()
	if err := sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	return func() { sem.Release(1) }, nil
}

// OOXMLLimitsFromConfig converts a validated ContentExtractorsConfig into the
// api package's internal ooxmlLimits. Used by server wiring; tests can
// continue to use defaultOOXMLLimits().
//
// MaxXMLElementDepth and MaxCompressionRatio are server-only ceilings (not
// operator-tunable) and are populated with the const ceilings here so the
// extractors see consistent values regardless of caller.
func OOXMLLimitsFromConfig(c config.ContentExtractorsConfig) ooxmlLimits {
	return ooxmlLimits{
		CompressedSizeBytes:   c.CompressedSizeBytes,
		DecompressedSizeBytes: c.DecompressedSizeBytes,
		PartSizeBytes:         c.PartSizeBytes,
		MarkdownSizeBytes:     c.MarkdownSizeBytes,
		MaxXMLElementDepth:    100,
		MaxCompressionRatio:   100,
		PPTXSlides:            c.PPTXSlides,
		XLSXCells:             c.XLSXCells,
	}
}
