package extract

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyError_LimitsMappedCorrectly(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		want       string
		wantDetail string
	}{
		{"compressed_size", &extractionLimitError{Kind: "compressed_size"}, ReasonExtractionLimitCompressedSize, ""},
		{"decompressed_size", &extractionLimitError{Kind: "decompressed_size"}, ReasonExtractionLimitDecompressedSize, ""},
		{"part_size", &extractionLimitError{Kind: "part_size", Detail: "word/document.xml"}, ReasonExtractionLimitPartSize, "word/document.xml"},
		{"part_count_with_detail", &extractionLimitError{Kind: "part_count", Detail: "slide #42"}, ReasonExtractionLimitPartCount, "slide #42"},
		{"markdown_size", &extractionLimitError{Kind: "markdown_size"}, ReasonExtractionLimitMarkdownSize, ""},
		{"xml_depth", &extractionLimitError{Kind: "xml_depth"}, ReasonExtractionLimitXMLDepth, ""},
		{"zip_nested", &extractionLimitError{Kind: "zip_nested", Detail: "nested.zip"}, ReasonExtractionLimitZipNested, "nested.zip"},
		{"zip_path", &extractionLimitError{Kind: "zip_path"}, ReasonExtractionLimitZipPath, ""},
		{"compression_ratio", &extractionLimitError{Kind: "compression_ratio"}, ReasonExtractionLimitCompressionRatio, ""},
		{"malformed_wrapped", fmt.Errorf("wrap: %w", ErrMalformed), ReasonExtractionMalformed, ""},
		{"unsupported_wrapped", fmt.Errorf("wrap: %w", ErrUnsupported), ReasonExtractionUnsupported, ""},
		{"context.DeadlineExceeded", context.DeadlineExceeded, ReasonExtractionLimitTimeout, ""},
		{"internal", errors.New("random failure"), ReasonExtractionInternal, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ClassifyError(c.err)
			assert.Equal(t, c.want, r.ReasonCode)
			assert.Equal(t, c.wantDetail, r.ReasonDetail)
		})
	}
}

func TestClassifyError_NilReturnsZero(t *testing.T) {
	r := ClassifyError(nil)
	assert.Equal(t, "", r.ReasonCode)
	assert.Equal(t, "", r.ReasonDetail)
}

// TestNewLimitError_ClassifiesAndMatchesSentinel verifies the public
// limit-error constructor produces a value that errors.Is matches against
// ErrExtractionLimit and that ClassifyError maps its Kind/Detail through.
func TestNewLimitError_ClassifiesAndMatchesSentinel(t *testing.T) {
	err := NewLimitError("part_count", "slide #101")
	assert.True(t, errors.Is(err, ErrExtractionLimit))
	r := ClassifyError(err)
	assert.Equal(t, ReasonExtractionLimitPartCount, r.ReasonCode)
	assert.Equal(t, "slide #101", r.ReasonDetail)
}
