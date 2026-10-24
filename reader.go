package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ResponsiveBreakpoint is the terminal width below which the sidebar auto-collapses
// and expands as a full-screen mobile overlay.
const ResponsiveBreakpoint = 80

// ReaderState manages the Markdown Reader view and its Header Sidebar.
type ReaderState struct {
	FilePath       string
	DisplayPath    string
	RawContent     string
	Rendered       RenderedDoc
	LastRenderW    int
	LastShowSyntax bool
	ShowSyntax     bool
	AutoReload     bool
	LastModTime    time.Time
	LastSize       int64
	FileModified   bool // True when the file on disk has changed since last load
	Scroll         int
	SidebarOpen    bool
	SidebarFocused bool
	HeaderCursor   int
	HeaderScroll   int
	IsNarrow       bool
}

// NewReaderState loads a Markdown file and initializes the ReaderState for the given screen dimensions.
func NewReaderState(filePath string, baseDir string, width, height int, showSyntax bool, autoReload ...bool) (*ReaderState, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	disp := filepath.Base(absPath)
	if baseDir != "" {
		if rel, err := filepath.Rel(baseDir, absPath); err == nil && !strings.HasPrefix(rel, "..") {
			disp = rel
		}
	}

	autoRel := false
	if len(autoReload) > 0 {
		autoRel = autoReload[0]
	}

	isNarrow := width < ResponsiveBreakpoint
	r := &ReaderState{
		FilePath:       absPath,
		DisplayPath:    disp,
		RawContent:     string(data),
		ShowSyntax:     showSyntax,
		AutoReload:     autoRel,
		LastModTime:    info.ModTime(),
		LastSize:       info.Size(),
		FileModified:   false,
		SidebarOpen:    !isNarrow, // Auto-collapsed on mobile/narrow screens, open on desktop
		SidebarFocused: false,
		IsNarrow:       isNarrow,
	}
	r.Reflow(width, height)
	return r, nil
}

// CheckFileChanged checks whether the opened file has been modified on disk.
// Returns true if the file changed since the last load.
func (r *ReaderState) CheckFileChanged() bool {
	info, err := os.Stat(r.FilePath)
	if err != nil {
		return r.FileModified
	}
	if !info.ModTime().Equal(r.LastModTime) || info.Size() != r.LastSize {
		data, err := os.ReadFile(r.FilePath)
		if err == nil && string(data) != r.RawContent {
			r.FileModified = true
		} else {
			// Metadata changed or touched, update baseline if content is identical
			r.LastModTime = info.ModTime()
			r.LastSize = info.Size()
		}
	}
	return r.FileModified
}

