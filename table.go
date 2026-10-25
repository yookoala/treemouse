package main

import (
	"fmt"
	"strings"
)

// ColumnAlignment represents GFM table column alignment.
type ColumnAlignment int

const (
	AlignLeft ColumnAlignment = iota
	AlignCenter
	AlignRight
)

// TableItem holds a parsed GFM Markdown table and its rendered line span in the document.
type TableItem struct {
	Index           int
	Headers         []string
	Alignments      []ColumnAlignment
	Rows            [][]string
	RawLines        []string
	StartRenderLine int
	EndRenderLine   int // Inclusive
	IsCropped       bool
}

// tableColSpan records the horizontal screen column range [StartX, EndX) for a visible column.
type tableColSpan struct {
	Col    int
	StartX int
	EndX   int
}

// tableRowSpan records the vertical screen row range [StartY, EndY] (inclusive) for a visible data row.
type tableRowSpan struct {
	Row    int
	StartY int
	EndY   int
}

// TableViewerState manages the full-screen interactive table viewer mode.
type TableViewerState struct {
	Table           TableItem
	CursorRow       int // 0-based index into Table.Rows
	CursorCol       int // 0-based index into Table.Headers
	ScrollRow       int // First visible data row index
	ColOffset       int // First visible column index (beyond pinned col 0 when applicable)
	ShowSyntax      bool
	DocumentTitle   string
	ShowCellPopup   bool // True when the full-content Cell Detail popup is open
	CellPopupScroll int  // Vertical scroll offset inside the Cell Detail popup
	LastColSpans    []tableColSpan
	LastRowSpans    []tableRowSpan
}

// NewTableViewerState creates an interactive viewer state for the given table.
func NewTableViewerState(tbl TableItem, docTitle string, showSyntax bool) *TableViewerState {
	return &TableViewerState{
		Table:         tbl,
		CursorRow:     0,
		CursorCol:     0,
		ScrollRow:     0,
		ColOffset:     0,
		ShowSyntax:    showSyntax,
		DocumentTitle: docTitle,
	}
}

// isTableDelimiterRow checks whether a line is a valid GFM table delimiter row (e.g. `| :--- | :---: | ---: |`).
func isTableDelimiterRow(line string) ([]ColumnAlignment, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.Contains(trimmed, "-") || !strings.Contains(trimmed, "|") {
		return nil, false
	}
	cells := splitTableCells(trimmed)
	if len(cells) == 0 {
		return nil, false
	}
	aligns := make([]ColumnAlignment, len(cells))
	for i, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return nil, false
		}
		leftColon := strings.HasPrefix(c, ":")
		rightColon := strings.HasSuffix(c, ":")
		core := strings.TrimPrefix(strings.TrimSuffix(c, ":"), ":")
		if len(core) == 0 {
			return nil, false
		}
		for _, ch := range core {
			if ch != '-' {
				return nil, false
			}
		}
		switch {
		case leftColon && rightColon:
			aligns[i] = AlignCenter
		case rightColon:
			aligns[i] = AlignRight
		default:
			aligns[i] = AlignLeft
		}
	}
	return aligns, true
}

// isTableRowCandidate checks if a line looks like a pipe-delimited table row.
func isTableRowCandidate(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "|")
}

