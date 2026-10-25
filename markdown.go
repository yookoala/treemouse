package main

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// HeaderItem represents a parsed Markdown header and its rendered line position.
type HeaderItem struct {
	Level      int
	Depth      int    // 0-based relative indentation depth based on ancestor headers in the file
	Title      string // Clean title without leading '#' or inline syntax
	RawTitle   string // Raw title with inline syntax intact
	Prefix     string // e.g., "#", "##", "###"
	SourceLine int    // 0-based line in source file
	RenderLine int    // 0-based line in wrapped rendered output
}

// SelectableKind identifies the type of interactive item in Selection Mode.
type SelectableKind int

const (
	SelectableLink SelectableKind = iota
	SelectableTable
)

// SelectableSpan represents a visual column span [StartCol, EndCol) on a single rendered line.
type SelectableSpan struct {
	RenderLine int
	StartCol   int
	EndCol     int
}

// SelectableItem represents a focusable link or table in the Reader's Selection Mode.
type SelectableItem struct {
	Kind       SelectableKind
	StartLine  int
	EndLine    int // Inclusive
	Spans      []SelectableSpan
	LinkLabel  string
	LinkTarget string
	TableIndex int
}

// RenderedDoc holds the wrapped ANSI lines, extracted headers, tables, and selectable items for a given width.
type RenderedDoc struct {
	Lines       []string
	Headers     []HeaderItem
	Tables      []TableItem
	Selectables []SelectableItem
}

var (
	reATXHeader   = regexp.MustCompile(`^(\s{0,3})(#{1,6})\s+(.*)$`)
	reHR          = regexp.MustCompile(`^\s{0,3}((-\s*){3,}|(\*\s*){3,}|(_\s*){3,})\s*$`)
	reUL          = regexp.MustCompile(`^(\s*)([-*+])\s+(.*)$`)
	reOL          = regexp.MustCompile(`^(\s*)(\d+[.)])\s+(.*)$`)
	reBlockquote  = regexp.MustCompile(`^(\s{0,3}>)\s?(.*)$`)
	reFencedBlock = regexp.MustCompile("^\\s{0,3}(```+|~~~+)(.*)$")
)

// styledToken represents a unit of text with its active ANSI formatting.
type styledToken struct {
	text       string
	style      string // ANSI sequences applied before text
	linkIdx    int    // -1 if not a link; >= 0 index of link within the parsed block
	linkLabel  string
	linkTarget string
}