// Reload re-reads the file from disk, updates modification metadata, and reflows the view
// while preserving the current scroll position.
func (r *ReaderState) Reload(width, height int) error {
	info, err := os.Stat(r.FilePath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(r.FilePath)
	if err != nil {
		return err
	}
	r.RawContent = string(data)
	r.LastModTime = info.ModTime()
	r.LastSize = info.Size()
	r.FileModified = false
	r.LastRenderW = 0 // Force re-render
	r.Reflow(width, height)
	r.syncHeaderCursorToScroll()
	return nil
}

// SetShowSyntax updates whether Markdown syntax & HTML comments are displayed and reflows the document.
func (r *ReaderState) SetShowSyntax(show bool, width, height int) {
	if r.ShowSyntax == show {
		return
	}
	r.ShowSyntax = show
	r.Reflow(width, height)
}

// SetAutoReload updates the auto-reload flag and immediately reloads if a pending change exists.
func (r *ReaderState) SetAutoReload(auto bool, width, height int) {
	r.AutoReload = auto
	if r.AutoReload && r.FileModified {
		_ = r.Reload(width, height)
	}
}

// OnResize updates responsive state when the terminal dimensions change.
func (r *ReaderState) OnResize(width, height int) {
	wasNarrow := r.IsNarrow
	r.IsNarrow = width < ResponsiveBreakpoint

	if r.IsNarrow && !wasNarrow {
		// Automatically collapse sidebar when transitioning to narrow/mobile screen
		r.SidebarOpen = false
		r.SidebarFocused = false
	} else if !r.IsNarrow && wasNarrow {
		// Re-open sidebar on wide screen if it wasn't open
		r.SidebarOpen = true
		r.SidebarFocused = false
	}
	r.Reflow(width, height)
}

// ToggleSidebar expands or collapses the Header Sidebar on both mobile and desktop sizes.
func (r *ReaderState) ToggleSidebar(width, height int) {
	r.SidebarOpen = !r.SidebarOpen
	if r.SidebarOpen {
		if r.IsNarrow {
			// In mobile overlay mode, expanding the sidebar focuses it immediately
			r.SidebarFocused = true
		}
		r.syncHeaderCursorToScroll()
	} else {
		r.SidebarFocused = false
	}
	r.Reflow(width, height)
}

// ToggleFocus switches keyboard focus between the main reader and the header sidebar.
func (r *ReaderState) ToggleFocus(width, height int) {
	if !r.SidebarOpen {
		r.SidebarOpen = true
		r.SidebarFocused = true
		r.syncHeaderCursorToScroll()
		r.Reflow(width, height)
		return
	}
	if r.IsNarrow {
		// In narrow overlay mode, toggling focus off closes the overlay back to reader
		r.SidebarOpen = false
		r.SidebarFocused = false
		return
	}
	r.SidebarFocused = !r.SidebarFocused
	if r.SidebarFocused {
		r.syncHeaderCursorToScroll()
	}
}

// sidebarWidth returns the width of the right-hand sidebar in wide mode.
func (r *ReaderState) sidebarWidth(totalWidth int) int {
	if !r.SidebarOpen || r.IsNarrow {
		return 0
	}
	sw := totalWidth * 30 / 100
	if sw < 24 {
		sw = 24
	}
	if sw > 38 {
		sw = 38
	}
	if sw > totalWidth/2 {
		sw = totalWidth / 2
	}
	return sw
}

// Reflow re-renders the Markdown text if the available reader pane width or syntax toggle has changed.
func (r *ReaderState) Reflow(totalWidth, totalHeight int) {
	if totalWidth < 10 {
		totalWidth = 10
	}
	sw := r.sidebarWidth(totalWidth)
	mainWidth := totalWidth - sw
	if mainWidth < 10 {
		mainWidth = 10
	}

	if r.LastRenderW != mainWidth || r.LastShowSyntax != r.ShowSyntax || len(r.Rendered.Lines) == 0 {
		// Preserve approximate reading progress across reflows
		var ratio float64
		if len(r.Rendered.Lines) > 1 {
			ratio = float64(r.Scroll) / float64(len(r.Rendered.Lines)-1)
		}
		r.Rendered = RenderMarkdown(r.RawContent, mainWidth, r.ShowSyntax)
		r.LastRenderW = mainWidth
		r.LastShowSyntax = r.ShowSyntax
		if len(r.Rendered.Lines) > 1 {
			r.Scroll = int(ratio * float64(len(r.Rendered.Lines)-1))
		}
	}
	bodyHeight := totalHeight - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	r.clampScroll(bodyHeight)
}

func (r *ReaderState) clampScroll(bodyHeight int) {
	maxScroll := len(r.Rendered.Lines) - bodyHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if r.Scroll > maxScroll {
		r.Scroll = maxScroll
	}
	if r.Scroll < 0 {
		r.Scroll = 0
	}
}

// ScrollBy scrolls the document vertically by delta lines.
func (r *ReaderState) ScrollBy(delta int, bodyHeight int) {
	r.Scroll += delta
	r.clampScroll(bodyHeight)
	if !r.SidebarFocused {
		r.syncHeaderCursorToScroll()
	}
}

// ScrollToTop jumps to the first line of the document.
func (r *ReaderState) ScrollToTop() {
	r.Scroll = 0
	if !r.SidebarFocused {
		r.syncHeaderCursorToScroll()
	}
}

// ScrollToBottom jumps to the end of the document.
func (r *ReaderState) ScrollToBottom(bodyHeight int) {
	r.Scroll = len(r.Rendered.Lines)
	r.clampScroll(bodyHeight)
	if !r.SidebarFocused {
		r.syncHeaderCursorToScroll()
	}
}

// MoveHeaderCursor moves the selection cursor inside the Header Sidebar.
func (r *ReaderState) MoveHeaderCursor(delta int, bodyHeight int) {
	if len(r.Rendered.Headers) == 0 {
		r.HeaderCursor = 0
		r.HeaderScroll = 0
		return
	}
	r.HeaderCursor += delta
	if r.HeaderCursor < 0 {
		r.HeaderCursor = 0
	}
	if r.HeaderCursor >= len(r.Rendered.Headers) {
		r.HeaderCursor = len(r.Rendered.Headers) - 1
	}
	r.ensureHeaderVisible(bodyHeight)
}

// JumpToSelectedHeader scrolls the reader to the currently selected header in the sidebar.
func (r *ReaderState) JumpToSelectedHeader(bodyHeight int) {
	if r.HeaderCursor >= 0 && r.HeaderCursor < len(r.Rendered.Headers) {
		target := r.Rendered.Headers[r.HeaderCursor].RenderLine
		r.Scroll = target
		r.clampScroll(bodyHeight)
	}
	// After jumping, return focus to reader (and close mobile overlay if narrow)
	r.SidebarFocused = false
	if r.IsNarrow {
		r.SidebarOpen = false
	}
}

func (r *ReaderState) syncHeaderCursorToScroll() {
	if len(r.Rendered.Headers) == 0 {
		r.HeaderCursor = 0
		return
	}
	best := 0
	for i, h := range r.Rendered.Headers {
		if h.RenderLine <= r.Scroll {
			best = i
		} else {
			break
		}
	}
	r.HeaderCursor = best
}

func (r *ReaderState) ensureHeaderVisible(bodyHeight int) {
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	if r.HeaderCursor < r.HeaderScroll {
		r.HeaderScroll = r.HeaderCursor
	}
	if r.HeaderCursor >= r.HeaderScroll+bodyHeight {
		r.HeaderScroll = r.HeaderCursor - bodyHeight + 1
	}
	maxScroll := len(r.Rendered.Headers) - bodyHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if r.HeaderScroll > maxScroll {
		r.HeaderScroll = maxScroll
	}
	if r.HeaderScroll < 0 {
		r.HeaderScroll = 0
	}
}

// ActiveContextHeaders returns the hierarchy of headers that the document text at
// the current scroll offset is contextually under (from highest outer level to deepest inner level).
func (r *ReaderState) ActiveContextHeaders(bodyHeight int) []HeaderItem {
	if len(r.Rendered.Headers) == 0 || r.Scroll <= 0 {
		return nil
	}
	maxLevels := bodyHeight / 3
	if maxLevels < 1 {
		maxLevels = 1
	}
	if maxLevels > 6 {
		maxLevels = 6
	}

	var stack []HeaderItem
	// Iterate iteratively so that as sticky header rows occupy the top of the viewport,
	// any header pushed underneath the sticky banner also joins the active hierarchy.
	for pass := 0; pass < 6; pass++ {
		topDocLine := r.Scroll + len(stack)
		var nextStack []HeaderItem
		for _, h := range r.Rendered.Headers {
			if h.RenderLine >= topDocLine {
				break
			}
			for len(nextStack) > 0 && nextStack[len(nextStack)-1].Level >= h.Level {
				nextStack = nextStack[:len(nextStack)-1]
			}
			nextStack = append(nextStack, h)
		}
		if len(nextStack) > maxLevels {
			nextStack = nextStack[:maxLevels]
		}
		if len(nextStack) == len(stack) {
			stack = nextStack
			break
		}
		stack = nextStack
	}
	return stack
}

func (r *ReaderState) renderContextHeaderLine(h HeaderItem, width int) string {
	indent := strings.Repeat("  ", h.Level-1)
	hCol := headerColor(h.Level)
	var text string
	if r.ShowSyntax {
		text = " " + indent + hCol + h.Prefix + " " + h.RawTitle
	} else {
		text = " " + indent + hCol + h.Title
	}
	return styleLine(text, width, bgContextBar, hCol)
}

// reloadHintBadge returns the formatted red "↻ r reload " indicator when the file has changed
// on disk and auto-reload mode is off.
func (r *ReaderState) reloadHintBadge(bg string) string {
	if !r.FileModified || r.AutoReload {
		return ""
	}
	return fgReloadKey + "↻ r" + ansiReset + bg + " " + fgReloadDesc + "reload "
}

// RenderLines produces the full-screen lines for the Reader View (including wide split sidebar or narrow full-screen overlay).
func (r *ReaderState) RenderLines(width, height int) []string {
	if width < 10 {
		width = 10
	}
	if height < 3 {
		height = 3
	}
	r.Reflow(width, height)
	bodyHeight := height - 2

	// Case 1: Narrow / Mobile mode with Sidebar expanded -> Full-screen overlay
	if r.IsNarrow && r.SidebarOpen {
		return r.renderMobileSidebarOverlay(width, height)
	}

	sw := r.sidebarWidth(width)
	mainW := width - sw
	r.clampScroll(bodyHeight)
	r.ensureHeaderVisible(bodyHeight)

	var lines []string

	// 1. Top Header Bar
	pct := 100
	maxScroll := len(r.Rendered.Lines) - bodyHeight
	if maxScroll > 0 {
		pct = r.Scroll * 100 / maxScroll
	}
	var badges strings.Builder
	if r.AutoReload {
		badges.WriteString("Auto Reload ")
	}
	if r.ShowSyntax {
		badges.WriteString("[SYNTAX] ")
	}
	badges.WriteString(fmt.Sprintf("[%d%%] ", pct))

	titlePath := r.DisplayPath
	if r.IsNarrow {
		titlePath = filepath.Base(r.FilePath)
	}

	if sw > 0 {
		mainHeader := styleBarWithRight(" 📖 "+titlePath, badges.String(), mainW, bgBar, fgActiveCursor, fgMuted)
		sbHeaderTitle := " Headers"
		if r.SidebarFocused {
			sbHeaderTitle = " ▸ Headers"
		}
		sbHeader := styleLine(sbHeaderTitle, sw, bgSidebarHeader, fgSidebarTitle)
		lines = append(lines, mainHeader+sbHeader)
	} else {
		mainHeader := styleBarWithRight(" 📖 "+titlePath, badges.String(), mainW, bgBar, fgActiveCursor, fgMuted)
		lines = append(lines, mainHeader)
	}

	// 2. Body Rows (with sticky contextual headers pinned at the top of the reader pane)
	ctxHeaders := r.ActiveContextHeaders(bodyHeight)
	for row := 0; row < bodyHeight; row++ {
		var mainLine string
		if row < len(ctxHeaders) {
			mainLine = r.renderContextHeaderLine(ctxHeaders[row], mainW)
		} else {
			docIdx := r.Scroll + row
			if docIdx < len(r.Rendered.Lines) {
				mainLine = r.Rendered.Lines[docIdx]
			} else {
				mainLine = styleLine("", mainW, bgMain, fgDefault)
			}
		}

		if sw > 0 {
			sbLine := r.renderSidebarRow(row, sw)
			lines = append(lines, mainLine+sbLine)
		} else {
			lines = append(lines, mainLine)
		}
	}

	// 3. Bottom Footer Bar (only essential features: Open/Jump when applicable, Commands, Quit — without colons,
	// with the red "r reload" indicator at the right end of the footer bar when modified and AutoReload is off)
	var footerText string
	if r.SidebarFocused {
		footerText = formatFooterHints(bgBar,
			HintPair{Key: "Enter", Desc: "Open"},
			HintPair{Key: "/", Desc: "Commands"},
			HintPair{Key: "Esc", Desc: "Quit"},
		)
	} else {
		footerText = formatFooterHints(bgBar,
			HintPair{Key: "/", Desc: "Commands"},
			HintPair{Key: "Esc", Desc: "Quit"},
		)
	}
	reloadBadge := r.reloadHintBadge(bgBar)
	if reloadBadge != "" {
		lines = append(lines, styleBarWithRight(footerText, reloadBadge, width, bgBar, fgMuted, fgReloadKey))
	} else {
		lines = append(lines, styleLine(footerText, width, bgBar, fgMuted))
	}

	return lines
}

func (r *ReaderState) renderSidebarRow(row int, sw int) string {
	if len(r.Rendered.Headers) == 0 {
		if row == 0 {
			return styleLine(" (no headers)", sw, bgSidebar, fgMuted)
		}
		return styleLine("", sw, bgSidebar, fgDefault)
	}

	hIdx := r.HeaderScroll + row
	if hIdx >= len(r.Rendered.Headers) {
		return styleLine("", sw, bgSidebar, fgDefault)
	}

	h := r.Rendered.Headers[hIdx]
	isSelected := hIdx == r.HeaderCursor
	rowBg := bgSidebar
	cursor := "  "
	if isSelected {
		if r.SidebarFocused {
			rowBg = bgSidebarActive
		} else {
			rowBg = bgHighlight
		}
		cursor = fgActiveCursor + "▸ "
	}

	indent := strings.Repeat("  ", h.Level-1)
	hCol := headerColor(h.Level)
	var text string
	if r.ShowSyntax {
		text = " " + cursor + indent + hCol + h.Prefix + " " + h.RawTitle
	} else {
		text = " " + cursor + indent + hCol + h.Title
	}
	return styleLine(text, sw, rowBg, fgDefault)
}

func (r *ReaderState) renderMobileSidebarOverlay(width, height int) []string {
	bodyHeight := height - 2
	r.ensureHeaderVisible(bodyHeight)

	var lines []string
	headerTitle := " Headers " + filepath.Base(r.FilePath)
	lines = append(lines, styleBarWithRight(headerTitle, "[Overlay] ", width, bgSidebarHeader, fgSidebarTitle, fgMuted))

	for row := 0; row < bodyHeight; row++ {
		lines = append(lines, r.renderSidebarRow(row, width))
	}

	footer := formatFooterHints(bgSidebarHeader,
		HintPair{Key: "Enter", Desc: "Open"},
		HintPair{Key: "/", Desc: "Commands"},
		HintPair{Key: "Esc", Desc: "Quit"},
	)
	reloadBadge := r.reloadHintBadge(bgSidebarHeader)
	if reloadBadge != "" {
		lines = append(lines, styleBarWithRight(footer, reloadBadge, width, bgSidebarHeader, fgMuted, fgReloadKey))
	} else {
		lines = append(lines, styleLine(footer, width, bgSidebarHeader, fgMuted))
	}
	return lines
}