// splitTableCells splits a GFM table row by unescaped '|' characters.
func splitTableCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "|") {
		trimmed = trimmed[1:]
	}
	if strings.HasSuffix(trimmed, "|") && !strings.HasSuffix(trimmed, `\|`) {
		trimmed = trimmed[:len(trimmed)-1]
	}

	var cells []string
	var cur strings.Builder
	inCode := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if ch == '\\' && i+1 < len(trimmed) && (trimmed[i+1] == '|' || trimmed[i+1] == '\\') {
			cur.WriteByte(trimmed[i+1])
			i++
			continue
		}
		if ch == '`' {
			inCode = !inCode
			cur.WriteByte(ch)
			continue
		}
		if ch == '|' && !inCode {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

// parseTableBlock attempts to parse a GFM table starting at rawLines[startIdx].
// Returns the parsed TableItem, the number of raw source lines consumed, and true if valid.
func parseTableBlock(rawLines []string, startIdx int, displaySyntax bool) (TableItem, int, bool) {
	if startIdx+1 >= len(rawLines) {
		return TableItem{}, 0, false
	}
	headerLine := rawLines[startIdx]
	delimLine := rawLines[startIdx+1]
	if !displaySyntax {
		var skip bool
		headerLine, _, skip = stripHTMLCommentsFromLine(headerLine, false)
		if skip {
			return TableItem{}, 0, false
		}
		delimLine, _, skip = stripHTMLCommentsFromLine(delimLine, false)
		if skip {
			return TableItem{}, 0, false
		}
	}

	if !isTableRowCandidate(headerLine) {
		return TableItem{}, 0, false
	}
	aligns, ok := isTableDelimiterRow(delimLine)
	if !ok {
		return TableItem{}, 0, false
	}
	headers := splitTableCells(headerLine)
	if len(headers) == 0 || len(headers) != len(aligns) {
		return TableItem{}, 0, false
	}

	numCols := len(headers)
	var rows [][]string
	rawConsumed := []string{rawLines[startIdx], rawLines[startIdx+1]}
	idx := startIdx + 2

	for idx < len(rawLines) {
		line := rawLines[idx]
		if reFencedBlock.MatchString(line) || reATXHeader.MatchString(line) {
			break
		}
		cleanLine := line
		if !displaySyntax {
			var skip bool
			cleanLine, _, skip = stripHTMLCommentsFromLine(cleanLine, false)
			if skip {
				rawConsumed = append(rawConsumed, line)
				idx++
				continue
			}
		}
		if strings.TrimSpace(cleanLine) == "" || !isTableRowCandidate(cleanLine) {
			break
		}
		cells := splitTableCells(cleanLine)
		normRow := make([]string, numCols)
		for c := 0; c < numCols; c++ {
			if c < len(cells) {
				normRow[c] = cells[c]
			}
		}
		rows = append(rows, normRow)
		rawConsumed = append(rawConsumed, line)
		idx++
	}

	return TableItem{
		Headers:    headers,
		Alignments: aligns,
		Rows:       rows,
		RawLines:   rawConsumed,
	}, idx - startIdx, true
}

// computeTableColumnWidths computes column widths for rendering a table within availWidth.
// When allowCrop is true (inline reader Option C), each column receives at least minColW columns
// so multi-line cell text remains readable; if the total table width exceeds availWidth,
// isCropped is returned as true so the caller can horizontally crop each rendered line.
func computeTableColumnWidths(tbl TableItem, availWidth int, minColW int, showSyntax bool) ([]int, int, bool) {
	numCols := len(tbl.Headers)
	if numCols == 0 {
		return nil, 0, false
	}

	naturalW := make([]int, numCols)
	for c, h := range tbl.Headers {
		w := cellDisplayWidth(h, showSyntax)
		if w < 3 {
			w = 3
		}
		naturalW[c] = w
	}
	for _, row := range tbl.Rows {
		for c := 0; c < numCols && c < len(row); c++ {
			w := cellDisplayWidth(row[c], showSyntax)
			if w > naturalW[c] {
				naturalW[c] = w
			}
		}
	}

	// Separator overhead: ` col0 │ col1 │ col2 ` -> 1 leading space + 1 trailing space + 3*(numCols-1) between columns
	sepOverhead := 2 + 3*(numCols-1)
	sumNatural := 0
	for _, w := range naturalW {
		sumNatural += w
	}

	colWidths := make([]int, numCols)
	if sumNatural+sepOverhead <= availWidth {
		copy(colWidths, naturalW)
		return colWidths, sumNatural + sepOverhead, false
	}

	// Need to shrink columns to fit availWidth while respecting minColW per column
	budgetForCols := availWidth - sepOverhead
	for c := 0; c < numCols; c++ {
		minForCol := minColW
		if naturalW[c] < minForCol {
			minForCol = naturalW[c]
		}
		colWidths[c] = minForCol
	}

	minSum := 0
	for _, w := range colWidths {
		minSum += w
	}

	if minSum < budgetForCols {
		// Distribute remaining extra width proportionally to columns that want more than minColW
		extra := budgetForCols - minSum
		needSum := 0
		for c := 0; c < numCols; c++ {
			if naturalW[c] > colWidths[c] {
				needSum += naturalW[c] - colWidths[c]
			}
		}
		if needSum > 0 {
			assigned := 0
			for c := 0; c < numCols; c++ {
				need := naturalW[c] - colWidths[c]
				if need > 0 {
					share := extra * need / needSum
					if share > need {
						share = need
					}
					colWidths[c] += share
					assigned += share
				}
			}
			rem := extra - assigned
			for c := 0; c < numCols && rem > 0; c++ {
				if colWidths[c] < naturalW[c] {
					colWidths[c]++
					rem--
				}
			}
		}
	}

	totalW := sepOverhead
	for _, w := range colWidths {
		totalW += w
	}
	isCropped := totalW > availWidth
	return colWidths, totalW, isCropped
}

func cellDisplayWidth(cell string, showSyntax bool) int {
	tokens := parseInlineTokens(cell, bgMain, fgDefault, showSyntax)
	w := 0
	for _, t := range tokens {
		w += visibleWidth(t.text)
	}
	return w
}

// alignCellLine pads a single wrapped cell line to colW according to alignment.
func alignCellLine(styledText string, colW int, align ColumnAlignment, rowBg string, baseFg string) string {
	vw := visibleWidth(styledText)
	if vw >= colW {
		return sliceANSI(styledText, 0, colW, rowBg)
	}
	pad := colW - vw
	switch align {
	case AlignRight:
		return rowBg + strings.Repeat(" ", pad) + styledText + ansiReset + rowBg + baseFg
	case AlignCenter:
		leftPad := pad / 2
		rightPad := pad - leftPad
		return rowBg + strings.Repeat(" ", leftPad) + styledText + ansiReset + rowBg + strings.Repeat(" ", rightPad) + baseFg
	default:
		return styledText + ansiReset + rowBg + strings.Repeat(" ", pad) + baseFg
	}
}

// renderInlineTable renders a TableItem for the inline Markdown Reader (Option C: multi-line wrapped cells,
// cropped horizontally with a '▸' indicator if the table's minimum readable width exceeds contentWidth).
func renderInlineTable(tbl *TableItem, totalWidth int, contentWidth int, showSyntax bool) []string {
	if showSyntax {
		var out []string
		for _, raw := range tbl.RawLines {
			expanded := strings.ReplaceAll(raw, "\t", "    ")
			tokens := parseInlineTokens(expanded, bgMain, fgDefault, true)
			wrapped := wrapStyledTokens(tokens, contentWidth, "", "", bgMain, fgDefault)
			for _, wl := range wrapped {
				out = append(out, styleLine(" "+wl, totalWidth, bgMain, fgDefault))
			}
		}
		tbl.IsCropped = false
		return out
	}

	minColW := 10
	colWidths, _, isCropped := computeTableColumnWidths(*tbl, contentWidth, minColW, false)
	tbl.IsCropped = isCropped
	numCols := len(tbl.Headers)
	if numCols == 0 {
		return nil
	}

	var outLines []string

	cropLineToWidth := func(fullLine string, lineBg string, defaultFg string) string {
		if !isCropped {
			return styleLine(" "+fullLine, totalWidth, lineBg, defaultFg)
		}
		// Crop to contentWidth - 1 and append a right-edge crop marker '▸'
		cropW := contentWidth - 1
		if cropW < 1 {
			cropW = 1
		}
		sliced := sliceANSI(fullLine, 0, cropW, lineBg)
		lineContent := " " + sliced + ansiReset + lineBg + fgFilterBadge + "▸"
		return styleLine(lineContent, totalWidth, lineBg, defaultFg)
	}

	// 1. Render Header Row (may wrap across multiple lines)
	headerWrapped := make([][]string, numCols)
	maxHeaderLines := 1
	for c := 0; c < numCols; c++ {
		tokens := parseInlineTokens(tbl.Headers[c], bgTableHeader, fgH1, false)
		lines := wrapStyledTokens(tokens, colWidths[c], "", "", bgTableHeader, fgH1)
		headerWrapped[c] = lines
		if len(lines) > maxHeaderLines {
			maxHeaderLines = len(lines)
		}
	}
	for l := 0; l < maxHeaderLines; l++ {
		var rowBuilder strings.Builder
		rowBuilder.WriteString(bgTableHeader + " ")
		for c := 0; c < numCols; c++ {
			cellStr := ""
			if l < len(headerWrapped[c]) {
				cellStr = headerWrapped[c][l]
			}
			rowBuilder.WriteString(alignCellLine(cellStr, colWidths[c], tbl.Alignments[c], bgTableHeader, fgH1))
			if c < numCols-1 {
				rowBuilder.WriteString(ansiReset + bgTableHeader + fgQuoteBar + " │ " + fgH1)
			}
		}
		rowBuilder.WriteString(" ")
		outLines = append(outLines, cropLineToWidth(rowBuilder.String(), bgTableHeader, fgH1))
	}

	// 2. Render Header Separator Line (`───┼───`)
	var sepBuilder strings.Builder
	sepBuilder.WriteString(fgQuoteBar + "─")
	for c := 0; c < numCols; c++ {
		sepBuilder.WriteString(strings.Repeat("─", colWidths[c]))
		if c < numCols-1 {
			sepBuilder.WriteString("─┼─")
		}
	}
	sepBuilder.WriteString("─")
	outLines = append(outLines, cropLineToWidth(sepBuilder.String(), bgMain, fgQuoteBar))

	// 3. Render Body Rows (each row can wrap inside cells; if any row wraps to >1 line, draw subtle row dividers)
	anyMultiLine := false
	bodyWrapped := make([][][]string, len(tbl.Rows))
	rowHeights := make([]int, len(tbl.Rows))

	for rIdx, row := range tbl.Rows {
		bodyWrapped[rIdx] = make([][]string, numCols)
		maxLines := 1
		for c := 0; c < numCols; c++ {
			cellText := ""
			if c < len(row) {
				cellText = row[c]
			}
			tokens := parseInlineTokens(cellText, bgMain, fgDefault, false)
			lines := wrapStyledTokens(tokens, colWidths[c], "", "", bgMain, fgDefault)
			bodyWrapped[rIdx][c] = lines
			if len(lines) > maxLines {
				maxLines = len(lines)
			}
		}
		rowHeights[rIdx] = maxLines
		if maxLines > 1 {
			anyMultiLine = true
		}
	}

	for rIdx := range tbl.Rows {
		h := rowHeights[rIdx]
		for l := 0; l < h; l++ {
			var rowBuilder strings.Builder
			rowBuilder.WriteString(bgMain + " ")
			for c := 0; c < numCols; c++ {
				cellStr := ""
				if l < len(bodyWrapped[rIdx][c]) {
					cellStr = bodyWrapped[rIdx][c][l]
				}
				rowBuilder.WriteString(alignCellLine(cellStr, colWidths[c], tbl.Alignments[c], bgMain, fgDefault))
				if c < numCols-1 {
					rowBuilder.WriteString(ansiReset + bgMain + fgQuoteBar + " │ " + fgDefault)
				}
			}
			rowBuilder.WriteString(" ")
			outLines = append(outLines, cropLineToWidth(rowBuilder.String(), bgMain, fgDefault))
		}
		if anyMultiLine && rIdx < len(tbl.Rows)-1 {
			outLines = append(outLines, cropLineToWidth(sepBuilder.String(), bgMain, fgQuoteBar))
		}
	}

	return outLines
}

// ActiveCellText returns the raw Markdown text of the currently highlighted cell in the Table Viewer.
func (tv *TableViewerState) ActiveCellText() string {
	if len(tv.Table.Rows) == 0 {
		if tv.CursorCol >= 0 && tv.CursorCol < len(tv.Table.Headers) {
			return tv.Table.Headers[tv.CursorCol]
		}
		return ""
	}
	if tv.CursorRow >= 0 && tv.CursorRow < len(tv.Table.Rows) {
		row := tv.Table.Rows[tv.CursorRow]
		if tv.CursorCol >= 0 && tv.CursorCol < len(row) {
			return row[tv.CursorCol]
		}
	}
	return ""
}

// ActiveCellLink extracts the first Markdown link destination in the currently selected cell, if any.
func (tv *TableViewerState) ActiveCellLink() (string, string, bool) {
	cell := tv.ActiveCellText()
	return extractFirstLink(cell)
}

// extractFirstLink finds the first Markdown link `[label](url)` in s and returns (label, url, true).
func extractFirstLink(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '[' || (s[i] == '!' && i+1 < len(s) && s[i+1] == '[') {
			if l := matchMarkdownLink(s[i:]); l > 0 {
				label, dest := parseMarkdownLinkParts(s[i : i+l])
				if dest != "" {
					return label, dest, true
				}
			}
		}
	}
	return "", "", false
}

