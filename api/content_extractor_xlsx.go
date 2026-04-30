package api

import (
	"bytes"
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
// Markdown-flavored text into a bounded markdown builder. Each sheet is
// rendered as `## Sheet: <name>` followed by a pipe table of trimmed cells.
//
// On non-nil error, the returned ExtractedContent is zero and must be discarded.
func (e *XLSXExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
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

		if err := xlsxRenderSheet(f, sheet, mb, e.limits, &cellsTotal); err != nil {
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
func xlsxRenderSheet(f *excelize.File, sheet string, mb *markdownBuilder, limits ooxmlLimits, cellsTotal *int) error {
	rows, err := f.Rows(sheet)
	if err != nil {
		return fmt.Errorf("%w: rows %s: %w", ErrMalformed, sheet, err)
	}
	defer func() { _ = rows.Close() }()

	// Buffer rows into [][]string, then trim and render.
	// Phase A: simple — no style-fingerprint header detection (treat row 1 as header).
	var data [][]string
	for rows.Next() {
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

	// Trim trailing empty rows + trailing empty columns
	data = xlsxTrimRows(data)
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

// xlsxTrimRows removes trailing empty rows. Empty = all cells empty after trim.
func xlsxTrimRows(rows [][]string) [][]string {
	end := len(rows)
	for end > 0 {
		empty := true
		for _, c := range rows[end-1] {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if !empty {
			break
		}
		end--
	}
	return rows[:end]
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
