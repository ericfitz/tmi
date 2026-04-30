package api

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// XLSXExtractor extracts Markdown-flavored text from an XLSX (OOXML) workbook.
// Uses github.com/xuri/excelize/v2 for archive parsing, shared-strings handling,
// and number-format rendering. Limit enforcement (cell count, markdown cap) is
// applied during the streaming render.
//
// NOTE: Phase A treats the first row of each sheet as the markdown table
// header. The full style-fingerprint header detection algorithm from the
// design spec is deferred to a follow-up.
type XLSXExtractor struct{ limits ooxmlLimits }

// NewXLSXExtractor returns an extractor configured with the given limits.
func NewXLSXExtractor(limits ooxmlLimits) *XLSXExtractor { return &XLSXExtractor{limits: limits} }

// Name returns the extractor name as registered with the registry.
func (e *XLSXExtractor) Name() string { return "xlsx" }

// CanHandle returns true iff contentType is the XLSX OOXML MIME type.
func (e *XLSXExtractor) CanHandle(contentType string) bool {
	return strings.EqualFold(contentType, xlsxContentType)
}

// Bounded marks XLSXExtractor as needing a wall-clock deadline.
func (e *XLSXExtractor) Bounded() bool { return true }

// Extract opens the workbook, walks visible sheets in tab order, and writes
// Markdown-flavored text into a bounded markdown builder. This is the
// legacy entry point that delegates to ExtractCtx with a background
// context (no cooperative cancellation).
//
// On non-nil error, the returned ExtractedContent is zero and must be discarded.
func (e *XLSXExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	return e.ExtractCtx(context.Background(), data, contentType)
}

// ExtractCtx is the context-aware extraction entry point. Each visible sheet
// is rendered as `## Sheet: <name>` followed by a pipe table of trimmed
// cells. The supplied ctx is checked between sheets and inside the row
// loop so wall-clock cancellation aborts long workbooks promptly.
//
// On non-nil error, the returned ExtractedContent is zero and must be discarded.
func (e *XLSXExtractor) ExtractCtx(ctx context.Context, data []byte, contentType string) (ExtractedContent, error) {
	// Up-front compressed-size guard (parity with DOCX/PPTX which get this via
	// ooxmlOpener.open()). excelize doesn't enforce this at the archive level,
	// only the post-decompression UnzipSizeLimit.
	if int64(len(data)) > e.limits.CompressedSizeBytes {
		return ExtractedContent{}, &extractionLimitError{
			Kind:     "compressed_size",
			Limit:    e.limits.CompressedSizeBytes,
			Observed: int64(len(data)),
		}
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit:    e.limits.DecompressedSizeBytes,
		UnzipXMLSizeLimit: e.limits.PartSizeBytes,
	})
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("%w: open: %w", ErrMalformed, err)
	}
	defer func() { _ = f.Close() }()

	mb := newMarkdownBuilder(e.limits.MarkdownSizeBytes)
	var title string
	cellsTotal := 0 // running count across sheets toward XLSXCells limit

	sheets := f.GetSheetList()
	first := true
	for _, sheet := range sheets {
		if cerr := ctx.Err(); cerr != nil {
			return ExtractedContent{}, cerr
		}
		// Skip hidden sheets
		visible, _ := f.GetSheetVisible(sheet)
		if !visible {
			continue
		}
		if first {
			title = sheet
			first = false
		} else {
			if _, err := mb.WriteString("\n\n"); err != nil {
				return ExtractedContent{}, err
			}
		}
		if _, err := mb.WriteString("## Sheet: " + sheet); err != nil {
			return ExtractedContent{}, err
		}

		if err := xlsxRenderSheet(ctx, f, sheet, mb, e.limits, &cellsTotal); err != nil {
			return ExtractedContent{}, err
		}
	}

	return ExtractedContent{
		Text:        strings.TrimRight(mb.String(), "\n"),
		Title:       title,
		ContentType: contentType,
	}, nil
}

