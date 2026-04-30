package api

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

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
type ooxmlLimits struct {
	CompressedSizeBytes   int64
	DecompressedSizeBytes int64
	PartSizeBytes         int64
	MarkdownSizeBytes     int64
	MaxXMLElementDepth    int
	MaxCompressionRatio   int64
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
}

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
		return nil, fmt.Errorf("%w: zip read: %v", ErrMalformed, err)
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

// openMember opens a single member by exact name, returning a reader that
// enforces per-part + cumulative + ratio limits. Returns ErrMalformed-wrapped
// error if the member doesn't exist. Returns *extractionLimitError if the
// member is a nested zip (sniffed by header).
func (a *ooxmlArchive) openMember(name string) (io.Reader, error) {
	for _, f := range a.zr.File {
		if f.Name != name {
			continue
		}
		if int64(f.UncompressedSize64) > a.limits.PartSizeBytes {
			return nil, &extractionLimitError{
				Kind: "part_size", Limit: a.limits.PartSizeBytes, Observed: int64(f.UncompressedSize64),
				Detail: name,
			}
		}
		// Compression-ratio sanity: only enforce when we have a non-zero
		// compressed size to compare against.
		if f.CompressedSize64 > 0 {
			ratio := int64(f.UncompressedSize64) / int64(f.CompressedSize64)
			if ratio > a.limits.MaxCompressionRatio {
				return nil, &extractionLimitError{
					Kind: "compression_ratio", Limit: a.limits.MaxCompressionRatio, Observed: ratio,
					Detail: name,
				}
			}
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: open %s: %v", ErrMalformed, name, err)
		}
		// Sniff header for nested-zip refusal.
		header := make([]byte, 4)
		n, _ := io.ReadFull(rc, header)
		if n == 4 && bytes.Equal(header[:4], []byte{0x50, 0x4b, 0x03, 0x04}) {
			_ = rc.Close()
			return nil, &extractionLimitError{Kind: "zip_nested", Limit: 0, Observed: 0, Detail: name}
		}
		return &boundedReader{
			under:    io.MultiReader(bytes.NewReader(header[:n]), rc),
			closer:   rc,
			archive:  a,
			partCap:  a.limits.PartSizeBytes,
			partRead: 0,
			memberID: name,
		}, nil
	}
	return nil, fmt.Errorf("%w: missing required part %q", ErrMalformed, name)
}

// boundedReader enforces per-part and cumulative-decompressed limits as it
// streams. archive.consumed is updated on every Read so that subsequent
// member opens see the running total.
type boundedReader struct {
	under    io.Reader
	closer   io.Closer
	archive  *ooxmlArchive
	partCap  int64
	partRead int64
	memberID string
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
	return n, err
}

func (b *boundedReader) Close() error {
	if b.closer != nil {
		return b.closer.Close()
	}
	return nil
}
