package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// ResponsiveBreakpoint is the terminal width below which the sidebar auto-collapses
// and expands as a full-screen mobile overlay.
const ResponsiveBreakpoint = 80

// ReaderHistoryEntry stores a previous document and scroll offset when following links.
type ReaderHistoryEntry struct {
	FilePath       string
	Scroll         int
	SelectionMode  bool
	SelectedIdx    int
}

// SearchMatch represents a single occurrence of a search query on a rendered document line.
type SearchMatch struct {
	RenderLine int
	StartCol   int
	EndCol     int
}

// ReaderState manages the Markdown Reader view and its Header Sidebar.
type ReaderState struct {
	FilePath            string
	DisplayPath         string
	RawContent          string
	Rendered            RenderedDoc
	LastRenderW         int
	LastShowSyntax      bool
	ShowSyntax          bool
	AutoReload          bool
	LastModTime         time.Time
	LastSize            int64
	FileModified        bool // True when the file on disk has changed since last load
	Scroll              int
	SidebarOpen         bool
	SidebarFocused      bool
	HeaderCursor        int
	HeaderScroll        int
	IsNarrow            bool
	SelectionMode       bool          // When true, up/down/left/right navigates links and tables
	SelectedIdx         int           // Index into Rendered.Selectables
	SearchActive        bool          // When true, the bottom search bar is open in the Reader
	SearchQuery         string        // Live search query string
	SearchCaseSensitive bool          // Default false (case-insensitive)
	SearchMatches       []SearchMatch // All occurrences in Rendered.Lines
	SearchMatchIdx      int           // 0-based index of currently focused match in SearchMatches
	StatusMsg           string
	History             []ReaderHistoryEntry
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
		if len(r.Rendered.Selectables) == 0 {
			r.SelectionMode = false
			r.SelectedIdx = 0
		} else if r.SelectedIdx >= len(r.Rendered.Selectables) {
			r.SelectedIdx = len(r.Rendered.Selectables) - 1
		}
		if r.SearchActive && r.SearchQuery != "" {
			r.recomputeSearchMatches()
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

// ToggleSelectionMode toggles Selection Mode on/off in the Reader.
// When entering Selection Mode, it focuses the first selectable item visible in the viewport
// (or the nearest selectable item in the document, auto-scrolling to it).
func (r *ReaderState) ToggleSelectionMode(bodyHeight int) {
	if r.SelectionMode {
		r.SelectionMode = false
		r.StatusMsg = ""
		return
	}
	if len(r.Rendered.Selectables) == 0 {
		r.SelectionMode = false
		r.StatusMsg = "No selectable links or tables in this document"
		return
	}
	r.SelectionMode = true
	r.StatusMsg = ""

	ctxLen := len(r.ActiveContextHeaders(bodyHeight))
	visStart := r.Scroll + ctxLen
	visEnd := r.Scroll + bodyHeight - 1

	// 1. Prefer the first selectable item currently visible in the viewport
	chosen := -1
	for i, item := range r.Rendered.Selectables {
		if item.EndLine >= visStart && item.StartLine <= visEnd {
			chosen = i
			break
		}
	}
	// 2. Otherwise pick the closest selectable item to visStart
	if chosen < 0 {
		bestDist := 1 << 30
		for i, item := range r.Rendered.Selectables {
			dist := item.StartLine - visStart
			if dist < 0 {
				dist = -dist
			}
			if dist < bestDist {
				bestDist = dist
				chosen = i
			}
		}
	}
	if chosen < 0 {
		chosen = 0
	}
	r.SelectedIdx = chosen
	r.ensureSelectedVisible(bodyHeight)
}

// MoveSelection moves focus to the previous/next selectable item in Selection Mode.
func (r *ReaderState) MoveSelection(delta int, bodyHeight int) {
	if len(r.Rendered.Selectables) == 0 {
		r.SelectionMode = false
		return
	}
	r.SelectedIdx += delta
	if r.SelectedIdx < 0 {
		r.SelectedIdx = 0
	}
	if r.SelectedIdx >= len(r.Rendered.Selectables) {
		r.SelectedIdx = len(r.Rendered.Selectables) - 1
	}
	r.ensureSelectedVisible(bodyHeight)
}

// CurrentSelectable returns the currently focused SelectableItem if Selection Mode is active.
func (r *ReaderState) CurrentSelectable() (SelectableItem, bool) {
	if !r.SelectionMode || r.SelectedIdx < 0 || r.SelectedIdx >= len(r.Rendered.Selectables) {
		return SelectableItem{}, false
	}
	return r.Rendered.Selectables[r.SelectedIdx], true
}

// SelectableAtScreenPos checks whether screen coordinates (x, y) in the Reader viewport hit a
// selectable link or table. Returns the index into r.Rendered.Selectables and true if hit.
func (r *ReaderState) SelectableAtScreenPos(x, y, totalWidth, totalHeight int) (int, bool) {
	bodyHeight := totalHeight - 2
	if y < 1 || y > bodyHeight {
		return -1, false
	}
	if r.IsNarrow && r.SidebarOpen {
		return -1, false
	}
	sw := r.sidebarWidth(totalWidth)
	mainW := totalWidth - sw
	if x < 0 || x >= mainW {
		return -1, false
	}

	row := y - 1
	ctxHeaders := r.ActiveContextHeaders(bodyHeight)
	if row < len(ctxHeaders) {
		return -1, false
	}

	docIdx := r.Scroll + row
	if docIdx < 0 || docIdx >= len(r.Rendered.Lines) {
		return -1, false
	}

	for i, item := range r.Rendered.Selectables {
		if docIdx < item.StartLine || docIdx > item.EndLine {
			continue
		}
		switch item.Kind {
		case SelectableLink:
			for _, sp := range item.Spans {
				if sp.RenderLine == docIdx && x >= sp.StartCol && x < sp.EndCol {
					return i, true
				}
			}
		case SelectableTable:
			return i, true
		}
	}
	return -1, false
}

// HeaderAtSidebarScreenPos checks whether screen coordinates (x, y) hit a header row inside
// the Reader's right-hand Header Sidebar (or full-screen narrow overlay).
func (r *ReaderState) HeaderAtSidebarScreenPos(x, y, totalWidth, totalHeight int) (int, bool) {
	bodyHeight := totalHeight - 2
	if y < 1 || y > bodyHeight || len(r.Rendered.Headers) == 0 {
		return -1, false
	}
	if r.IsNarrow {
		if !r.SidebarOpen {
			return -1, false
		}
	} else {
		sw := r.sidebarWidth(totalWidth)
		if sw <= 0 || x < totalWidth-sw {
			return -1, false
		}
	}
	hIdx := r.HeaderScroll + (y - 1)
	if hIdx < 0 || hIdx >= len(r.Rendered.Headers) {
		return -1, false
	}
	return hIdx, true
}

func (r *ReaderState) ensureSelectedVisible(bodyHeight int) {
	item, ok := r.CurrentSelectable()
	if !ok {
		return
	}
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	ctxLen := len(r.ActiveContextHeaders(bodyHeight))
	visStart := r.Scroll + ctxLen
	visEnd := r.Scroll + bodyHeight - 1

	if item.StartLine < visStart {
		r.Scroll = item.StartLine - ctxLen
		if r.Scroll < 0 {
			r.Scroll = 0
		}
		// Recompute sticky header height at new scroll position so item is not hidden underneath sticky headers
		newCtxLen := len(r.ActiveContextHeaders(bodyHeight))
		if item.StartLine < r.Scroll+newCtxLen {
			r.Scroll = item.StartLine - newCtxLen
		}
	} else if item.EndLine > visEnd {
		itemHeight := item.EndLine - item.StartLine + 1
		avail := bodyHeight - ctxLen
		if avail < 1 {
			avail = 1
		}
		if itemHeight > avail {
			// Tall table: align its top just below sticky headers
			r.Scroll = item.StartLine - ctxLen
		} else {
			r.Scroll = item.EndLine - bodyHeight + 1
		}
	}
	r.clampScroll(bodyHeight)
	if !r.SidebarFocused {
		r.syncHeaderCursorToScroll()
	}
}

// OpenSearch opens the bottom search bar in the Reader.
// It keeps the previous SearchCaseSensitive setting and resets the query for fresh typing.
func (r *ReaderState) OpenSearch() {
	r.SearchActive = true
	r.SearchQuery = ""
	r.SearchMatches = nil
	r.SearchMatchIdx = 0
	r.StatusMsg = ""
	if r.IsNarrow && r.SidebarOpen {
		r.SidebarOpen = false
		r.SidebarFocused = false
	} else {
		r.SidebarFocused = false
	}
}

// CloseSearch closes the bottom search bar while keeping the final scroll/navigation position.
func (r *ReaderState) CloseSearch() {
	r.SearchActive = false
}

// SetSearchQuery updates the live search query, recomputes all matches in the file,
// and jumps to the nearest match at or below the current viewport (or wraps around).
func (r *ReaderState) SetSearchQuery(query string, bodyHeight int) {
	r.SearchQuery = query
	r.recomputeSearchMatches()
	if len(r.SearchMatches) == 0 {
		r.SearchMatchIdx = 0
		return
	}
	ctxLen := len(r.ActiveContextHeaders(bodyHeight))
	visStart := r.Scroll + ctxLen
	chosen := 0
	for i, m := range r.SearchMatches {
		if m.RenderLine >= visStart {
			chosen = i
			break
		}
	}
	r.SearchMatchIdx = chosen
	r.ensureSearchMatchVisible(bodyHeight)
}

// ToggleSearchCaseSensitivity toggles between case-insensitive (default) and case-sensitive search,
// immediately recomputing matches for the current query.
func (r *ReaderState) ToggleSearchCaseSensitivity(bodyHeight int) {
	r.SearchCaseSensitive = !r.SearchCaseSensitive
	r.SetSearchQuery(r.SearchQuery, bodyHeight)
}

// StepSearchMatch moves to the next (delta = +1) or previous (delta = -1) occurrence with wrap-around,
// scrolling the viewport so the matched line is clearly visible.
func (r *ReaderState) StepSearchMatch(delta int, bodyHeight int) {
	n := len(r.SearchMatches)
	if n == 0 {
		return
	}
	r.SearchMatchIdx = (r.SearchMatchIdx + delta) % n
	if r.SearchMatchIdx < 0 {
		r.SearchMatchIdx += n
	}
	r.ensureSearchMatchVisible(bodyHeight)
}

func (r *ReaderState) recomputeSearchMatches() {
	r.SearchMatches = nil
	if r.SearchQuery == "" {
		r.SearchMatchIdx = 0
		return
	}
	qRunes := []rune(r.SearchQuery)
	qLen := len(qRunes)
	if qLen == 0 {
		return
	}

	for lineIdx, styledLine := range r.Rendered.Lines {
		plain := strings.TrimRight(stripANSI(styledLine), " ")
		pRunes := []rune(plain)
		if len(pRunes) < qLen {
			continue
		}

		// Precompute visual start column of each rune on this line
		colAt := make([]int, len(pRunes)+1)
		col := 0
		for i, ru := range pRunes {
			colAt[i] = col
			col += runewidth.RuneWidth(ru)
		}
		colAt[len(pRunes)] = col

		for i := 0; i <= len(pRunes)-qLen; {
			matched := true
			for j := 0; j < qLen; j++ {
				a := pRunes[i+j]
				b := qRunes[j]
				if r.SearchCaseSensitive {
					if a != b {
						matched = false
						break
					}
				} else {
					if unicode.ToLower(a) != unicode.ToLower(b) {
						matched = false
						break
					}
				}
			}
			if matched {
				r.SearchMatches = append(r.SearchMatches, SearchMatch{
					RenderLine: lineIdx,
					StartCol:   colAt[i],
					EndCol:     colAt[i+qLen],
				})
				i += qLen
			} else {
				i++
			}
		}
	}

	if len(r.SearchMatches) == 0 {
		r.SearchMatchIdx = 0
	} else if r.SearchMatchIdx >= len(r.SearchMatches) {
		r.SearchMatchIdx = len(r.SearchMatches) - 1
	} else if r.SearchMatchIdx < 0 {
		r.SearchMatchIdx = 0
	}
}

func (r *ReaderState) ensureSearchMatchVisible(bodyHeight int) {
	if len(r.SearchMatches) == 0 || r.SearchMatchIdx < 0 || r.SearchMatchIdx >= len(r.SearchMatches) {
		return
	}
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	matchLine := r.SearchMatches[r.SearchMatchIdx].RenderLine
	ctxLen := len(r.ActiveContextHeaders(bodyHeight))
	visStart := r.Scroll + ctxLen
	visEnd := r.Scroll + bodyHeight - 1

	if matchLine < visStart {
		r.Scroll = matchLine - ctxLen
		if r.Scroll < 0 {
			r.Scroll = 0
		}
		newCtxLen := len(r.ActiveContextHeaders(bodyHeight))
		if matchLine < r.Scroll+newCtxLen {
			r.Scroll = matchLine - newCtxLen
		}
	} else if matchLine > visEnd {
		r.Scroll = matchLine - bodyHeight/2
	}
	r.clampScroll(bodyHeight)
	if !r.SidebarFocused {
		r.syncHeaderCursorToScroll()
	}
}

// JumpToAnchor scrolls the reader to the header matching the given anchor slug or title.
func (r *ReaderState) JumpToAnchor(anchor string, bodyHeight int) bool {
	slug := normalizeHeadingSlug(anchor)
	if slug == "" {
		return false
	}
	for i, h := range r.Rendered.Headers {
		if normalizeHeadingSlug(h.Title) == slug || strings.EqualFold(strings.TrimSpace(h.Title), strings.TrimSpace(anchor)) {
			r.HeaderCursor = i
			r.Scroll = h.RenderLine
			r.clampScroll(bodyHeight)
			return true
		}
	}
	return false
}

func normalizeHeadingSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(s, "#")))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if r == ' ' || r == '-' || r == '_' {
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
			lastDash = false
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// NavigateToFile loads a new Markdown file in the same ReaderState while pushing the current
// file and scroll offset onto the Reader back-history stack.
func (r *ReaderState) NavigateToFile(targetPath string, baseDir string, anchor string, width, height int) error {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(absTarget)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(absTarget)
	if err != nil {
		return err
	}

	// Push current document state onto history
	r.History = append(r.History, ReaderHistoryEntry{
		FilePath:      r.FilePath,
		Scroll:        r.Scroll,
		SelectionMode: r.SelectionMode,
		SelectedIdx:   r.SelectedIdx,
	})

	disp := filepath.Base(absTarget)
	if baseDir != "" {
		if rel, err := filepath.Rel(baseDir, absTarget); err == nil && !strings.HasPrefix(rel, "..") {
			disp = rel
		}
	}

	r.FilePath = absTarget
	r.DisplayPath = disp
	r.RawContent = string(data)
	r.LastModTime = info.ModTime()
	r.LastSize = info.Size()
	r.FileModified = false
	r.Scroll = 0
	r.SelectionMode = false
	r.SelectedIdx = 0
	r.SearchActive = false
	r.SearchQuery = ""
	r.SearchMatches = nil
	r.SearchMatchIdx = 0
	r.StatusMsg = ""
	r.LastRenderW = 0
	r.Reflow(width, height)
	if anchor != "" {
		r.JumpToAnchor(anchor, height-2)
	}
	r.syncHeaderCursorToScroll()
	return nil
}

// PopHistory pops the most recent ReaderHistoryEntry and restores that document and scroll position.
// Returns true if a history entry was popped, or false if the history stack is empty.
func (r *ReaderState) PopHistory(baseDir string, width, height int) bool {
	if len(r.History) == 0 {
		return false
	}
	last := r.History[len(r.History)-1]
	r.History = r.History[:len(r.History)-1]

	info, err := os.Stat(last.FilePath)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(last.FilePath)
	if err != nil {
		return false
	}

	disp := filepath.Base(last.FilePath)
	if baseDir != "" {
		if rel, err := filepath.Rel(baseDir, last.FilePath); err == nil && !strings.HasPrefix(rel, "..") {
			disp = rel
		}
	}

	r.FilePath = last.FilePath
	r.DisplayPath = disp
	r.RawContent = string(data)
	r.LastModTime = info.ModTime()
	r.LastSize = info.Size()
	r.FileModified = false
	r.LastRenderW = 0
	r.Reflow(width, height)
	r.Scroll = last.Scroll
	r.SelectionMode = last.SelectionMode
	r.SelectedIdx = last.SelectedIdx
	if r.SelectedIdx >= len(r.Rendered.Selectables) {
		r.SelectedIdx = maxInt(0, len(r.Rendered.Selectables)-1)
	}
	r.SearchActive = false
	r.SearchQuery = ""
	r.SearchMatches = nil
	r.SearchMatchIdx = 0
	r.StatusMsg = ""
	r.clampScroll(height - 2)
	r.syncHeaderCursorToScroll()
	return true
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
	indent := strings.Repeat("  ", h.Depth)
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
	if r.SelectionMode {
		badges.WriteString(fmt.Sprintf("[SELECT %d/%d] ", r.SelectedIdx+1, len(r.Rendered.Selectables)))
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
	activeItem, hasActiveItem := r.CurrentSelectable()
	for row := 0; row < bodyHeight; row++ {
		var mainLine string
		if row < len(ctxHeaders) {
			mainLine = r.renderContextHeaderLine(ctxHeaders[row], mainW)
		} else {
			docIdx := r.Scroll + row
			if docIdx < len(r.Rendered.Lines) {
				mainLine = r.Rendered.Lines[docIdx]
				if hasActiveItem && docIdx >= activeItem.StartLine && docIdx <= activeItem.EndLine {
					mainLine = r.decorateSelectedLine(mainLine, docIdx, activeItem, mainW)
				}
				if r.SearchActive && len(r.SearchMatches) > 0 {
					mainLine = r.decorateSearchMatches(mainLine, docIdx, mainW)
				}
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

	// 3. Bottom Footer Bar (or active bottom Search Bar when SearchActive is true)
	if r.SearchActive {
		lines = append(lines, r.renderSearchBar(width))
		return lines
	}

	var footerText string
	if r.StatusMsg != "" {
		footerText = " " + fgWarn + r.StatusMsg
	} else if r.SidebarFocused {
		footerText = formatFooterHints(bgBar,
			HintPair{Key: "Enter", Desc: "Open"},
			HintPair{Key: "/", Desc: "Search"},
			HintPair{Key: ":", Desc: "Commands"},
			HintPair{Key: "Esc", Desc: "Quit"},
		)
	} else if r.SelectionMode {
		footerText = formatFooterHints(bgBar,
			HintPair{Key: "Enter", Desc: "Open"},
			HintPair{Key: "/", Desc: "Search"},
			HintPair{Key: ":", Desc: "Commands"},
			HintPair{Key: "Esc", Desc: "Exit Select"},
		)
	} else {
		footerText = formatFooterHints(bgBar,
			HintPair{Key: "/", Desc: "Search"},
			HintPair{Key: ":", Desc: "Commands"},
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

// decorateSearchMatches highlights all search matches on docIdx, using a bright highlight for the
// active match (SearchMatchIdx) and a softer gold highlight for other occurrences on the line.
func (r *ReaderState) decorateSearchMatches(line string, docIdx int, mainW int) string {
	out := line
	otherStyle := bgSearchMatch + fgBright + ansiBold
	activeStyle := bgSearchCurrent + fgSearchCurrent
	for i, m := range r.SearchMatches {
		if m.RenderLine != docIdx {
			continue
		}
		style := otherStyle
		if i == r.SearchMatchIdx {
			style = activeStyle
		}
		out = highlightANSIColRange(out, m.StartCol, m.EndCol, mainW, style)
	}
	return out
}

// renderSearchBar renders the interactive bottom search bar showing the live query,
// match count, case sensitivity indicator, and prev/next/case/close shortcut hints.
func (r *ReaderState) renderSearchBar(width int) string {
	var matchInfo string
	matchColor := fgFilterBadge
	if r.SearchQuery == "" {
		matchInfo = "0 matches"
		matchColor = fgMuted
	} else if len(r.SearchMatches) == 0 {
		matchInfo = "0 matches"
		matchColor = fgNo
	} else {
		matchInfo = fmt.Sprintf("%d/%d matches", r.SearchMatchIdx+1, len(r.SearchMatches))
	}

	caseLabel := "Aa:off"
	caseDescColor := fgHintDesc
	if r.SearchCaseSensitive {
		caseLabel = "Aa:ON"
		caseDescColor = fgFilterBadge
	}

	var rightHints string
	if r.IsNarrow {
		rightHints = fmt.Sprintf("%s[%s]%s  %s↑/↓%s%s Next/Prev  %s^I%s%s %s  %sEsc%s%s Close ",
			matchColor, matchInfo, ansiReset+bgBar,
			fgHintKey, ansiReset+bgBar, fgHintDesc,
			fgHintKey, ansiReset+bgBar, caseDescColor, caseLabel,
			fgHintKey, ansiReset+bgBar, fgHintDesc,
		)
	} else {
		rightHints = fmt.Sprintf("%s[%s]%s   %s↑/↓%s%s Prev/Next   %sTab%s%s Case (%s)   %sEsc%s%s Close ",
			matchColor, matchInfo, ansiReset+bgBar,
			fgHintKey, ansiReset+bgBar, fgHintDesc,
			fgHintKey, ansiReset+bgBar, caseDescColor, caseLabel,
			fgHintKey, ansiReset+bgBar, fgHintDesc,
		)
	}

	leftPrompt := " " + fgActiveCursor + "/" + fgBright + r.SearchQuery + fgActiveCursor + "▎"
	return styleBarWithRight(leftPrompt, rightHints, width, bgBar, fgBright, fgMuted)
}

// decorateSelectedLine highlights the currently focused link or table in Selection Mode.
func (r *ReaderState) decorateSelectedLine(line string, docIdx int, item SelectableItem, mainW int) string {
	switch item.Kind {
	case SelectableLink:
		highlightStyle := bgSelectFocus + fgBright + ansiBold + ansiUnderline
		out := line
		for _, sp := range item.Spans {
			if sp.RenderLine == docIdx {
				out = highlightANSIColRange(out, sp.StartCol, sp.EndCol, mainW, highlightStyle)
			}
		}
		return out
	case SelectableTable:
		if docIdx == item.StartLine {
			plain := strings.TrimSpace(stripANSI(line))
			badge := " [Enter View Table] "
			return styleBarWithRight(" ▸ "+plain, badge, mainW, bgSelectFocus, fgBright+ansiBold, fgFilterBadge)
		}
		plain := stripANSI(line)
		if strings.HasPrefix(plain, " ") {
			plain = "▎" + plain[1:]
		}
		return styleLine(plain, mainW, bgHighlight, fgBright)
	}
	return line
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

	indent := strings.Repeat("  ", h.Depth)
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
		HintPair{Key: "/", Desc: "Search"},
		HintPair{Key: ":", Desc: "Commands"},
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
