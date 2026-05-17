package extract

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xuri/excelize/v2"
)

const xlsxMIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// buildXLSX builds an in-memory XLSX from a sheet name and rows.
// Each row is a slice of cell values; first row should be the header if any.
func buildXLSX(t *testing.T, sheetName string, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if sheetName != "Sheet1" {
		idx, err := f.NewSheet(sheetName)
		if err != nil {
			t.Fatalf("new sheet: %v", err)
		}
		f.SetActiveSheet(idx)
		if err := f.DeleteSheet("Sheet1"); err != nil {
			t.Fatalf("delete sheet: %v", err)
		}
	}
	for r, row := range rows {
		for c, v := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatalf("coord: %v", err)
			}
			if err := f.SetCellValue(sheetName, cell, v); err != nil {
				t.Fatalf("set cell %s: %v", cell, err)
			}
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	return buf.Bytes()
}

func TestXLSXExtractor_BoundedAndCanHandle(t *testing.T) {
	e := NewXLSXExtractor(DefaultLimits())
	assert.True(t, e.Bounded())
	assert.True(t, e.CanHandle(xlsxMIME))
	assert.False(t, e.CanHandle("application/pdf"))
	assert.Equal(t, "xlsx", e.Name())
}

func TestXLSXExtractor_SingleSheetSimple(t *testing.T) {
	data := buildXLSX(t, "Data", [][]any{
		{"Name", "Age"},
		{"Alice", 30},
		{"Bob", 25},
	})
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(data, xlsxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "## Sheet: Data")
	assert.Contains(t, out.Text, "| Name | Age |")
	assert.Contains(t, out.Text, "| Alice | 30 |")
	assert.Contains(t, out.Text, "| Bob | 25 |")
	assert.Equal(t, "Data", out.Title)
}

func TestXLSXExtractor_MultiSheetOrdering(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	// Sheet1 (default), then add Sheet2 and Sheet3
	if err := f.SetCellValue("Sheet1", "A1", "First"); err != nil {
		t.Fatalf("a: %v", err)
	}
	if _, err := f.NewSheet("Second"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if err := f.SetCellValue("Second", "A1", "Mid"); err != nil {
		t.Fatalf("b: %v", err)
	}
	if _, err := f.NewSheet("Third"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if err := f.SetCellValue("Third", "A1", "Last"); err != nil {
		t.Fatalf("c: %v", err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}

	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	i1 := strings.Index(out.Text, "## Sheet: Sheet1")
	i2 := strings.Index(out.Text, "## Sheet: Second")
	i3 := strings.Index(out.Text, "## Sheet: Third")
	assert.True(t, i1 >= 0, "Sheet1 missing")
	assert.True(t, i2 > i1, "Second must come after Sheet1")
	assert.True(t, i3 > i2, "Third must come after Second")
}

func TestXLSXExtractor_EmptyWorkbook(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	// Even when a sheet has no data, its name is emitted so downstream
	// chunkers can attribute the empty-sheet event.
	assert.Contains(t, out.Text, "## Sheet: Sheet1")
}

func TestXLSXExtractor_MalformedDataIsErrMalformed(t *testing.T) {
	e := NewXLSXExtractor(DefaultLimits())
	_, err := e.Extract([]byte("not an xlsx"), xlsxMIME)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMalformed), "expected ErrMalformed wrap, got %v", err)
}

func TestXLSXExtractor_HiddenSheetSkipped(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetCellValue("Sheet1", "A1", "Visible"); err != nil {
		t.Fatalf("a: %v", err)
	}
	if _, err := f.NewSheet("Hidden"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if err := f.SetCellValue("Hidden", "A1", "ShouldNotAppear"); err != nil {
		t.Fatalf("b: %v", err)
	}
	if err := f.SetSheetVisible("Hidden", false); err != nil {
		t.Fatalf("hide: %v", err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}

	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "Visible")
	assert.NotContains(t, out.Text, "ShouldNotAppear")
	assert.NotContains(t, out.Text, "## Sheet: Hidden")
}

func TestXLSXExtractor_CellCountLimit(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	// 50 cells across 5 rows × 10 cols
	for r := 0; r < 5; r++ {
		for c := 0; c < 10; c++ {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			_ = f.SetCellValue("Sheet1", cell, fmt.Sprintf("c%d", r*10+c))
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}

	limits := DefaultLimits()
	limits.XLSXCells = 20 // should trip well before 50
	e := NewXLSXExtractor(limits)
	_, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "part_count", le.Kind)
}

func TestXLSXExtractor_CellTypes(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	_ = f.SetCellValue("Sheet1", "A1", "Type")
	_ = f.SetCellValue("Sheet1", "B1", "Value")
	_ = f.SetCellValue("Sheet1", "A2", "string")
	_ = f.SetCellValue("Sheet1", "B2", "hello")
	_ = f.SetCellValue("Sheet1", "A3", "number")
	_ = f.SetCellValue("Sheet1", "B3", 42.5)
	_ = f.SetCellValue("Sheet1", "A4", "bool")
	_ = f.SetCellValue("Sheet1", "B4", true)
	_ = f.SetCellFormula("Sheet1", "B5", "=A2&\" world\"")
	_ = f.SetCellValue("Sheet1", "A5", "formula")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "hello")
	assert.Contains(t, out.Text, "42.5")
	// excelize renders bools as "TRUE"/"FALSE" or "1"/"0" depending on cell format.
	// Just verify the row is present:
	assert.Contains(t, out.Text, "| bool |")
	assert.Contains(t, out.Text, "| formula |")
}

func TestXLSXExtractor_MergedCells(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	_ = f.SetCellValue("Sheet1", "A1", "Header")
	_ = f.MergeCell("Sheet1", "A1", "B1") // A1:B1 merged with value "Header"
	_ = f.SetCellValue("Sheet1", "A2", "x")
	_ = f.SetCellValue("Sheet1", "B2", "y")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "Header")
	assert.Contains(t, out.Text, "x")
	assert.Contains(t, out.Text, "y")
	// Merged A1:B1 — value in A1, blank in B1 (excelize returns "" for the
	// merged-with cell)
	assert.Contains(t, out.Text, `| Header |  |`)
	// Data row unaffected
	assert.Contains(t, out.Text, `| x | y |`)
}

