package extract

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// xlsxRowFingerprint captures the dominant style attributes of a row, used by
// the header-detection algorithm. A zero-value fingerprint represents "no
// styling signal" (e.g., row contained no styled cells).
type xlsxRowFingerprint struct {
	bgColor    string
	fontSize   float64
	fontBold   bool
	fontItalic bool
	// hasStyle is true if at least one cell in the row contributed a non-empty
	// (bgColor, fontSize) signal. Rows whose cells all have zero styles are
	// treated as "uniform unstyled" rather than as a meaningful fingerprint.
	hasStyle bool
}

// xlsxHeaderDecision is the output of header detection over the first ≤5
// non-empty rows of a sheet.
type xlsxHeaderDecision struct {
	hasHeader bool
}

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// XLSXExtractor extracts Markdown-flavored text from an XLSX (OOXML) workbook.
// Uses github.com/xuri/excelize/v2 for archive parsing, shared-strings handling,
// and number-format rendering. Limit enforcement (cell count, markdown cap) is
// applied during the streaming render.
//
// Header detection follows the design spec: a per-row style fingerprint
// (dominant bgColor, fontSize, fontWeight, fontItalic) is computed across the
// first ≤5 non-empty rows, with a content-heuristic fallback when style
// fingerprints are uniform across the inspected rows. See xlsxDetectHeader.
type XLSXExtractor struct{ limits Limits }

// NewXLSXExtractor returns an extractor configured with the given limits.
func NewXLSXExtractor(limits Limits) *XLSXExtractor { return &XLSXExtractor{limits: limits} }

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
func xlsxRenderSheet(ctx context.Context, f *excelize.File, sheet string, mb *markdownBuilder, limits Limits, cellsTotal *int) error {
	rows, err := f.Rows(sheet)
	if err != nil {
		return fmt.Errorf("%w: rows %s: %w", ErrMalformed, sheet, err)
	}
	defer func() { _ = rows.Close() }()

	// Buffer rows into [][]string, then trim and render. We track each row's
	// physical 1-based row number so the header-detection pass can query
	// per-cell styles via cell coordinates (excelize's streaming Rows iterator
	// emits one entry per physical row from row 1 to the last populated row,
	// so position i in the slice corresponds to physical row i+1).
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

	// Header detection runs over the pre-trim data (physical row coordinates)
	// so per-cell style queries map directly to cell names.
	decision := xlsxDetectHeader(f, sheet, data)

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

	if _, err := mb.WriteString("\n\n"); err != nil {
		return err
	}
	if decision.hasHeader {
		// Render with first row as markdown table header.
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
	} else {
		// No header detected: emit rows as-is without a header/separator. The
		// output is still useful pipe-delimited content for downstream
		// consumers (search, embedding); strict markdown table rendering is
		// not required when there is no semantic header.
		for _, row := range data {
			if err := xlsxWriteRow(mb, row); err != nil {
				return err
			}
		}
	}
	return nil
}

// xlsxDetectHeader applies the style-fingerprint header detection rules from
// the design spec (docs/superpowers/specs/2026-04-29-ooxml-extractors-design.md,
// XLSX section) over the first ≤5 non-empty rows of the sheet.
//
// Algorithm:
//   - Compute a per-row dominant fingerprint (bgColor, fontSize, fontBold,
//     fontItalic) by mode across the row's non-empty cells.
//   - Apply the rule cascade based on how many non-empty rows are available
//     (capped at 5):
//   - 0–1 rows: no header.
//   - 2 rows: insufficient style signal — content heuristic only.
//   - 3 rows: "header + uniform" only.
//   - 4 rows: "header + uniform" plus "alternating, no header".
//   - 5+ rows: full ruleset (header+uniform, header+alternating,
//     alternating-no-header).
//   - When fingerprints are uniform across all examined rows (no styling
//     signal), fall back to the content heuristic: header iff all row-1 cells
//     are string-typed AND no duplicates AND row 2 has at least one
//     non-string cell.
//
// xlsxRowRef pairs a physical 1-based row number with the row's cell values.
type xlsxRowRef struct {
	physRow int
	cells   []string
}

