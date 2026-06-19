package extract

import "time"

// Limits is the set of extraction caps the OOXML opener, the XML decoder,
// and the wall-clock budget enforce. Relocated and exported from the
// package-private ooxmlLimits.
// SEM@f7dfe970572e2574027691de97c695d5ae39d5b7: extraction size and depth caps applied to OOXML, XML decoding, and wall-clock budget (pure)
type Limits struct {
	CompressedSizeBytes   int64
	DecompressedSizeBytes int64
	PartSizeBytes         int64
	MarkdownSizeBytes     int64
	MaxXMLElementDepth    int
	MaxCompressionRatio   int64
	PPTXSlides            int
	XLSXCells             int
	// WallClockBudget is the per-extraction deadline (0 disables).
	WallClockBudget time.Duration
}

// DefaultLimits returns the design-spec default values.
// SEM@f7dfe970572e2574027691de97c695d5ae39d5b7: return design-spec default extraction limits (pure)
func DefaultLimits() Limits {
	return Limits{
		CompressedSizeBytes:   20 * 1024 * 1024,
		DecompressedSizeBytes: 50 * 1024 * 1024,
		PartSizeBytes:         20 * 1024 * 1024,
		MarkdownSizeBytes:     128 * 1024,
		MaxXMLElementDepth:    100,
		MaxCompressionRatio:   100,
		PPTXSlides:            100,
		XLSXCells:             1000,
		WallClockBudget:       30 * time.Second,
	}
}