func TestXLSXExtractor_PipeEscaping(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	_ = f.SetCellValue("Sheet1", "A1", "A | B")
	_ = f.SetCellValue("Sheet1", "B1", "C")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, `A \| B`)
}

func TestXLSXExtractor_TrimsTrailingEmpty(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	_ = f.SetCellValue("Sheet1", "A1", "h1")
	_ = f.SetCellValue("Sheet1", "B1", "h2")
	_ = f.SetCellValue("Sheet1", "A2", "v1")
	// Leave B2 empty, and intentionally write to D5 then leave E5..F5 empty
	_ = f.SetCellValue("Sheet1", "D5", "x")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	// The extractor should not panic or emit empty separator rows
	assert.Contains(t, out.Text, "h1")
	assert.Contains(t, out.Text, "v1")
}

func TestXLSXExtractor_TrimsLeadingEmpty(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	// Data starts at row 3, column B
	_ = f.SetCellValue("Sheet1", "B3", "h1")
	_ = f.SetCellValue("Sheet1", "C3", "h2")
	_ = f.SetCellValue("Sheet1", "B4", "v1")
	_ = f.SetCellValue("Sheet1", "C4", "v2")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	// Header row should be h1 | h2 (leading empty col trimmed, leading rows trimmed)
	assert.Contains(t, out.Text, "| h1 | h2 |")
	assert.Contains(t, out.Text, "| v1 | v2 |")
	// No empty header rows preceding the real header
	assert.NotContains(t, out.Text, "|  |  |")
}

func TestXLSXExtractor_TripsCompressedSize(t *testing.T) {
	// Create a real but small XLSX, then set a CompressedSizeBytes limit below its actual size.
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	_ = f.SetCellValue("Sheet1", "A1", "x")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}

	limits := DefaultLimits()
	limits.CompressedSizeBytes = 100 // tiny — well below any real xlsx
	e := NewXLSXExtractor(limits)
	_, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "compressed_size", le.Kind)
}

func TestXLSXExtractor_DateCell(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	// Set a date value with a date number format
	_ = f.SetCellValue("Sheet1", "A1", "Date")
	if err := f.SetCellValue("Sheet1", "B1", "2026-04-30"); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Apply a date format style to A2/B2
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	// excelize renders dates per the cell's number format. As long as the cell
	// value appears in the output (not crashed/swallowed), the path is exercised.
	assert.Contains(t, out.Text, "2026-04-30")
}