// RenderMarkdown parses raw Markdown content and formats it into wrapped ANSI lines
// fitted to the given width (including a 1-column left/right margin when width allows).
// By default (showSyntax == false), Markdown syntax markers (#, **, *, etc.) and HTML
// comments are hidden while links remain visible and underlined.
func RenderMarkdown(source string, width int, showSyntax ...bool) RenderedDoc {
	displaySyntax := false
	if len(showSyntax) > 0 {
		displaySyntax = showSyntax[0]
	}

	if width < 10 {
		width = 10
	}
	contentWidth := width - 2 // 1 space left margin, 1 space right margin
	if contentWidth < 4 {
		contentWidth = width
	}

	rawLines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var outLines []string
	var headers []HeaderItem
	var tables []TableItem
	var selectables []SelectableItem

	inCodeBlock := false
	fenceMarker := ""
	inHTMLComment := false
	lastRenderedEmpty := false
	commentSkippedSinceEmpty := false

	appendWrappedBlock := func(tokens []styledToken, firstPrefix, contPrefix, lineBg, baseFg string) {
		baseLine := len(outLines)
		wrapped, linkItems := wrapStyledTokensWithLinks(tokens, contentWidth, firstPrefix, contPrefix, lineBg, baseFg, baseLine)
		for _, wl := range wrapped {
			outLines = append(outLines, styleLine(" "+wl, width, lineBg, baseFg))
		}
		selectables = append(selectables, linkItems...)
	}

	// Detect optional YAML frontmatter / meta header at the very start of the file (`---\n...\n---`).
	startSrcIdx := 0
	if fmEndIdx, hasFrontmatter := detectYAMLFrontmatter(rawLines); hasFrontmatter {
		if displaySyntax {
			// In syntax/code displaying mode (`m`), render the raw YAML frontmatter lines in dimmed style
			for i := 0; i <= fmEndIdx; i++ {
				expanded := strings.ReplaceAll(rawLines[i], "\t", "    ")
				wrapped := wrapPlainLine(expanded, contentWidth)
				for _, wl := range wrapped {
					outLines = append(outLines, styleLine(" "+fgUnsupported+wl, width, bgMain, fgUnsupported))
				}
			}
			startSrcIdx = fmEndIdx + 1
		} else {
			// By default, hide the YAML frontmatter and skip any immediately following blank lines
			// so the document starts cleanly without a leading blank gap.
			startSrcIdx = fmEndIdx + 1
			for startSrcIdx < len(rawLines) && strings.TrimSpace(rawLines[startSrcIdx]) == "" {
				startSrcIdx++
			}
		}
	}

	for srcIdx := startSrcIdx; srcIdx < len(rawLines); srcIdx++ {
		line := rawLines[srcIdx]
		// 1. Check fenced code blocks first (comments inside code blocks are literal code)
		if !inHTMLComment {
			if m := reFencedBlock.FindStringSubmatch(line); m != nil {
				lastRenderedEmpty = false
				commentSkippedSinceEmpty = false
				marker := m[1][:3]
				if !inCodeBlock {
					inCodeBlock = true
					fenceMarker = marker
					if displaySyntax {
						outLines = append(outLines, styleLine(" "+fgQuoteBar+line, width, bgCode, fgCode))
					} else {
						lang := strings.TrimSpace(m[2])
						label := "─── code "
						if lang != "" {
							label = "─── " + lang + " "
						}
						fill := contentWidth - visibleWidth(label)
						if fill < 0 {
							fill = 0
						}
						bar := fgQuoteBar + label + strings.Repeat("─", fill)
						outLines = append(outLines, styleLine(" "+bar, width, bgCode, fgCode))
					}
					continue
				} else if strings.HasPrefix(m[1], fenceMarker) {
					inCodeBlock = false
					fenceMarker = ""
					if displaySyntax {
						outLines = append(outLines, styleLine(" "+fgQuoteBar+line, width, bgCode, fgCode))
					} else {
						bar := fgQuoteBar + strings.Repeat("─", contentWidth)
						outLines = append(outLines, styleLine(" "+bar, width, bgCode, fgCode))
					}
					continue
				}
			}
		}

		if inCodeBlock {
			expanded := strings.ReplaceAll(line, "\t", "    ")
			wrapped := wrapPlainLine(expanded, contentWidth)
			for _, wl := range wrapped {
				outLines = append(outLines, styleLine(" "+fgCode+wl, width, bgCode, fgCode))
			}
			continue
		}

		// 2. Process HTML comments (<!-- ... -->)
		if !displaySyntax {
			var skipWholeLine bool
			line, inHTMLComment, skipWholeLine = stripHTMLCommentsFromLine(line, inHTMLComment)
			if skipWholeLine {
				if lastRenderedEmpty {
					commentSkippedSinceEmpty = true
				}
				continue
			}
		} else {
			// When displaying syntax, render full HTML comment lines in dimmed style
			trimmed := strings.TrimSpace(line)
			if inHTMLComment {
				if strings.Contains(line, "-->") {
					inHTMLComment = false
				}
				wrapped := wrapPlainLine(strings.ReplaceAll(line, "\t", "    "), contentWidth)
				for _, wl := range wrapped {
					outLines = append(outLines, styleLine(" "+fgUnsupported+wl, width, bgMain, fgUnsupported))
				}
				lastRenderedEmpty = false
				continue
			}
			if strings.HasPrefix(trimmed, "<!--") {
				if !strings.Contains(trimmed, "-->") {
					inHTMLComment = true
					wrapped := wrapPlainLine(strings.ReplaceAll(line, "\t", "    "), contentWidth)
					for _, wl := range wrapped {
						outLines = append(outLines, styleLine(" "+fgUnsupported+wl, width, bgMain, fgUnsupported))
					}
					lastRenderedEmpty = false
					continue
				}
				if strings.HasSuffix(trimmed, "-->") {
					wrapped := wrapPlainLine(strings.ReplaceAll(line, "\t", "    "), contentWidth)
					for _, wl := range wrapped {
						outLines = append(outLines, styleLine(" "+fgUnsupported+wl, width, bgMain, fgUnsupported))
					}
					lastRenderedEmpty = false
					continue
				}
			}
		}

		// Empty line
		if strings.TrimSpace(line) == "" {
			// If comments were hidden between empty lines, collapse the surrounding empty lines into one
			if !displaySyntax && lastRenderedEmpty && commentSkippedSinceEmpty {
				continue
			}
			outLines = append(outLines, styleLine("", width, bgMain, fgDefault))
			lastRenderedEmpty = true
			commentSkippedSinceEmpty = false
			continue
		}

		lastRenderedEmpty = false
		commentSkippedSinceEmpty = false

		// Horizontal rule (check before unordered list since "* * *" or "---" matches HR)
		if reHR.MatchString(line) && !reATXHeader.MatchString(line) {
			if displaySyntax {
				outLines = append(outLines, styleLine(" "+fgQuoteBar+line, width, bgMain, fgDefault))
			} else {
				hr := fgQuoteBar + strings.Repeat("─", contentWidth)
				outLines = append(outLines, styleLine(" "+hr, width, bgMain, fgDefault))
			}
			continue
		}

		// ATX Header (# .. ######)
		if m := reATXHeader.FindStringSubmatch(line); m != nil {
			hashes := m[2]
			level := len(hashes)
			rawTitle := strings.TrimSpace(strings.TrimRight(m[3], "# "))
			if rawTitle == "" {
				rawTitle = strings.TrimSpace(m[3])
			}
			renderIdx := len(outLines)
			headers = append(headers, HeaderItem{
				Level:      level,
				Title:      stripInlineMarkdown(rawTitle),
				RawTitle:   rawTitle,
				Prefix:     hashes,
				SourceLine: srcIdx,
				RenderLine: renderIdx,
			})

			hCol := headerColor(level)
			headerText := rawTitle
			if displaySyntax {
				headerText = hashes + " " + rawTitle
			}
			tokens := parseInlineTokens(headerText, bgMain, hCol, displaySyntax)
			appendWrappedBlock(tokens, "", "", bgMain, hCol)
			continue
		}

		// GFM Pipe Table (`| Col1 | Col2 |\n| --- | --- |`)
		if tbl, consumed, ok := parseTableBlock(rawLines, srcIdx, displaySyntax); ok {
			tbl.Index = len(tables)
			tbl.StartRenderLine = len(outLines)
			tblLines := renderInlineTable(&tbl, width, contentWidth, displaySyntax)
			outLines = append(outLines, tblLines...)
			tbl.EndRenderLine = len(outLines) - 1
			if tbl.EndRenderLine < tbl.StartRenderLine {
				tbl.EndRenderLine = tbl.StartRenderLine
			}
			tables = append(tables, tbl)
			selectables = append(selectables, SelectableItem{
				Kind:       SelectableTable,
				StartLine:  tbl.StartRenderLine,
				EndLine:    tbl.EndRenderLine,
				TableIndex: tbl.Index,
			})
			srcIdx += consumed - 1
			continue
		}

		// Blockquote (> ...)
		if m := reBlockquote.FindStringSubmatch(line); m != nil {
			qMarker := m[1]
			qText := m[2]
			markerStr := "│ "
			if displaySyntax {
				markerStr = strings.TrimSpace(qMarker) + " "
			}
			firstPrefix := fgQuoteBar + markerStr + ansiReset + bgMain + fgQuote + ansiItalic
			contPrefix := fgQuoteBar + markerStr + ansiReset + bgMain + fgQuote + ansiItalic
			baseFg := fgQuote + ansiItalic
			tokens := parseInlineTokens(qText, bgMain, baseFg, displaySyntax)
			appendWrappedBlock(tokens, firstPrefix, contPrefix, bgMain, baseFg)
			continue
		}

		// Unordered List (- / * / +)
		if m := reUL.FindStringSubmatch(line); m != nil {
			indent := strings.ReplaceAll(m[1], "\t", "  ")
			bullet := m[2]
			itemText := m[3]
			bulletDisplay := "• "
			if displaySyntax {
				bulletDisplay = bullet + " "
			}
			firstPrefix := indent + fgH3 + bulletDisplay + ansiReset + bgMain + fgDefault
			contPrefix := indent + "  "
			tokens := parseInlineTokens(itemText, bgMain, fgDefault, displaySyntax)
			appendWrappedBlock(tokens, firstPrefix, contPrefix, bgMain, fgDefault)
			continue
		}

		// Ordered List (1. / 1))
		if m := reOL.FindStringSubmatch(line); m != nil {
			indent := strings.ReplaceAll(m[1], "\t", "  ")
			num := m[2]
			itemText := m[3]
			firstPrefix := indent + fgH3 + num + " " + ansiReset + bgMain + fgDefault
			contPrefix := indent + strings.Repeat(" ", visibleWidth(num)+1)
			tokens := parseInlineTokens(itemText, bgMain, fgDefault, displaySyntax)
			appendWrappedBlock(tokens, firstPrefix, contPrefix, bgMain, fgDefault)
			continue
		}

		// Normal paragraph line
		expanded := strings.ReplaceAll(line, "\t", "    ")
		tokens := parseInlineTokens(expanded, bgMain, fgDefault, displaySyntax)
		appendWrappedBlock(tokens, "", "", bgMain, fgDefault)
	}

	if len(outLines) == 0 {
		outLines = append(outLines, styleLine("", width, bgMain, fgDefault))
	}

	assignRelativeHeaderDepths(headers)

	return RenderedDoc{
		Lines:       outLines,
		Headers:     headers,
		Tables:      tables,
		Selectables: selectables,
	}
}

