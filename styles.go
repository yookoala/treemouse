package main

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// ANSI escape sequences and 256-color palette for borderless dark UI.
const (
	ansiReset         = "\x1b[0m"
	ansiBold          = "\x1b[1m"
	ansiBoldOff       = "\x1b[22m"
	ansiItalic        = "\x1b[3m"
	ansiItalicOff     = "\x1b[23m"
	ansiUnderline     = "\x1b[4m"
	ansiUnderlineOff  = "\x1b[24m"
	ansiStrike        = "\x1b[9m"
	ansiStrikeOff     = "\x1b[29m"

	// Backgrounds
	bgMain            = "\x1b[48;5;233m" // Deep dark background for main content
	bgContextBar      = "\x1b[48;5;234m" // Sticky contextual headers background (between header bar and main content)
	bgSidebar         = "\x1b[48;5;235m" // Slightly lighter dark background for right sidebar
	bgBar             = "\x1b[48;5;236m" // Top header bar & bottom footer bar background
	bgCode            = "\x1b[48;5;236m" // Inline/block code background
	bgHighlight       = "\x1b[48;5;237m" // Active selected row background
	bgSidebarHeader   = "\x1b[48;5;238m" // Sidebar header bar & popup panel background
	bgSidebarActive   = "\x1b[48;5;239m" // Active row inside sidebar when focused
	bgSelectFocus     = "\x1b[48;5;24m"  // Highlight background for focused selectable link/table in Selection Mode
	bgTableHeader     = "\x1b[48;5;236m" // Table header row background

	// Foregrounds
	fgDefault         = "\x1b[38;5;252m" // Standard readable text
	fgBright          = "\x1b[38;5;255m" // Bright white text
	fgHintKey         = "\x1b[1;38;5;254m" // Bright key in footer hints
	fgHintDesc        = "\x1b[38;5;244m" // Darker description in footer hints
	fgMuted           = "\x1b[38;5;248m" // Secondary info
	fgUnsupported     = "\x1b[38;5;240m" // Dimmed color for unsupported file types
	fgQuote           = "\x1b[38;5;250m" // Blockquote text
	fgQuoteBar        = "\x1b[38;5;245m" // Blockquote vertical bar & HR (visible on mobile OLED screens)
	fgCode            = "\x1b[38;5;216m" // Inline & block code text
	fgLink            = "\x1b[38;5;75m"  // Markdown link color
	fgFolder          = "\x1b[1;34m"     // Folder item color
	fgActiveCursor    = "\x1b[1;36m"     // Active selected item color
	fgFilterBadge     = "\x1b[38;5;221m" // Filter indicator in header
	fgSidebarTitle    = "\x1b[1;35m"     // Sidebar header title
	fgYes             = "\x1b[1;32m"       // Confirm Yes
	fgNo              = "\x1b[1;31m"       // Confirm No
	fgReloadKey       = "\x1b[1;38;5;196m" // Bright red for 'r' reload key hint
	fgReloadDesc      = "\x1b[38;5;124m"   // Darker red for 'reload' label
	fgWarn            = "\x1b[38;5;214m"   // Status warning in footer

	// Markdown Header Level Colors (all bold)
	fgH1              = "\x1b[1;38;5;81m"  // Cyan
	fgH2              = "\x1b[1;38;5;114m" // Green
	fgH3              = "\x1b[1;38;5;221m" // Gold
	fgH4              = "\x1b[1;38;5;213m" // Pink/Magenta
	fgH5              = "\x1b[1;38;5;111m" // Soft Blue
	fgH6              = "\x1b[1;38;5;183m" // Lavender
)

func init() {
	// Always treat East Asian Ambiguous width characters (such as box-drawing '─', '│',
	// bullets '•', arrows '▸', and ellipsis '…') as 1 column wide so they match modern
	// terminal emulators and do not misalign or overflow lines under CJK locales.
	runewidth.DefaultCondition.EastAsianWidth = false
}

// headerColor returns the bold ANSI foreground escape sequence for header levels 1..6.
func headerColor(level int) string {
	switch level {
	case 1:
		return fgH1
	case 2:
		return fgH2
	case 3:
		return fgH3
	case 4:
		return fgH4
	case 5:
		return fgH5
	default:
		return fgH6
	}
}

// HintPair represents a key and its usage label for footer bars.
type HintPair struct {
	Key  string
	Desc string
}

// formatFooterHints formats a slice of HintPair without colons, using bright text
// for the key and darker text for the usage description.
func formatFooterHints(bg string, pairs ...HintPair) string {
	var parts []string
	for _, p := range pairs {
		parts = append(parts, fgHintKey+p.Key+ansiReset+bg+" "+fgHintDesc+p.Desc)
	}
	return " " + strings.Join(parts, "   ")
}

// visibleWidth calculates the terminal display width of a string, ignoring ANSI escape sequences.
func visibleWidth(s string) int {
	w := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		w += runewidth.RuneWidth(r)
	}
	return w
}

