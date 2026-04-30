package api

import (
	"bytes"
	"errors"
	"fmt"
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
type extractionLimitError struct {
	Kind     string // compressed_size | decompressed_size | part_size | part_count |
	// markdown_size | timeout | xml_depth | zip_nested | zip_path | compression_ratio
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

func newMarkdownBuilder(max int64) *markdownBuilder { return &markdownBuilder{max: max} }

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