// detectYAMLFrontmatter checks if rawLines starts with a YAML frontmatter block (`---` on line 0,
// followed by key-value metadata lines and closed by `---` or `...`).
// Returns the 0-based closing line index and true if a valid frontmatter header is present.
func detectYAMLFrontmatter(rawLines []string) (int, bool) {
	if len(rawLines) < 3 {
		return 0, false
	}
	first := strings.TrimPrefix(rawLines[0], "\ufeff")
	if strings.TrimSpace(first) != "---" {
		return 0, false
	}
	hasColon := false
	for i := 1; i < len(rawLines); i++ {
		trimmed := strings.TrimSpace(rawLines[i])
		if trimmed == "---" || trimmed == "..." {
			if hasColon {
				return i, true
			}
			return 0, false
		}
		if strings.Contains(trimmed, ":") {
			hasColon = true
		}
	}
	return 0, false
}

// assignRelativeHeaderDepths computes the 0-based relative indentation depth for each header
// based on the distinct heading levels present in the document (e.g., if a file only uses
// H2 and H3, H2 gets Depth 0 and H3 gets Depth 1).
func assignRelativeHeaderDepths(headers []HeaderItem) {
	if len(headers) == 0 {
		return
	}
	var present [7]bool
	for _, h := range headers {
		if h.Level >= 1 && h.Level <= 6 {
			present[h.Level] = true
		}
	}
	var rankMap [7]int
	rank := 0
	for lvl := 1; lvl <= 6; lvl++ {
		if present[lvl] {
			rankMap[lvl] = rank
			rank++
		}
	}
	for i := range headers {
		lvl := headers[i].Level
		if lvl >= 1 && lvl <= 6 {
			headers[i].Depth = rankMap[lvl]
		}
	}
}