// xlsxRenderSheet writes one sheet's rows as a markdown table.
// cellsTotal is updated in-place; trips part_count when it exceeds limits.XLSXCells.
// The ctx is checked at the top of every row iteration so wall-clock
// cancellation breaks out of long sheets promptly.
func xlsxRenderSheet(ctx context.Context, f *excelize.File, sheet string, mb *markdownBuilder, limits ooxmlLimits, cellsTotal *int) error {
	rows, err := f.Rows(sheet)
	if err != nil {
		return fmt.Errorf("%w: rows %s: %w", ErrMalformed, sheet, err)
	}
	defer func() { _ = rows.Close() }()

	// Buffer rows into [][]string, then trim and render.
	// Phase A: simple — no style-fingerprint header detection (treat row 1 as header).
	var data [][]string
	for rows.Next() {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		cols, err := rows.Columns()
		if err != nil {
			return fmt.Errorf("%w: cols %s: %w", ErrMalformed, sheet, err)
		}
		for range cols {
			*cellsTotal++
			if *cellsTotal > limits.XLSXCells {
				return &extractionLimitError{
					Kind: "part_count", Limit: int64(limits.XLSXCells), Observed: int64(*cellsTotal),
					Detail: fmt.Sprintf("xlsx cells (sheet %q)", sheet),
				}
			}
		}
		data = append(data, cols)
	}
	if err := rows.Error(); err != nil {
		return fmt.Errorf("%w: row iteration %s: %w", ErrMalformed, sheet, err)
	}

	// Trim leading + trailing empty rows, and trailing empty columns.
	data = xlsxTrim(data)
	if len(data) == 0 {
		return nil // nothing to render
	}
	// Equal-length rows for table rendering
	cols := 0
	for _, r := range data {
		if len(r) > cols {
			cols = len(r)
		}
	}
	for i := range data {
		for len(data[i]) < cols {
			data[i] = append(data[i], "")
		}
	}

	// Render markdown pipe table — Phase A treats first row as header.
	if _, err := mb.WriteString("\n\n"); err != nil {
		return err
	}
	if err := xlsxWriteRow(mb, data[0]); err != nil {
		return err
	}
	if err := xlsxWriteSeparator(mb, cols); err != nil {
		return err
	}
	for _, row := range data[1:] {
		if err := xlsxWriteRow(mb, row); err != nil {
			return err
		}
	}
	return nil
}

// xlsxTrim removes leading + trailing empty rows, and leading + trailing empty
// columns. Empty = all cells empty after strings.TrimSpace. The column trim
// finds the leftmost and rightmost column indices that contain a non-empty
// cell across all rows and truncates every row to [minCol, maxCol]; this drops
// the unused leading/trailing columns excelize emits when data starts
// mid-sheet (e.g., at B3) so the rendered table doesn't carry an empty
// leading column.
func xlsxTrim(rows [][]string) [][]string {
	// Trim trailing empty rows.
	end := len(rows)
	for end > 0 {
		if !xlsxRowEmpty(rows[end-1]) {
			break
		}
		end--
	}
	rows = rows[:end]

	// Trim leading empty rows.
	start := 0
	for start < len(rows) {
		if !xlsxRowEmpty(rows[start]) {
			break
		}
		start++
	}
	rows = rows[start:]

	if len(rows) == 0 {
		return rows
	}

	// Find leftmost + rightmost non-empty column across all rows.
	minCol, maxCol := -1, -1
	for _, r := range rows {
		for c := 0; c < len(r); c++ {
			if strings.TrimSpace(r[c]) != "" {
				if minCol < 0 || c < minCol {
					minCol = c
				}
				if c > maxCol {
					maxCol = c
				}
			}
		}
	}
	if maxCol < 0 {
		return nil
	}
	for i := range rows {
		if len(rows[i]) > maxCol+1 {
			rows[i] = rows[i][:maxCol+1]
		}
		if minCol > 0 && len(rows[i]) > minCol {
			rows[i] = rows[i][minCol:]
		} else if minCol > 0 {
			// Row was shorter than minCol — make it empty so equal-length
			// padding fills it.
			rows[i] = nil
		}
	}
	return rows
}

// xlsxRowEmpty reports whether every cell is empty after TrimSpace.
func xlsxRowEmpty(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// xlsxWriteRow writes one pipe-delimited row, escaping `|` in cell content.
func xlsxWriteRow(mb *markdownBuilder, cells []string) error {
	if _, err := mb.WriteString("| "); err != nil {
		return err
	}
	for i, c := range cells {
		c = strings.ReplaceAll(c, "|", `\|`)
		c = strings.ReplaceAll(c, "\n", " ")
		if _, err := mb.WriteString(c); err != nil {
			return err
		}
		if i < len(cells)-1 {
			if _, err := mb.WriteString(" | "); err != nil {
				return err
			}
		}
	}
	if _, err := mb.WriteString(" |\n"); err != nil {
		return err
	}
	return nil
}

// xlsxWriteSeparator writes the markdown table header separator row.
func xlsxWriteSeparator(mb *markdownBuilder, cols int) error {
	if _, err := mb.WriteString("|"); err != nil {
		return err
	}
	for i := 0; i < cols; i++ {
		if _, err := mb.WriteString(" --- |"); err != nil {
			return err
		}
	}
	if _, err := mb.WriteString("\n"); err != nil {
		return err
	}
	return nil
}
