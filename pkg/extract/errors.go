package extract

import (
	"context"
	"errors"
	"fmt"
)

// Sentinel errors returned by extractors. Callers use errors.Is to classify.
var (
	ErrExtractionLimit = errors.New("extraction limit exceeded")
	ErrMalformed       = errors.New("malformed document")
	ErrUnsupported     = errors.New("unsupported document subformat")
)

// Extraction access-reason-code constants. Relocated verbatim from
// api/access_diagnostics.go so the monolith and the worker agree on the
// strings written to access_reason_code / the result envelope.
const (
	ReasonExtractionLimitCompressedSize   = "extraction_limit:compressed_size"
	ReasonExtractionLimitDecompressedSize = "extraction_limit:decompressed_size"
	ReasonExtractionLimitPartSize         = "extraction_limit:part_size"
	ReasonExtractionLimitPartCount        = "extraction_limit:part_count"
	ReasonExtractionLimitMarkdownSize     = "extraction_limit:markdown_size"
	ReasonExtractionLimitTimeout          = "extraction_limit:timeout"
	ReasonExtractionLimitXMLDepth         = "extraction_limit:xml_depth"
	ReasonExtractionLimitZipNested        = "extraction_limit:zip_nested"
	ReasonExtractionLimitZipPath          = "extraction_limit:zip_path"
	ReasonExtractionLimitCompressionRatio = "extraction_limit:compression_ratio"
	ReasonExtractionMalformed             = "extraction_malformed"
	ReasonExtractionUnsupported           = "extraction_unsupported"
	ReasonExtractionInternal              = "extraction_internal"
)

// extractionLimitError describes which limit tripped during extraction. The
// API surface (Kind values) is stable: the pipeline maps Kind into
// access_reason_code.
//
// Kind values: compressed_size | decompressed_size | part_size | part_count |
// markdown_size | timeout | xml_depth | zip_nested | zip_path | compression_ratio
//
// Kept package-private — callers use errors.Is(err, ErrExtractionLimit)
// and ClassifyError to consume it.
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

// Classification describes how a typed extractor error maps to a reason
// code, plus an optional human-readable Detail. Relocated from
// api.ExtractionClassification; the Status field is dropped because
// access_status is a monolith concept — the worker reports only reason
// codes, and the monolith's result-consumer (Plan 3) derives access_status.
type Classification struct {
	ReasonCode   string
	ReasonDetail string
}

// ClassifyError walks the error chain and returns the matching reason code.
// Default is ReasonExtractionInternal. A nil error returns the zero value.
func ClassifyError(err error) Classification {
	if err == nil {
		return Classification{}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Classification{ReasonCode: ReasonExtractionLimitTimeout}
	}
	var le *extractionLimitError
	if errors.As(err, &le) {
		var code string
		switch le.Kind {
		case "compressed_size":
			code = ReasonExtractionLimitCompressedSize
		case "decompressed_size":
			code = ReasonExtractionLimitDecompressedSize
		case "part_size":
			code = ReasonExtractionLimitPartSize
		case "part_count":
			code = ReasonExtractionLimitPartCount
		case "markdown_size":
			code = ReasonExtractionLimitMarkdownSize
		case "xml_depth":
			code = ReasonExtractionLimitXMLDepth
		case "zip_nested":
			code = ReasonExtractionLimitZipNested
		case "zip_path":
			code = ReasonExtractionLimitZipPath
		case "compression_ratio":
			code = ReasonExtractionLimitCompressionRatio
		}
		if code != "" {
			return Classification{ReasonCode: code, ReasonDetail: le.Detail}
		}
	}
	if errors.Is(err, ErrMalformed) {
		return Classification{ReasonCode: ReasonExtractionMalformed}
	}
	if errors.Is(err, ErrUnsupported) {
		return Classification{ReasonCode: ReasonExtractionUnsupported}
	}
	return Classification{ReasonCode: ReasonExtractionInternal}
}