// stripHTMLCommentsFromLine removes `<!-- ... -->` blocks when showSyntax is false.
// If the line was non-empty and becomes completely empty after stripping comments,
// skipWholeLine is returned as true so comment-only lines leave no blank gap.
func stripHTMLCommentsFromLine(line string, inComment bool) (string, bool, bool) {
	hadComment := inComment || strings.Contains(line, "<!--")
	if !hadComment {
		return line, false, false
	}

	var out strings.Builder
	s := line
	for len(s) > 0 {
		if inComment {
			end := strings.Index(s, "-->")
			if end < 0 {
				s = ""
				break
			}
			inComment = false
			s = s[end+3:]
		} else {
			start := strings.Index(s, "<!--")
			if start < 0 {
				out.WriteString(s)
				break
			}
			out.WriteString(s[:start])
			inComment = true
			s = s[start+4:]
		}
	}

	res := out.String()
	if strings.TrimSpace(res) == "" && strings.TrimSpace(line) != "" {
		return "", inComment, true
	}
	return res, inComment, false
}

// parseInlineTokens parses inline Markdown constructs into styled spans:
// - Links `[text](url "title")`: always keeps full raw Markdown brackets, URL, and quotes + ANSI underline + link color
// - Inline code `` `code` ``: code background + foreground (hides backticks unless showSyntax is true)
// - Bold+Italic `***text***` or `___text___`: ANSI bold + italic
// - Bold `**text**` or `__text__`: ANSI bold
// - Italic `*text*` or `_text_`: ANSI italic
// - Strikethrough `~~text~~`: ANSI strikethrough
// - Underline `<u>text</u>`: ANSI underline
// - Inline HTML comments `<!-- ... -->`: rendered dimmed when showSyntax is true
func parseInlineTokens(s string, lineBg, baseFg string, showSyntax bool) []styledToken {
	baseStyle := ansiReset + lineBg + baseFg
	var spans []styledToken

	i := 0
	n := len(s)
	var plain strings.Builder
	nextLinkIdx := 0

	flushPlain := func() {
		if plain.Len() > 0 {
			spans = append(spans, styledToken{
				text:    plain.String(),
				style:   baseStyle,
				linkIdx: -1,
			})
			plain.Reset()
		}
	}

	for i < n {
		// 0. Inline HTML comment when showSyntax is true
		if showSyntax && strings.HasPrefix(s[i:], "<!--") {
			if end := strings.Index(s[i+4:], "-->"); end >= 0 {
				flushPlain()
				commentText := s[i : i+4+end+3]
				spans = append(spans, styledToken{
					text:    commentText,
					style:   ansiReset + lineBg + fgUnsupported,
					linkIdx: -1,
				})
				i += 4 + end + 3
				continue
			}
		}

		// 1. Inline code: `...`
		if s[i] == '`' {
			end := strings.IndexByte(s[i+1:], '`')
			if end >= 0 {
				flushPlain()
				var codeContent string
				if showSyntax {
					codeContent = s[i : i+1+end+1]
				} else {
					codeContent = s[i+1 : i+1+end]
				}
				spans = append(spans, styledToken{
					text:    codeContent,
					style:   ansiReset + bgCode + fgCode,
					linkIdx: -1,
				})
				i += end + 2
				continue
			}
		}

		// 2. Markdown link (or image link): `[label](url "title")` or `![alt](url)`
		if s[i] == '[' || (s[i] == '!' && i+1 < n && s[i+1] == '[') {
			if linkLen := matchMarkdownLink(s[i:]); linkLen > 0 {
				flushPlain()
				rawLink := s[i : i+linkLen]
				lbl, dest := parseMarkdownLinkParts(rawLink)
				// Always keep full markdown brackets, quotes, and URL with ANSI underline
				spans = append(spans, styledToken{
					text:       rawLink,
					style:      baseStyle + fgLink + ansiUnderline,
					linkIdx:    nextLinkIdx,
					linkLabel:  lbl,
					linkTarget: dest,
				})
				nextLinkIdx++
				i += linkLen
				continue
			}
		}

		// 3. HTML <u>...</u> underline tag
		if strings.HasPrefix(strings.ToLower(s[i:]), "<u>") {
			lowerRest := strings.ToLower(s[i+3:])
			if end := strings.Index(lowerRest, "</u>"); end >= 0 {
				flushPlain()
				var inner string
				if showSyntax {
					inner = s[i : i+3+end+4]
				} else {
					inner = s[i+3 : i+3+end]
				}
				spans = append(spans, styledToken{
					text:    inner,
					style:   baseStyle + ansiUnderline,
					linkIdx: -1,
				})
				i += 3 + end + 4
				continue
			}
		}

		// 4. Bold + Italic: ***...*** or ___...___
		if strings.HasPrefix(s[i:], "***") || strings.HasPrefix(s[i:], "___") {
			delim := s[i : i+3]
			if end := strings.Index(s[i+3:], delim); end > 0 {
				flushPlain()
				var inner string
				if showSyntax {
					inner = s[i : i+3+end+3]
				} else {
					inner = s[i+3 : i+3+end]
				}
				spans = append(spans, styledToken{
					text:    inner,
					style:   baseStyle + ansiBold + ansiItalic,
					linkIdx: -1,
				})
				i += 3 + end + 3
				continue
			}
		}

		// 5. Bold: **...** or __...__
		if strings.HasPrefix(s[i:], "**") || strings.HasPrefix(s[i:], "__") {
			delim := s[i : i+2]
			if end := strings.Index(s[i+2:], delim); end > 0 {
				flushPlain()
				var inner string
				if showSyntax {
					inner = s[i : i+2+end+2]
				} else {
					inner = s[i+2 : i+2+end]
				}
				spans = append(spans, styledToken{
					text:    inner,
					style:   baseStyle + ansiBold,
					linkIdx: -1,
				})
				i += 2 + end + 2
				continue
			}
		}

		// 6. Strikethrough: ~~...~~
		if strings.HasPrefix(s[i:], "~~") {
			if end := strings.Index(s[i+2:], "~~"); end > 0 {
				flushPlain()
				var inner string
				if showSyntax {
					inner = s[i : i+2+end+2]
				} else {
					inner = s[i+2 : i+2+end]
				}
				spans = append(spans, styledToken{
					text:    inner,
					style:   baseStyle + ansiStrike,
					linkIdx: -1,
				})
				i += 2 + end + 2
				continue
			}
		}

		// 7. Italic: *...* or _..._
		if s[i] == '*' || s[i] == '_' {
			delim := s[i : i+1]
			if end := strings.Index(s[i+1:], delim); end > 0 {
				rawInner := s[i+1 : i+1+end]
				if !strings.HasPrefix(rawInner, " ") && !strings.HasSuffix(rawInner, " ") {
					flushPlain()
					var text string
					if showSyntax {
						text = s[i : i+1+end+1]
					} else {
						text = rawInner
					}
					spans = append(spans, styledToken{
						text:    text,
						style:   baseStyle + ansiItalic,
						linkIdx: -1,
					})
					i += 1 + end + 1
					continue
				}
			}
		}

		plain.WriteByte(s[i])
		i++
	}
	flushPlain()
	return spans
}