// xlsxCollectInspectRows returns up to the first 5 non-empty rows from data,
// pairing each with its physical (1-based) row index for cell-style queries.
func xlsxCollectInspectRows(data [][]string) []xlsxRowRef {
	var refs []xlsxRowRef
	for i, r := range data {
		if xlsxRowEmpty(r) {
			continue
		}
		refs = append(refs, xlsxRowRef{physRow: i + 1, cells: r})
		if len(refs) == 5 {
			break
		}
	}
	return refs
}

// xlsxApplyStyleRules returns the header decision (and whether any rule fired)
// based on per-row style fingerprints. fps must have len == n. n is in [3, 5+].
// The 2-row case is handled by the caller via the content heuristic only.
func xlsxApplyStyleRules(fps []xlsxRowFingerprint) (xlsxHeaderDecision, bool) {
	n := len(fps)
	switch {
	case n == 3:
		// Header + uniform: row1 != row2, row2 == row3 -> header.
		if !xlsxFingerprintEqual(fps[0], fps[1]) && xlsxFingerprintEqual(fps[1], fps[2]) {
			return xlsxHeaderDecision{hasHeader: true}, true
		}
		return xlsxHeaderDecision{hasHeader: false}, true
	case n == 4:
		if !xlsxFingerprintEqual(fps[0], fps[1]) &&
			xlsxFingerprintEqual(fps[1], fps[2]) &&
			xlsxFingerprintEqual(fps[2], fps[3]) {
			return xlsxHeaderDecision{hasHeader: true}, true
		}
		if xlsxFingerprintEqual(fps[0], fps[2]) &&
			xlsxFingerprintEqual(fps[1], fps[3]) &&
			!xlsxFingerprintEqual(fps[0], fps[1]) {
			return xlsxHeaderDecision{hasHeader: false}, true
		}
		return xlsxHeaderDecision{hasHeader: false}, true
	case n >= 5:
		if !xlsxFingerprintEqual(fps[0], fps[1]) &&
			xlsxFingerprintEqual(fps[1], fps[2]) &&
			xlsxFingerprintEqual(fps[2], fps[3]) {
			return xlsxHeaderDecision{hasHeader: true}, true
		}
		if !xlsxFingerprintEqual(fps[0], fps[1]) &&
			!xlsxFingerprintEqual(fps[0], fps[2]) &&
			xlsxFingerprintEqual(fps[1], fps[3]) &&
			xlsxFingerprintEqual(fps[2], fps[4]) {
			return xlsxHeaderDecision{hasHeader: true}, true
		}
		if xlsxFingerprintEqual(fps[0], fps[2]) &&
			xlsxFingerprintEqual(fps[2], fps[4]) &&
			xlsxFingerprintEqual(fps[1], fps[3]) &&
			!xlsxFingerprintEqual(fps[0], fps[1]) {
			return xlsxHeaderDecision{hasHeader: false}, true
		}
		return xlsxHeaderDecision{hasHeader: false}, true
	}
	return xlsxHeaderDecision{hasHeader: false}, false
}

// xlsxContentHeuristicHeader returns true iff the row pair (row1, row2)
// matches the spec's content heuristic: all row-1 non-empty cells are
// string-typed AND no duplicates AND row 2 has at least one non-string cell.
func xlsxContentHeuristicHeader(f *excelize.File, sheet string, row1, row2 xlsxRowRef) bool {
	row1Strings := 0
	row1Cells := 0
	seen := map[string]bool{}
	dup := false
	for c, v := range row1.cells {
		if strings.TrimSpace(v) == "" {
			continue
		}
		row1Cells++
		cellName, err := excelize.CoordinatesToCellName(c+1, row1.physRow)
		if err != nil {
			return false
		}
		t, _ := f.GetCellType(sheet, cellName)
		if xlsxCellTypeIsString(t) {
			row1Strings++
		}
		key := strings.TrimSpace(v)
		if seen[key] {
			dup = true
		}
		seen[key] = true
	}
	if row1Cells == 0 || row1Strings != row1Cells || dup {
		return false
	}
	for c, v := range row2.cells {
		if strings.TrimSpace(v) == "" {
			continue
		}
		cellName, err := excelize.CoordinatesToCellName(c+1, row2.physRow)
		if err != nil {
			continue
		}
		t, _ := f.GetCellType(sheet, cellName)
		if !xlsxCellTypeIsString(t) {
			return true
		}
	}
	return false
}