// MoveRow moves the active row cursor up/down in the full-screen Table Viewer.
func (tv *TableViewerState) MoveRow(delta int) {
	if len(tv.Table.Rows) == 0 {
		tv.CursorRow = 0
		return
	}
	tv.CursorRow += delta
	if tv.CursorRow < 0 {
		tv.CursorRow = 0
	}
	if tv.CursorRow >= len(tv.Table.Rows) {
		tv.CursorRow = len(tv.Table.Rows) - 1
	}
}

// MoveCol moves the active column cursor left/right in the full-screen Table Viewer.
func (tv *TableViewerState) MoveCol(delta int) {
	numCols := len(tv.Table.Headers)
	if numCols == 0 {
		tv.CursorCol = 0
		return
	}
	tv.CursorCol += delta
	if tv.CursorCol < 0 {
		tv.CursorCol = 0
	}
	if tv.CursorCol >= numCols {
		tv.CursorCol = numCols - 1
	}
	if tv.CursorCol < tv.ColOffset {
		tv.ColOffset = tv.CursorCol
	}
}

// RenderLines renders the full-screen interactive Table Viewer.
func (tv *TableViewerState) RenderLines(width, height int) []string {
	if width < 10 {
		width = 10
	}
	if height < 5 {
		height = 5
	}

	numCols := len(tv.Table.Headers)
	numRows := len(tv.Table.Rows)
	contentW := width - 2
	if contentW < 6 {
		contentW = width
	}

	// Compute comfortable column widths for the interactive viewer (minimum 12 cols per column)
	colWidths, _, _ := computeTableColumnWidths(tv.Table, contentW, 12, tv.ShowSyntax)
	for c := range colWidths {
		if colWidths[c] > contentW-2 {
			colWidths[c] = contentW - 2
		}
		if colWidths[c] < 6 {
			colWidths[c] = 6
		}
	}

	// Ensure CursorCol is within visible columns [ColOffset .. lastVisibleCol]
	if tv.CursorCol < tv.ColOffset {
		tv.ColOffset = tv.CursorCol
	}
	for tv.ColOffset < tv.CursorCol {
		used := 2
		fits := true
		for c := tv.ColOffset; c <= tv.CursorCol; c++ {
			if c > tv.ColOffset {
				used += 3
			}
			used += colWidths[c]
			if used > contentW && c > tv.ColOffset {
				fits = false
				break
			}
		}
		if fits {
			break
		}
		tv.ColOffset++
	}

	// Determine visible column range [tv.ColOffset .. endCol]
	endCol := tv.ColOffset
	usedW := 2 + colWidths[tv.ColOffset]
	for c := tv.ColOffset + 1; c < numCols; c++ {
		if usedW+3+colWidths[c] <= contentW {
			usedW += 3 + colWidths[c]
			endCol = c
		} else {
			break
		}
	}

	// Record horizontal screen column spans [StartX, EndX) for mouse hit-testing
	tv.LastColSpans = nil
	curX := 2
	for c := tv.ColOffset; c <= endCol; c++ {
		tv.LastColSpans = append(tv.LastColSpans, tableColSpan{
			Col:    c,
			StartX: curX,
			EndX:   curX + colWidths[c],
		})
		curX += colWidths[c] + 3
	}
	tv.LastRowSpans = nil

	// Top Bar
	var lines []string
	colInfo := fmt.Sprintf("[Row %d/%d  Col %d/%d] ", tv.CursorRow+1, maxInt(1, numRows), tv.CursorCol+1, maxInt(1, numCols))
	leftIndicator := ""
	if tv.ColOffset > 0 {
		leftIndicator = "◀ "
	}
	rightIndicator := ""
	if endCol < numCols-1 {
		rightIndicator = " ▶"
	}
	headerBarTitle := fmt.Sprintf(" ⊞ Table Viewer — %s %s%s", tv.DocumentTitle, leftIndicator, rightIndicator)
	lines = append(lines, styleBarWithRight(headerBarTitle, colInfo, width, bgBar, fgActiveCursor, fgFilterBadge))

	// Reserve bottom 2 body rows for the Active Cell Inspector strip (helpful on mobile/small screens)
	bodyHeight := height - 2
	inspectorHeight := 2
	if bodyHeight < 5 {
		inspectorHeight = 1
	}
	gridAvailHeight := bodyHeight - inspectorHeight

	// Render Pinned Table Header + Separator
	headerCellLines := make([][]string, endCol-tv.ColOffset+1)
	maxH := 1
	for idx, c := 0, tv.ColOffset; c <= endCol; idx, c = idx+1, c+1 {
		hdrFg := fgH1
		hdrBg := bgTableHeader
		if c == tv.CursorCol {
			hdrBg = bgSelectFocus
			hdrFg = fgBright + ansiBold
		}
		tokens := parseInlineTokens(tv.Table.Headers[c], hdrBg, hdrFg, tv.ShowSyntax)
		wrapped := wrapStyledTokens(tokens, colWidths[c], "", "", hdrBg, hdrFg)
		headerCellLines[idx] = wrapped
		if len(wrapped) > maxH {
			maxH = len(wrapped)
		}
	}
	if maxH > 2 {
		maxH = 2
	}

	for l := 0; l < maxH && len(lines)-1 < gridAvailHeight; l++ {
		var rb strings.Builder
		rb.WriteString(bgTableHeader + " ")
		if tv.ColOffset > 0 {
			rb.WriteString(fgFilterBadge + "◀")
		} else {
			rb.WriteString(" ")
		}
		for idx, c := 0, tv.ColOffset; c <= endCol; idx, c = idx+1, c+1 {
			cellBg := bgTableHeader
			cellFg := fgH1
			if c == tv.CursorCol {
				cellBg = bgSelectFocus
				cellFg = fgBright + ansiBold
			}
			cellStr := ""
			if l < len(headerCellLines[idx]) {
				cellStr = headerCellLines[idx][l]
			}
			rb.WriteString(cellBg + alignCellLine(cellStr, colWidths[c], tv.Table.Alignments[c], cellBg, cellFg))
			if c < endCol {
				rb.WriteString(ansiReset + bgTableHeader + fgQuoteBar + " │ ")
			}
		}
		if endCol < numCols-1 {
			rb.WriteString(ansiReset + bgTableHeader + fgFilterBadge + " ▶")
		}
		lines = append(lines, styleLine(rb.String(), width, bgTableHeader, fgH1))
	}

	// Separator row under pinned header
	if len(lines)-1 < gridAvailHeight {
		var sb strings.Builder
		sb.WriteString(" ──")
		for c := tv.ColOffset; c <= endCol; c++ {
			sb.WriteString(strings.Repeat("─", colWidths[c]))
			if c < endCol {
				sb.WriteString("─┼─")
			}
		}
		sb.WriteString("─")
		lines = append(lines, styleLine(fgQuoteBar+sb.String(), width, bgMain, fgQuoteBar))
	}

	dataRowsAvail := gridAvailHeight - (len(lines) - 1)
	if dataRowsAvail < 1 {
		dataRowsAvail = 1
	}

	// Adjust ScrollRow so CursorRow is visible
	if tv.CursorRow < tv.ScrollRow {
		tv.ScrollRow = tv.CursorRow
	}
	if tv.CursorRow >= tv.ScrollRow+dataRowsAvail {
		tv.ScrollRow = tv.CursorRow - dataRowsAvail + 1
	}

	// Render Data Rows
	usedGridLines := 0
	for rIdx := tv.ScrollRow; rIdx < numRows && usedGridLines < dataRowsAvail; rIdx++ {
		row := tv.Table.Rows[rIdx]
		isActiveRow := rIdx == tv.CursorRow
		baseRowBg := bgMain
		if isActiveRow {
			baseRowBg = bgHighlight
		}

		cellLines := make([][]string, endCol-tv.ColOffset+1)
		rowH := 1
		for idx, c := 0, tv.ColOffset; c <= endCol; idx, c = idx+1, c+1 {
			cellBg := baseRowBg
			cellFg := fgDefault
			if isActiveRow && c == tv.CursorCol {
				cellBg = bgSelectFocus
				cellFg = fgBright + ansiBold
			}
			val := ""
			if c < len(row) {
				val = row[c]
			}
			tokens := parseInlineTokens(val, cellBg, cellFg, tv.ShowSyntax)
			wrapped := wrapStyledTokens(tokens, colWidths[c], "", "", cellBg, cellFg)
			cellLines[idx] = wrapped
			if len(wrapped) > rowH {
				rowH = len(wrapped)
			}
		}

		// Ensure CursorRow is never pushed off-screen if an earlier row is very tall
		if rIdx < tv.CursorRow && usedGridLines+rowH > dataRowsAvail {
			tv.ScrollRow = rIdx + 1
			continue
		}

		rowStartY := len(lines)
		for l := 0; l < rowH && usedGridLines < dataRowsAvail; l++ {
			var rb strings.Builder
			cursorPrefix := "  "
			if isActiveRow && l == 0 {
				cursorPrefix = fgActiveCursor + "▸ "
			}
			rb.WriteString(baseRowBg + cursorPrefix)
			for idx, c := 0, tv.ColOffset; c <= endCol; idx, c = idx+1, c+1 {
				cellBg := baseRowBg
				cellFg := fgDefault
				if isActiveRow && c == tv.CursorCol {
					cellBg = bgSelectFocus
					cellFg = fgBright + ansiBold
				}
				cellStr := ""
				if l < len(cellLines[idx]) {
					cellStr = cellLines[idx][l]
				}
				rb.WriteString(cellBg + alignCellLine(cellStr, colWidths[c], tv.Table.Alignments[c], cellBg, cellFg))
				if c < endCol {
					rb.WriteString(ansiReset + baseRowBg + fgQuoteBar + " │ ")
				}
			}
			lines = append(lines, styleLine(rb.String(), width, baseRowBg, fgDefault))
			usedGridLines++
		}
		rowEndY := len(lines) - 1
		if rowEndY >= rowStartY {
			tv.LastRowSpans = append(tv.LastRowSpans, tableRowSpan{
				Row:    rIdx,
				StartY: rowStartY,
				EndY:   rowEndY,
			})
		}
	}

	// Fill any remaining grid rows
	for len(lines)-1 < gridAvailHeight {
		lines = append(lines, styleLine("", width, bgMain, fgDefault))
	}

	// Render Bottom Active Cell Inspector strip (shows full unwrapped/wrapped cell content on mobile)
	colName := ""
	if tv.CursorCol >= 0 && tv.CursorCol < len(tv.Table.Headers) {
		colName = stripInlineMarkdown(tv.Table.Headers[tv.CursorCol])
	}
	cellVal := tv.ActiveCellText()
	_, linkTarget, hasLink := tv.ActiveCellLink()
	inspectorPrefix := fmt.Sprintf(" %s%s:%s ", fgH3, colName, ansiReset+bgContextBar+fgBright)
	inspectorTokens := parseInlineTokens(cellVal, bgContextBar, fgBright, tv.ShowSyntax)
	inspectorWrapped := wrapStyledTokens(inspectorTokens, contentW, inspectorPrefix, "   ", bgContextBar, fgBright)
	for i := 0; i < inspectorHeight; i++ {
		if i < len(inspectorWrapped) {
			if i == 0 && hasLink {
				badge := "[Enter Open Link: " + linkTarget + "] "
				lines = append(lines, styleBarWithRight(" "+inspectorWrapped[i], badge, width, bgContextBar, fgBright, fgLink))
			} else {
				lines = append(lines, styleLine(" "+inspectorWrapped[i], width, bgContextBar, fgBright))
			}
		} else {
			lines = append(lines, styleLine("", width, bgContextBar, fgBright))
		}
	}

	// Footer Bar
	var footer string
	if hasLink {
		footer = formatFooterHints(bgBar,
			HintPair{Key: "↑/↓/←/→", Desc: "Navigate"},
			HintPair{Key: "Enter", Desc: "Open Link"},
			HintPair{Key: "Space", Desc: "View Cell"},
			HintPair{Key: "/", Desc: "Commands"},
			HintPair{Key: "Esc", Desc: "Back"},
		)
	} else {
		footer = formatFooterHints(bgBar,
			HintPair{Key: "↑/↓/←/→", Desc: "Navigate"},
			HintPair{Key: "Enter", Desc: "View Cell"},
			HintPair{Key: "/", Desc: "Commands"},
			HintPair{Key: "Esc", Desc: "Back"},
		)
	}
	lines = append(lines, styleLine(footer, width, bgBar, fgMuted))

	if tv.ShowCellPopup {
		lines = tv.renderWithCellPopup(lines, width, height)
	}
	return lines
}