// matchMarkdownLink matches `[text](dest)` or `![alt](dest)` at the start of s
// and returns the byte length of the full link, or 0 if not a valid link.
func matchMarkdownLink(s string) int {
	pos := 0
	if strings.HasPrefix(s, "![") {
		pos = 2
	} else if strings.HasPrefix(s, "[") {
		pos = 1
	} else {
		return 0
	}

	bracketDepth := 1
	for pos < len(s) && bracketDepth > 0 {
		if s[pos] == '\\' && pos+1 < len(s) {
			pos += 2
			continue
		}
		if s[pos] == '[' {
			bracketDepth++
		} else if s[pos] == ']' {
			bracketDepth--
		}
		pos++
	}
	if bracketDepth != 0 || pos >= len(s) || s[pos] != '(' {
		return 0
	}

	pos++ // skip '('
	parenDepth := 1
	inQuotes := byte(0)
	for pos < len(s) && parenDepth > 0 {
		ch := s[pos]
		if ch == '\\' && pos+1 < len(s) {
			pos += 2
			continue
		}
		if inQuotes != 0 {
			if ch == inQuotes {
				inQuotes = 0
			}
		} else {
			if ch == '"' || ch == '\'' {
				inQuotes = ch
			} else if ch == '(' {
				parenDepth++
			} else if ch == ')' {
				parenDepth--
			}
		}
		pos++
	}
	if parenDepth != 0 {
		return 0
	}
	return pos
}

