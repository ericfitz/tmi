package config

import (
	"fmt"
	"time"
)

// Hardcoded ceilings — server-only protection. Operators cannot override.
const (
	maxCompressedSizeBytes   = int64(50 * 1024 * 1024)
	maxDecompressedSizeBytes = int64(100 * 1024 * 1024)
	maxPartSizeBytes         = int64(50 * 1024 * 1024)
	maxPPTXSlides            = 250
	maxXLSXCells             = 10000
	maxMarkdownSizeBytes     = int64(256 * 1024)
	maxWallClockBudget       = 60 * time.Second
	maxPerUserConcurrency    = 16
)

// ContentExtractorsConfig holds operator-tunable defaults for the OOXML
// extractor pipeline. Each value must be > 0 and <= the corresponding
// hardcoded ceiling.
type ContentExtractorsConfig struct {
	CompressedSizeBytes       int64         `yaml:"compressed_size_bytes" env:"TMI_CONTENT_EXTRACTORS_COMPRESSED_SIZE_BYTES"`
	DecompressedSizeBytes     int64         `yaml:"decompressed_size_bytes" env:"TMI_CONTENT_EXTRACTORS_DECOMPRESSED_SIZE_BYTES"`
	PartSizeBytes             int64         `yaml:"part_size_bytes" env:"TMI_CONTENT_EXTRACTORS_PART_SIZE_BYTES"`
	PPTXSlides                int           `yaml:"pptx_slides" env:"TMI_CONTENT_EXTRACTORS_PPTX_SLIDES"`
	XLSXCells                 int           `yaml:"xlsx_cells" env:"TMI_CONTENT_EXTRACTORS_XLSX_CELLS"`
	MarkdownSizeBytes         int64         `yaml:"markdown_size_bytes" env:"TMI_CONTENT_EXTRACTORS_MARKDOWN_SIZE_BYTES"`
	WallClockBudget           time.Duration `yaml:"wall_clock_budget" env:"TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET"`
	PerUserConcurrencyDefault int           `yaml:"per_user_concurrency_default" env:"TMI_CONTENT_EXTRACTORS_PER_USER_CONCURRENCY_DEFAULT"`
}

// DefaultContentExtractorsConfig returns the project-wide defaults documented
// in the OOXML design spec.
func DefaultContentExtractorsConfig() ContentExtractorsConfig {
	return ContentExtractorsConfig{
		CompressedSizeBytes:       20 * 1024 * 1024,
		DecompressedSizeBytes:     50 * 1024 * 1024,
		PartSizeBytes:             20 * 1024 * 1024,
		PPTXSlides:                100,
		XLSXCells:                 1000,
		MarkdownSizeBytes:         128 * 1024,
		WallClockBudget:           30 * time.Second,
		PerUserConcurrencyDefault: 2,
	}
}

// Validate enforces > 0 and <= ceiling for every field.
func (c ContentExtractorsConfig) Validate() error {
	check := func(name string, v, ceiling int64) error {
		if v <= 0 {
			return fmt.Errorf("content_extractors.%s must be > 0 (got %d)", name, v)
		}
		if v > ceiling {
			return fmt.Errorf("content_extractors.%s must be <= %d (got %d)", name, ceiling, v)
		}
		return nil
	}
	if err := check("compressed_size_bytes", c.CompressedSizeBytes, maxCompressedSizeBytes); err != nil {
		return err
	}
	if err := check("decompressed_size_bytes", c.DecompressedSizeBytes, maxDecompressedSizeBytes); err != nil {
		return err
	}
	if err := check("part_size_bytes", c.PartSizeBytes, maxPartSizeBytes); err != nil {
		return err
	}
	if err := check("pptx_slides", int64(c.PPTXSlides), int64(maxPPTXSlides)); err != nil {
		return err
	}
	if err := check("xlsx_cells", int64(c.XLSXCells), int64(maxXLSXCells)); err != nil {
		return err
	}
	if err := check("markdown_size_bytes", c.MarkdownSizeBytes, maxMarkdownSizeBytes); err != nil {
		return err
	}
	if c.WallClockBudget <= 0 {
		return fmt.Errorf("content_extractors.wall_clock_budget must be > 0 (got %s)", c.WallClockBudget)
	}
	if c.WallClockBudget > maxWallClockBudget {
		return fmt.Errorf("content_extractors.wall_clock_budget must be <= %s (got %s)", maxWallClockBudget, c.WallClockBudget)
	}
	if err := check("per_user_concurrency_default", int64(c.PerUserConcurrencyDefault), int64(maxPerUserConcurrency)); err != nil {
		return err
	}
	return nil
}