// ScrollCellPopup scrolls the Cell Detail popup vertically by delta lines.
func (tv *TableViewerState) ScrollCellPopup(delta int, width, height int) {
	tv.CellPopupScroll += delta
	if tv.CellPopupScroll < 0 {
		tv.CellPopupScroll = 0
	}
	_, bodyRows := tv.cellPopupWrappedLines(width, height)
	wrapped, _ := tv.cellPopupWrappedLines(width, height)
	maxScroll := len(wrapped) - bodyRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if tv.CellPopupScroll > maxScroll {
		tv.CellPopupScroll = maxScroll
	}
}

func (tv *TableViewerState) cellPopupDimensions(width, height int) (int, int) {
	popupW := width - 6
	if popupW > 72 {
		popupW = 72
	}
	if popupW < 24 {
		popupW = width - 2
	}
	if popupW < 10 {
		popupW = width
	}
	bodyRows := height - 8
	if bodyRows > 14 {
		bodyRows = 14
	}
	if bodyRows < 3 {
		bodyRows = 3
	}
	return popupW, bodyRows
}

func (tv *TableViewerState) cellPopupWrappedLines(width, height int) ([]string, int) {
	popupW, bodyRows := tv.cellPopupDimensions(width, height)
	innerW := popupW - 4
	if innerW < 6 {
		innerW = popupW - 2
	}
	cellVal := tv.ActiveCellText()
	if strings.TrimSpace(cellVal) == "" {
		cellVal = "(empty cell)"
	}
	tokens := parseInlineTokens(cellVal, bgSidebar, fgBright, tv.ShowSyntax)
	wrapped := wrapStyledTokens(tokens, innerW, "", "", bgSidebar, fgBright)
	return wrapped, bodyRows
}