// parseMarkdownLinkParts extracts the label and destination URL/path from a matched Markdown link
// such as `[label](url "optional title")` or `![alt](<url>)`.
func parseMarkdownLinkParts(rawLink string) (string, string) {
	pos := 0
	if strings.HasPrefix(rawLink, "![") {
		pos = 2
	} else if strings.HasPrefix(rawLink, "[") {
		pos = 1
	} else {
		return "", ""
	}
	labelStart := pos
	bracketDepth := 1
	for pos < len(rawLink) && bracketDepth > 0 {
		if rawLink[pos] == '\\' && pos+1 < len(rawLink) {
			pos += 2
			continue
		}
		if rawLink[pos] == '[' {
			bracketDepth++
		} else if rawLink[pos] == ']' {
			bracketDepth--
		}
		pos++
	}
	if bracketDepth != 0 || pos >= len(rawLink) || rawLink[pos] != '(' {
		return "", ""
	}
	label := rawLink[labelStart : pos-1]
	destSpec := strings.TrimSpace(rawLink[pos+1 : len(rawLink)-1])
	if destSpec == "" {
		return label, ""
	}
	if strings.HasPrefix(destSpec, "<") {
		if end := strings.IndexByte(destSpec, '>'); end > 0 {
			return label, destSpec[1:end]
		}
	}
	// Strip optional title `"title"` or `'title'` after whitespace
	for i := 0; i < len(destSpec); i++ {
		if destSpec[i] == ' ' || destSpec[i] == '\t' {
			return label, strings.TrimSpace(destSpec[:i])
		}
	}
	return label, destSpec
}