// truncateANSI truncates an ANSI-styled string to fit within maxWidth display columns,
// appending an ellipsis ("…") if truncated and preserving a trailing reset if needed.
func truncateANSI(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if visibleWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}

	target := maxWidth - 1
	var b strings.Builder
	w := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		rw := runewidth.RuneWidth(r)
		if w+rw > target {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	b.WriteRune('…')
	return b.String()
}

// styleLine wraps content in the given background and default foreground, truncating or
// padding with spaces to exact width columns so the background color fills the entire line.
func styleLine(content string, width int, bg string, fg string) string {
	if width <= 0 {
		return ""
	}
	truncated := truncateANSI(content, width)
	vw := visibleWidth(truncated)
	pad := width - vw
	if pad < 0 {
		pad = 0
	}
	return bg + fg + truncated + bg + strings.Repeat(" ", pad) + ansiReset
}

// styleBarWithRight renders a single-line bar with left-aligned content and right-aligned badge.
func styleBarWithRight(left, right string, width int, bg, leftFg, rightFg string) string {
	if width <= 0 {
		return ""
	}
	rw := visibleWidth(right)
	if rw >= width {
		return styleLine(right, width, bg, rightFg)
	}
	availLeft := width - rw - 1
	leftTrunc := truncateANSI(left, availLeft)
	lw := visibleWidth(leftTrunc)
	spaces := width - lw - rw
	if spaces < 1 {
		spaces = 1
	}
	return bg + leftFg + leftTrunc + bg + strings.Repeat(" ", spaces) + rightFg + right + ansiReset
}

// overlayPopup overlays a solid rectangular block of styled lines onto the center of baseLines.
func overlayPopup(baseLines []string, popupLines []string, totalWidth int, popupWidth int, baseBg string) []string {
	if len(baseLines) == 0 || len(popupLines) == 0 {
		return baseLines
	}
	out := make([]string, len(baseLines))
	copy(out, baseLines)

	startRow := (len(baseLines) - len(popupLines)) / 2
	if startRow < 1 {
		startRow = 1
	}
	leftCol := (totalWidth - popupWidth) / 2
	if leftCol < 0 {
		leftCol = 0
	}
	rightCol := leftCol + popupWidth

	for i, pLine := range popupLines {
		rowIdx := startRow + i
		if rowIdx < 0 || rowIdx >= len(out) {
			continue
		}
		leftPart := sliceANSI(out[rowIdx], 0, leftCol, baseBg)
		rightPart := sliceANSI(out[rowIdx], rightCol, totalWidth, baseBg)
		out[rowIdx] = leftPart + pLine + rightPart + ansiReset
	}
	return out
}

// sliceANSI extracts a visual column slice [startCol, endCol) from an ANSI-styled line,
// padding with spaces using fallbackBg if the line is shorter or wide runes straddle boundaries.
func sliceANSI(s string, startCol, endCol int, fallbackBg string) string {
	if endCol <= startCol {
		return ""
	}
	var out strings.Builder
	var activeEsc strings.Builder
	col := 0
	inEsc := false
	var escBuf strings.Builder
	started := false

	for _, r := range s {
		if inEsc {
			escBuf.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
				seq := escBuf.String()
				activeEsc.WriteString(seq)
				if started && col < endCol {
					out.WriteString(seq)
				}
				escBuf.Reset()
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			escBuf.WriteRune(r)
			continue
		}

		rw := runewidth.RuneWidth(r)
		nextCol := col + rw

		if nextCol <= startCol {
			col = nextCol
			continue
		}
		if col >= endCol {
			break
		}

		if !started {
			started = true
			out.WriteString(fallbackBg)
			out.WriteString(activeEsc.String())
			// If a wide character straddles startCol, pad the leading column
			if col < startCol {
				out.WriteString(strings.Repeat(" ", nextCol-startCol))
				col = nextCol
				continue
			}
		}

		// If a wide character would exceed endCol, pad with space instead
		if nextCol > endCol {
			out.WriteString(strings.Repeat(" ", endCol-col))
			col = endCol
			break
		}

		out.WriteRune(r)
		col = nextCol
	}

	if !started {
		out.WriteString(fallbackBg)
	}
	if col < endCol {
		out.WriteString(fallbackBg)
		out.WriteString(strings.Repeat(" ", endCol-col))
	}
	return out.String()
}

// highlightANSIColRange applies a highlight style (e.g. bgSelectFocus + fgBright + ansiBold + ansiUnderline)
// to the visual column span [startCol, endCol) of an ANSI-styled line of totalWidth columns.
func highlightANSIColRange(line string, startCol, endCol, totalWidth int, highlightStyle string) string {
	if startCol < 0 {
		startCol = 0
	}
	if endCol > totalWidth {
		endCol = totalWidth
	}
	if endCol <= startCol {
		return line
	}
	left := sliceANSI(line, 0, startCol, bgMain)
	mid := stripANSI(sliceANSI(line, startCol, endCol, bgMain))
	right := sliceANSI(line, endCol, totalWidth, bgMain)
	return left + ansiReset + highlightStyle + mid + ansiReset + right + ansiReset
}

// stripANSI removes all ANSI escape sequences from s.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
