package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContentExtractorsConfig_Defaults(t *testing.T) {
	c := DefaultContentExtractorsConfig()
	assert.EqualValues(t, 20*1024*1024, c.CompressedSizeBytes)
	assert.EqualValues(t, 50*1024*1024, c.DecompressedSizeBytes)
	assert.EqualValues(t, 20*1024*1024, c.PartSizeBytes)
	assert.Equal(t, 100, c.PPTXSlides)
	assert.Equal(t, 1000, c.XLSXCells)
	assert.EqualValues(t, 128*1024, c.MarkdownSizeBytes)
	assert.Equal(t, 30*time.Second, c.WallClockBudget)
	assert.Equal(t, 2, c.PerUserConcurrencyDefault)
}

func TestContentExtractorsConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ContentExtractorsConfig)
		wantErr string
	}{
		{"valid defaults", func(c *ContentExtractorsConfig) {}, ""},
		{"compressed > ceiling", func(c *ContentExtractorsConfig) { c.CompressedSizeBytes = 60 * 1024 * 1024 }, "compressed_size_bytes"},
		{"compressed zero", func(c *ContentExtractorsConfig) { c.CompressedSizeBytes = 0 }, "compressed_size_bytes"},
		{"decompressed > ceiling", func(c *ContentExtractorsConfig) { c.DecompressedSizeBytes = 200 * 1024 * 1024 }, "decompressed_size_bytes"},
		{"part > ceiling", func(c *ContentExtractorsConfig) { c.PartSizeBytes = 60 * 1024 * 1024 }, "part_size_bytes"},
		{"slides > ceiling", func(c *ContentExtractorsConfig) { c.PPTXSlides = 251 }, "pptx_slides"},
		{"cells > ceiling", func(c *ContentExtractorsConfig) { c.XLSXCells = 10001 }, "xlsx_cells"},
		{"markdown > ceiling", func(c *ContentExtractorsConfig) { c.MarkdownSizeBytes = 257 * 1024 }, "markdown_size_bytes"},
		{"wall clock > ceiling", func(c *ContentExtractorsConfig) { c.WallClockBudget = 61 * time.Second }, "wall_clock_budget"},
		{"wall clock zero", func(c *ContentExtractorsConfig) { c.WallClockBudget = 0 }, "wall_clock_budget"},
		{"per-user > ceiling", func(c *ContentExtractorsConfig) { c.PerUserConcurrencyDefault = 17 }, "per_user_concurrency_default"},
		{"per-user zero", func(c *ContentExtractorsConfig) { c.PerUserConcurrencyDefault = 0 }, "per_user_concurrency_default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultContentExtractorsConfig()
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