// stripInlineMarkdown removes basic inline formatting markers for clean sidebar display.
func stripInlineMarkdown(s string) string {
	s, _, _ = stripHTMLCommentsFromLine(s, false)
	for _, marker := range []string{"***", "___", "**", "__", "~~", "`", "<u>", "</u>", "*", "_"} {
		s = strings.ReplaceAll(s, marker, "")
	}
	return strings.TrimSpace(s)
}

// wrapUnit represents a breakable chunk of styled text (e.g. an ASCII word, space, or single CJK character).
type wrapUnit struct {
	text    string
	style   string
	width   int
	isSpace bool
	linkIdx int
}

// splitIntoWrapUnits breaks styled tokens into word/CJK wrapping units so English words
// wrap cleanly on spaces and CJK characters wrap cleanly at any character boundary.
func splitIntoWrapUnits(tokens []styledToken) []wrapUnit {
	var units []wrapUnit
	for _, tok := range tokens {
		var wordBuf strings.Builder
		wordWidth := 0
		flushWord := func() {
			if wordBuf.Len() > 0 {
				units = append(units, wrapUnit{
					text:    wordBuf.String(),
					style:   tok.style,
					width:   wordWidth,
					linkIdx: tok.linkIdx,
				})
				wordBuf.Reset()
				wordWidth = 0
			}
		}

		for _, r := range tok.text {
			rw := runewidth.RuneWidth(r)
			if r == ' ' {
				flushWord()
				units = append(units, wrapUnit{
					text:    " ",
					style:   tok.style,
					width:   1,
					isSpace: true,
					linkIdx: tok.linkIdx,
				})
			} else if rw > 1 || isCJKPunct(r) {
				// Wide CJK rune or full-width punctuation can break before/after
				flushWord()
				units = append(units, wrapUnit{
					text:    string(r),
					style:   tok.style,
					width:   rw,
					linkIdx: tok.linkIdx,
				})
			} else {
				wordBuf.WriteRune(r)
				wordWidth += rw
			}
		}
		flushWord()
	}
	return units
}