func (tv *TableViewerState) renderWithCellPopup(baseLines []string, width, height int) []string {
	popupW, bodyRows := tv.cellPopupDimensions(width, height)
	wrapped, _ := tv.cellPopupWrappedLines(width, height)

	maxScroll := len(wrapped) - bodyRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if tv.CellPopupScroll > maxScroll {
		tv.CellPopupScroll = maxScroll
	}
	if tv.CellPopupScroll < 0 {
		tv.CellPopupScroll = 0
	}

	colName := fmt.Sprintf("Column %d", tv.CursorCol+1)
	if tv.CursorCol >= 0 && tv.CursorCol < len(tv.Table.Headers) {
		colName = stripInlineMarkdown(tv.Table.Headers[tv.CursorCol])
	}
	title := fmt.Sprintf(" 🔍 Row %d — %s", tv.CursorRow+1, colName)
	scrollBadge := "[Esc Close] "
	if maxScroll > 0 {
		pct := tv.CellPopupScroll * 100 / maxScroll
		scrollBadge = fmt.Sprintf("[%d%%] [Esc Close] ", pct)
	}

	var popupLines []string
	popupLines = append(popupLines, styleBarWithRight(title, scrollBadge, popupW, bgBar, fgActiveCursor, fgMuted))
	popupLines = append(popupLines, styleLine("", popupW, bgSidebar, fgBright))

	for i := 0; i < bodyRows; i++ {
		lineIdx := tv.CellPopupScroll + i
		if lineIdx < len(wrapped) {
			popupLines = append(popupLines, styleLine("  "+wrapped[lineIdx], popupW, bgSidebar, fgBright))
		} else {
			popupLines = append(popupLines, styleLine("", popupW, bgSidebar, fgBright))
		}
	}

	popupLines = append(popupLines, styleLine("", popupW, bgSidebar, fgBright))
	_, _, hasLink := tv.ActiveCellLink()
	var popupFooter string
	if hasLink {
		popupFooter = formatFooterHints(bgBar,
			HintPair{Key: "↑/↓", Desc: "Scroll"},
			HintPair{Key: "Enter", Desc: "Open Link"},
			HintPair{Key: "Esc", Desc: "Close"},
		)
	} else {
		popupFooter = formatFooterHints(bgBar,
			HintPair{Key: "↑/↓", Desc: "Scroll"},
			HintPair{Key: "Enter/Esc", Desc: "Close"},
		)
	}
	popupLines = append(popupLines, styleLine(popupFooter, popupW, bgBar, fgMuted))

	return overlayPopup(baseLines, popupLines, width, popupW, bgMain)
}

// CellAtScreenPos maps terminal screen coordinates (x, y) in the Table Viewer to (rowIdx, colIdx, true)
// if the pointer is over a visible data cell.
func (tv *TableViewerState) CellAtScreenPos(x, y, width, height int) (int, int, bool) {
	if len(tv.LastRowSpans) == 0 || len(tv.LastColSpans) == 0 {
		_ = tv.RenderLines(width, height)
	}
	hitRow := -1
	for _, rs := range tv.LastRowSpans {
		if y >= rs.StartY && y <= rs.EndY {
			hitRow = rs.Row
			break
		}
	}
	if hitRow < 0 {
		return -1, -1, false
	}

	hitCol := -1
	for _, cs := range tv.LastColSpans {
		if x >= cs.StartX-1 && x <= cs.EndX {
			hitCol = cs.Col
			break
		}
	}
	if hitCol < 0 {
		return -1, -1, false
	}
	return hitRow, hitCol, true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