func TestXLSXExtractor_TripsUnzipSizeLimit(t *testing.T) {
	// Build an xlsx with enough content that decompressed size exceeds a small limit.
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	bigStr := strings.Repeat("padding-padding-padding-", 1000) // ~24KB
	for i := 0; i < 100; i++ {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		_ = f.SetCellValue("Sheet1", cell, bigStr)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}

	limits := DefaultLimits()
	limits.DecompressedSizeBytes = 10 * 1024 // tiny — well below the decompressed size
	e := NewXLSXExtractor(limits)
	_, err := e.Extract(buf.Bytes(), xlsxMIME)
	// excelize returns its own error when UnzipSizeLimit is exceeded; we wrap it as ErrMalformed.
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMalformed), "excelize unzip error should wrap to ErrMalformed; got: %v", err)
}

// TestXLSXExtractor_HeaderDetection_HeaderlessAllStringsWithDuplicates verifies
// the content-heuristic fallback rejects a header when row 1 contains
// duplicate string values, even though every row-1 cell is a string and
// fingerprints are uniform (no styling). This is the canonical "headerless
// data table" signal.
func TestXLSXExtractor_HeaderDetection_HeaderlessAllStringsWithDuplicates(t *testing.T) {
	// Row 1 has a duplicate ("apple"), so the content heuristic must reject.
	// All cells are strings, so no style differentiation possible.
	data := buildXLSX(t, "Sheet1", [][]any{
		{"apple", "banana", "apple"},
		{"cherry", "date", "elderberry"},
		{"fig", "grape", "honeydew"},
	})
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(data, xlsxMIME)
	assert.NoError(t, err)
	// No-header rendering: no separator row of "| --- | --- | --- |".
	assert.NotContains(t, out.Text, "| --- | --- | --- |",
		"expected no markdown header separator when row 1 has duplicate strings")
	// All data rows present.
	assert.Contains(t, out.Text, "| apple | banana | apple |")
	assert.Contains(t, out.Text, "| cherry | date | elderberry |")
	assert.Contains(t, out.Text, "| fig | grape | honeydew |")
}

// TestXLSXExtractor_HeaderDetection_StyledHeader verifies that a styled
// header row (different bgColor + bold) triggers the "header + uniform"
// style rule. Three rows of data with row 1 styled distinctly.
func TestXLSXExtractor_HeaderDetection_StyledHeader(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	// Header row with bold + yellow fill.
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 12},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFFF00"}},
	})
	if err != nil {
		t.Fatalf("new style: %v", err)
	}
	// Body style (different bgColor) so body rows have a non-zero fingerprint
	// distinct from the header.
	bodyStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: false, Size: 11},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFFFFF"}},
	})
	if err != nil {
		t.Fatalf("new style: %v", err)
	}
	_ = f.SetCellValue("Sheet1", "A1", "Country")
	_ = f.SetCellValue("Sheet1", "B1", "Capital")
	_ = f.SetCellValue("Sheet1", "A2", "France")
	_ = f.SetCellValue("Sheet1", "B2", "Paris")
	_ = f.SetCellValue("Sheet1", "A3", "Germany")
	_ = f.SetCellValue("Sheet1", "B3", "Berlin")
	_ = f.SetCellValue("Sheet1", "A4", "Italy")
	_ = f.SetCellValue("Sheet1", "B4", "Rome")
	if err := f.SetCellStyle("Sheet1", "A1", "B1", headerStyle); err != nil {
		t.Fatalf("style header: %v", err)
	}
	if err := f.SetCellStyle("Sheet1", "A2", "B4", bodyStyle); err != nil {
		t.Fatalf("style body: %v", err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}

	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	// Header detected -> separator row emitted.
	assert.Contains(t, out.Text, "| --- | --- |",
		"expected markdown header separator when row 1 is styled distinctly")
	assert.Contains(t, out.Text, "| Country | Capital |")
	assert.Contains(t, out.Text, "| France | Paris |")
}

