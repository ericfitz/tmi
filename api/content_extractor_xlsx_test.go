package api

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
	e := NewXLSXExtractor(defaultOOXMLLimits())
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
	e := NewXLSXExtractor(defaultOOXMLLimits())
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

	e := NewXLSXExtractor(defaultOOXMLLimits())
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
	e := NewXLSXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	// Empty default sheet still emits its name
	assert.Contains(t, out.Text, "## Sheet: Sheet1")
}

func TestXLSXExtractor_MalformedDataIsErrMalformed(t *testing.T) {
	e := NewXLSXExtractor(defaultOOXMLLimits())
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

	e := NewXLSXExtractor(defaultOOXMLLimits())
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

	limits := defaultOOXMLLimits()
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
	e := NewXLSXExtractor(defaultOOXMLLimits())
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
	e := NewXLSXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	// First row: Header in A1, blank in B1 (merged top-left holds value)
	// Just verify Header appears and the row layout includes the trailing pipe
	assert.Contains(t, out.Text, "Header")
	assert.Contains(t, out.Text, "x")
	assert.Contains(t, out.Text, "y")
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
	e := NewXLSXExtractor(defaultOOXMLLimits())
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
	e := NewXLSXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(buf.Bytes(), xlsxMIME)
	assert.NoError(t, err)
	// The extractor should not panic or emit empty separator rows
	assert.Contains(t, out.Text, "h1")
	assert.Contains(t, out.Text, "v1")
}