// data is the pre-trim row slice; index i corresponds to physical row i+1.
func xlsxDetectHeader(f *excelize.File, sheet string, data [][]string) xlsxHeaderDecision {
	refs := xlsxCollectInspectRows(data)
	if len(refs) <= 1 {
		return xlsxHeaderDecision{hasHeader: false}
	}

	fps := make([]xlsxRowFingerprint, len(refs))
	for i, ref := range refs {
		fps[i] = xlsxRowDominantFingerprint(f, sheet, ref.physRow, ref.cells)
	}

	uniform := true
	for i := 1; i < len(fps); i++ {
		if !xlsxFingerprintEqual(fps[0], fps[i]) {
			uniform = false
			break
		}
	}

	if !uniform && len(refs) >= 3 {
		if decision, fired := xlsxApplyStyleRules(fps); fired {
			return decision
		}
	}

	return xlsxHeaderDecision{hasHeader: xlsxContentHeuristicHeader(f, sheet, refs[0], refs[1])}
}

// xlsxRowDominantFingerprint computes the mode (most-common) fingerprint for
// a row across its non-empty cells. Empty cells are skipped. If no cell
// contributes a styled fingerprint, the result is the zero value with
// hasStyle=false (treated as "uniform unstyled" by xlsxFingerprintEqual).
func xlsxRowDominantFingerprint(f *excelize.File, sheet string, physRow int, cells []string) xlsxRowFingerprint {
	type fpKey struct {
		bg     string
		size   float64
		bold   bool
		italic bool
	}
	counts := map[fpKey]int{}
	var keys []fpKey // preserve first-seen order to break ties deterministically
	anyStyled := false
	for c, v := range cells {
		if strings.TrimSpace(v) == "" {
			continue
		}
		cellName, err := excelize.CoordinatesToCellName(c+1, physRow)
		if err != nil {
			continue
		}
		styleID, err := f.GetCellStyle(sheet, cellName)
		if err != nil || styleID == 0 {
			// Default style — treat as zero fingerprint contribution.
			continue
		}
		st, err := f.GetStyle(styleID)
		if err != nil || st == nil {
			continue
		}
		k := fpKey{}
		if len(st.Fill.Color) > 0 {
			k.bg = strings.ToLower(st.Fill.Color[0])
		}
		if st.Font != nil {
			k.size = st.Font.Size
			k.bold = st.Font.Bold
			k.italic = st.Font.Italic
		}
		if k == (fpKey{}) {
			// Style index referenced but yielded no signal.
			continue
		}
		anyStyled = true
		if _, ok := counts[k]; !ok {
			keys = append(keys, k)
		}
		counts[k]++
	}
	if !anyStyled {
		return xlsxRowFingerprint{}
	}
	// Mode: highest count, ties broken by first-seen order.
	var best fpKey
	bestCount := -1
	for _, k := range keys {
		if counts[k] > bestCount {
			best = k
			bestCount = counts[k]
		}
	}
	return xlsxRowFingerprint{
		bgColor:    best.bg,
		fontSize:   best.size,
		fontBold:   best.bold,
		fontItalic: best.italic,
		hasStyle:   true,
	}
}

// xlsxFingerprintEqual reports whether two row fingerprints are equal. Two
// "no signal" fingerprints (hasStyle==false on both sides) are equal — uniform
// unstyled rows compare equal so the rule cascade falls through to the
// content heuristic instead of accidentally matching a style rule.
func xlsxFingerprintEqual(a, b xlsxRowFingerprint) bool {
	return a == b
}

// xlsxCellTypeIsString reports whether the cell type is a string type per the
// content heuristic. Shared strings, inline strings, and the formula "str"
// type (which excelize maps to CellTypeFormula) are treated as strings.
// Numbers, booleans, dates, and errors are not.
func xlsxCellTypeIsString(t excelize.CellType) bool {
	switch t {
	case excelize.CellTypeSharedString, excelize.CellTypeInlineString:
		return true
	default:
		return false
	}
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