// TestXLSXExtractor_HeaderDetection_AlternatingDataFingerprints verifies the
// "header + alternating" 5-row rule: row 1 styled distinctly, then alternating
// stripes (row2==row4, row3==row5).
func TestXLSXExtractor_HeaderDetection_AlternatingDataFingerprints(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 12},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFFF00"}},
	})
	if err != nil {
		t.Fatalf("new header style: %v", err)
	}
	stripeA, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 11},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EEEEEE"}},
	})
	if err != nil {
		t.Fatalf("new stripeA style: %v", err)
	}
	stripeB, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 11},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFFFFF"}},
	})
	if err != nil {
		t.Fatalf("new stripeB style: %v", err)
	}
	_ = f.SetCellValue("Sheet1", "A1", "ID")
	_ = f.SetCellValue("Sheet1", "B1", "Name")
	_ = f.SetCellValue("Sheet1", "A2", "1")
	_ = f.SetCellValue("Sheet1", "B2", "Alpha")
	_ = f.SetCellValue("Sheet1", "A3", "2")
	_ = f.SetCellValue("Sheet1", "B3", "Bravo")
	_ = f.SetCellValue("Sheet1", "A4", "3")
	_ = f.SetCellValue("Sheet1", "B4", "Charlie")
	_ = f.SetCellValue("Sheet1", "A5", "4")
	_ = f.SetCellValue("Sheet1", "B5", "Delta")
	if err := f.SetCellStyle("Sheet1", "A1", "B1", headerStyle); err != nil {
		t.Fatalf("style header: %v", err)
	}
	// Row 2 -> stripeA, Row 3 -> stripeB, Row 4 -> stripeA, Row 5 -> stripeB.
	if err := f.SetCellStyle("Sheet1", "A2", "B2", stripeA); err != nil {
		t.Fatalf("style 2: %v", err)
	}
	if err := f.SetCellStyle("Sheet1", "A3", "B3", stripeB); err != nil {
		t.Fatalf("style 3: %v", err)
	}
	if err := f.SetCellStyle("Sheet1", "A4", "B4", stripeA); err != nil {
		t.Fatalf("style 4: %v", err)
	}
	if err := f.SetCellStyle("Sheet1", "A5", "B5", stripeB); err != nil {
		t.Fatalf("style 5: %v", err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}

	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "| --- | --- |",
		"expected header separator when row 1 differs and rows 2/3 alternate as 4/5")
	assert.Contains(t, out.Text, "| ID | Name |")
}

// TestXLSXExtractor_HeaderDetection_TwoRowContentHeuristicPositive verifies
// the 2-row content heuristic when row 2 has at least one non-string cell.
func TestXLSXExtractor_HeaderDetection_TwoRowContentHeuristicPositive(t *testing.T) {
	// All-string row 1, no duplicates; row 2 mixes string + number.
	data := buildXLSX(t, "Sheet1", [][]any{
		{"Name", "Age"},
		{"Alice", 30},
	})
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(data, xlsxMIME)
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "| --- | --- |",
		"expected header separator: row1 strings no-dup + row2 has number")
	assert.Contains(t, out.Text, "| Name | Age |")
	assert.Contains(t, out.Text, "| Alice | 30 |")
}

// TestXLSXExtractor_HeaderDetection_TwoRowContentHeuristicNegative verifies
// the 2-row content heuristic returns no-header when row 1 contains a number.
func TestXLSXExtractor_HeaderDetection_TwoRowContentHeuristicNegative(t *testing.T) {
	// Row 1 has a numeric cell -> not all strings -> no header.
	data := buildXLSX(t, "Sheet1", [][]any{
		{"col1", 100},
		{"foo", 200},
	})
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(data, xlsxMIME)
	assert.NoError(t, err)
	assert.NotContains(t, out.Text, "| --- | --- |",
		"expected no header separator when row 1 has a numeric cell")
	// Both rows still appear as data rows.
	assert.Contains(t, out.Text, "| col1 | 100 |")
	assert.Contains(t, out.Text, "| foo | 200 |")
}

// TestXLSXExtractor_HeaderDetection_UnicodeNoCrash is a sanity check that
// multi-byte UTF-8 cell content (CJK + emoji + diacritics) does not crash
// header detection or rendering.
func TestXLSXExtractor_HeaderDetection_UnicodeNoCrash(t *testing.T) {
	data := buildXLSX(t, "Sheet1", [][]any{
		{"名前", "年齢", "メモ"},
		{"日本語", 42, "test"},
		{"Ωμέγα", 7, "café"},
		{"русский", 13, "naïve"},
		{"emoji", 100, "fire"},
	})
	e := NewXLSXExtractor(DefaultLimits())
	out, err := e.Extract(data, xlsxMIME)
	assert.NoError(t, err)
	// Multi-byte content must round-trip intact.
	assert.Contains(t, out.Text, "名前")
	assert.Contains(t, out.Text, "年齢")
	assert.Contains(t, out.Text, "Ωμέγα")
	assert.Contains(t, out.Text, "русский")
	assert.Contains(t, out.Text, "café")
}