func isCJKPunct(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// wrapStyledTokens wraps styled tokens into lines of at most maxWidth display columns.
func wrapStyledTokens(tokens []styledToken, maxWidth int, firstPrefix, contPrefix, lineBg, baseFg string) []string {
	lines, _ := wrapStyledTokensWithLinks(tokens, maxWidth, firstPrefix, contPrefix, lineBg, baseFg, 0)
	return lines
}

// wrapStyledTokensWithLinks wraps styled tokens and records SelectableItem spans for any Markdown links.
// Note: inline lines rendered into the main reader receive a 1-space left margin (`" "+wl`),
// so recorded column spans are offset by +1 to match final rendered line columns.
func wrapStyledTokensWithLinks(tokens []styledToken, maxWidth int, firstPrefix, contPrefix, lineBg, baseFg string, baseRenderLine int) ([]string, []SelectableItem) {
	if maxWidth < 4 {
		maxWidth = 4
	}
	units := splitIntoWrapUnits(tokens)
	baseStyle := ansiReset + lineBg + baseFg

	// Build link metadata lookup by linkIdx
	type linkMeta struct {
		label  string
		target string
		spans  []SelectableSpan
	}
	linkMap := make(map[int]*linkMeta)
	var linkOrder []int
	for _, tok := range tokens {
		if tok.linkIdx >= 0 {
			if _, exists := linkMap[tok.linkIdx]; !exists {
				linkMap[tok.linkIdx] = &linkMeta{
					label:  tok.linkLabel,
					target: tok.linkTarget,
				}
				linkOrder = append(linkOrder, tok.linkIdx)
			}
		}
	}

	prefix := firstPrefix
	prefixW := visibleWidth(prefix)
	if prefixW >= maxWidth-2 {
		prefix = ""
		prefixW = 0
	}

	var lines []string
	var curLine strings.Builder
	curLine.WriteString(baseStyle)
	curLine.WriteString(prefix)
	curW := prefixW
	hasContentOnLine := false
	activeStyle := ""

	recordLinkCols := func(linkIdx int, startCol, endCol int) {
		if linkIdx < 0 || endCol <= startCol {
			return
		}
		meta := linkMap[linkIdx]
		if meta == nil {
			return
		}
		renderLine := baseRenderLine + len(lines)
		// +1 for the leading 1-column margin added by styleLine(" "+wl, ...)
		sCol := startCol + 1
		eCol := endCol + 1
		if n := len(meta.spans); n > 0 && meta.spans[n-1].RenderLine == renderLine && meta.spans[n-1].EndCol == sCol {
			meta.spans[n-1].EndCol = eCol
		} else {
			meta.spans = append(meta.spans, SelectableSpan{
				RenderLine: renderLine,
				StartCol:   sCol,
				EndCol:     eCol,
			})
		}
	}

	writeStyled := func(style, text string, linkIdx int, width int) {
		if style != activeStyle {
			curLine.WriteString(style)
			activeStyle = style
		}
		curLine.WriteString(text)
		recordLinkCols(linkIdx, curW, curW+width)
	}

	startNewLine := func() {
		lines = append(lines, curLine.String()+baseStyle)
		curLine.Reset()
		p := contPrefix
		pw := visibleWidth(p)
		if pw >= maxWidth-2 {
			p = ""
			pw = 0
		}
		curLine.WriteString(baseStyle)
		curLine.WriteString(p)
		curW = pw
		hasContentOnLine = false
		activeStyle = ""
	}

	for _, u := range units {
		// Skip leading spaces on wrapped continuation lines
		if u.isSpace && !hasContentOnLine && len(lines) > 0 {
			continue
		}

		if curW+u.width <= maxWidth {
			writeStyled(u.style, u.text, u.linkIdx, u.width)
			curW += u.width
			if !u.isSpace {
				hasContentOnLine = true
			}
			continue
		}

		// If the unit fits on a fresh line, wrap before writing it
		contW := visibleWidth(contPrefix)
		if contW >= maxWidth-2 {
			contW = 0
		}
		if hasContentOnLine && contW+u.width <= maxWidth {
			startNewLine()
			if u.isSpace {
				continue
			}
			writeStyled(u.style, u.text, u.linkIdx, u.width)
			curW += u.width
			hasContentOnLine = true
			continue
		}

		// Otherwise the single token itself is wider than the line (e.g. a long URL); break rune-by-rune
		for _, r := range u.text {
			rw := runewidth.RuneWidth(r)
			if curW+rw > maxWidth && curW > 0 {
				startNewLine()
			}
			writeStyled(u.style, string(r), u.linkIdx, rw)
			curW += rw
			hasContentOnLine = true
		}
	}

	lines = append(lines, curLine.String()+baseStyle)

	var items []SelectableItem
	for _, idx := range linkOrder {
		meta := linkMap[idx]
		if meta == nil || len(meta.spans) == 0 {
			continue
		}
		items = append(items, SelectableItem{
			Kind:       SelectableLink,
			StartLine:  meta.spans[0].RenderLine,
			EndLine:    meta.spans[len(meta.spans)-1].RenderLine,
			Spans:      meta.spans,
			LinkLabel:  meta.label,
			LinkTarget: meta.target,
			TableIndex: -1,
		})
	}
	return lines, items
}

// wrapPlainLine wraps an unstyled line (such as a code block line) to maxWidth display columns.
func wrapPlainLine(s string, maxWidth int) []string {
	if maxWidth < 4 {
		maxWidth = 4
	}
	if s == "" {
		return []string{""}
	}
	var lines []string
	var cur strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > maxWidth && w > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			w = 0
		}
		cur.WriteRune(r)
		w += rw
	}
	if cur.Len() > 0 || len(lines) == 0 {
		lines = append(lines, cur.String())
	}
	return lines
}
