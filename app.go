package main

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// AppMode represents the active view in the mode stack.
type AppMode int

const (
	ModeBrowser AppMode = iota // Base Mode: File Browser
	ModeReader                 // Pushed Mode: Markdown File Reader
)

// CommandOption represents an executable command in the "/" Command Palette popup.
type CommandOption struct {
	ID       string
	Label    string
	Shortcut string // Displayed in brackets on the right side, e.g. "[m]"
}

// AppModel is the root Bubble Tea model managing the mode stack and responsive layout.
type AppModel struct {
	Mode            AppMode
	RootDir         string
	Browser         *BrowserState
	Reader          *ReaderState
	ShowSyntax      bool // Whether Markdown syntax & HTML comments are displayed
	ShowCommandMenu bool // Whether the "/" Commands popup is open
	CommandQuery    string
	CommandCursor   int
	CommandScroll   int
	ShowHelpManual  bool // Whether the Help Manual modal is open
	ShowQuitConfirm bool
	Width           int
	Height          int
}

// NewAppModel constructs the application model from an optional CLI path argument.
// Regardless of whether a file or directory is specified, the File Browser is always
// initialized as the Base Mode so Esc always returns to it before quitting.
func NewAppModel(targetPath string) (*AppModel, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	absCwd, _ := filepath.Abs(cwd)

	width := 80
	height := 24

	if targetPath == "" {
		browser, err := NewBrowserState(absCwd)
		if err != nil {
			return nil, err
		}
		return &AppModel{
			Mode:    ModeBrowser,
			RootDir: absCwd,
			Browser: browser,
			Width:   width,
			Height:  height,
		}, nil
	}

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absTarget)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		browser, err := NewBrowserState(absTarget)
		if err != nil {
			return nil, err
		}
		return &AppModel{
			Mode:    ModeBrowser,
			RootDir: absTarget,
			Browser: browser,
			Width:   width,
			Height:  height,
		}, nil
	}

	// Target is a file: initialize the File Browser in the file's parent directory
	// with the cursor on that file, and push the Reader View on top if supported.
	parentDir := filepath.Dir(absTarget)
	fileName := filepath.Base(absTarget)
	browser, err := NewBrowserState(parentDir)
	if err != nil {
		return nil, err
	}
	_ = browser.LoadDir(parentDir, fileName)

	m := &AppModel{
		Mode:    ModeBrowser,
		RootDir: absCwd,
		Browser: browser,
		Width:   width,
		Height:  height,
	}

	if IsSupportedMarkdown(fileName) {
		reader, err := NewReaderState(absTarget, absCwd, width, height, m.ShowSyntax)
		if err != nil {
			return nil, err
		}
		m.Reader = reader
		m.Mode = ModeReader
	} else {
		m.Browser.StatusMsg = "Unsupported file type: " + fileName + " (only .md/.markdown supported)"
	}

	return m, nil
}

// Init implements tea.Model.
func (m *AppModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		if m.Reader != nil {
			m.Reader.OnResize(m.Width, m.Height)
		}
		return m, nil

	case tea.KeyMsg:
		key := msg.String()

		// Hard emergency interrupt
		if key == "ctrl+c" {
			return m, tea.Quit
		}

		// 1. If Quit Confirmation Popup is open over Base Mode:
		// Only Y/y confirms quitting, and N/n cancels.
		if m.ShowQuitConfirm {
			switch strings.ToLower(key) {
			case "y":
				return m, tea.Quit
			case "n", "esc":
				m.ShowQuitConfirm = false
				return m, nil
			}
			return m, nil
		}

		// 2. If Help Manual Popup is open:
		if m.ShowHelpManual {
			switch key {
			case "esc", "q", "?", "enter":
				m.ShowHelpManual = false
			}
			return m, nil
		}

		// 3. If "/" Commands Popup is open:
		if m.ShowCommandMenu {
			return m.updateCommandMenu(msg)
		}

		// 4. Global "/" shortcut opens the Commands popup from any mode
		if key == "/" {
			m.openCommandMenu()
			return m, nil
		}

		// 5. Global "?" shortcut opens Help Manual
		if key == "?" {
			m.ShowHelpManual = true
			return m, nil
		}

		// 6. Global "m" shortcut toggles Markdown syntax & HTML comments display
		if key == "m" {
			m.toggleSyntaxDisplay()
			return m, nil
		}

		// 7. Dispatch according to current mode in stack
		switch m.Mode {
		case ModeReader:
			return m.updateReader(key)
		case ModeBrowser:
			return m.updateBrowser(key)
		}
	}
	return m, nil
}

func (m *AppModel) toggleSyntaxDisplay() {
	m.ShowSyntax = !m.ShowSyntax
	if m.Reader != nil {
		m.Reader.SetShowSyntax(m.ShowSyntax, m.Width, m.Height)
	}
}

func (m *AppModel) openCommandMenu() {
	m.ShowCommandMenu = true
	m.CommandQuery = ""
	m.CommandCursor = 0
	m.CommandScroll = 0
}

// availableCommands returns the list of commands for the current mode, filtered by CommandQuery.
func (m *AppModel) availableCommands() []CommandOption {
	syntaxLabel := "Toggle Markdown Syntax & Comments (Currently: Hidden)"
	if m.ShowSyntax {
		syntaxLabel = "Toggle Markdown Syntax & Comments (Currently: Shown)"
	}

	var all []CommandOption
	if m.Mode == ModeReader && m.Reader != nil {
		sidebarLabel := "Toggle Header Sidebar"
		if m.Reader.SidebarOpen {
			sidebarLabel = "Hide Header Sidebar"
		} else {
			sidebarLabel = "Expand Header Sidebar"
		}
		all = []CommandOption{
			{ID: "toggle_syntax", Label: syntaxLabel, Shortcut: "[m]"},
			{ID: "toggle_sidebar", Label: sidebarLabel, Shortcut: "[t]"},
			{ID: "focus_sidebar", Label: "Switch Focus: Reader / Header Sidebar", Shortcut: "[Tab]"},
			{ID: "scroll_top", Label: "Jump to Top of Document", Shortcut: "[g]"},
			{ID: "scroll_bottom", Label: "Jump to Bottom of Document", Shortcut: "[G]"},
			{ID: "help", Label: "Open Help Manual", Shortcut: "[?]"},
			{ID: "back_files", Label: "Back to File Browser", Shortcut: "[Esc]"},
		}
	} else {
		filterLabel := "Toggle Filter: Markdown Files Only (Currently: ALL)"
		if m.Browser.FilterMD {
			filterLabel = "Toggle Filter: Show All Files (Currently: MD ONLY)"
		}
		all = []CommandOption{
			{ID: "open_selected", Label: "Open Selected Item", Shortcut: "[Enter]"},
			{ID: "toggle_filter", Label: filterLabel, Shortcut: "[f]"},
			{ID: "parent_dir", Label: "Go to Parent Directory", Shortcut: "[Bksp]"},
			{ID: "toggle_syntax", Label: syntaxLabel, Shortcut: "[m]"},
			{ID: "help", Label: "Open Help Manual", Shortcut: "[?]"},
			{ID: "quit_app", Label: "Quit Application", Shortcut: "[Esc]"},
		}
	}

	q := strings.ToLower(strings.TrimSpace(m.CommandQuery))
	if q == "" {
		return all
	}
	var filtered []CommandOption
	for _, cmd := range all {
		if strings.Contains(strings.ToLower(cmd.Label), q) ||
			strings.Contains(strings.ToLower(cmd.Shortcut), q) {
			filtered = append(filtered, cmd)
		}
	}
	return filtered
}

func (m *AppModel) updateCommandMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	cmds := m.availableCommands()

	switch key {
	case "esc", "/":
		// "/" toggles the command popup closed; Esc pops back to previous caller
		if key == "/" && m.CommandQuery != "" {
			// If user hasn't typed anything, "/" closes; if they typed, allow Esc to close
		}
		m.ShowCommandMenu = false
		return m, nil

	case "up", "ctrl+p", "ctrl+k":
		if len(cmds) > 0 {
			m.CommandCursor--
			if m.CommandCursor < 0 {
				m.CommandCursor = 0
			}
		}
		return m, nil

	case "down", "ctrl+n", "ctrl+j":
		if len(cmds) > 0 {
			m.CommandCursor++
			if m.CommandCursor >= len(cmds) {
				m.CommandCursor = len(cmds) - 1
			}
		}
		return m, nil

	case "backspace":
		if len(m.CommandQuery) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.CommandQuery)
			m.CommandQuery = m.CommandQuery[:len(m.CommandQuery)-size]
			m.CommandCursor = 0
			m.CommandScroll = 0
		}
		return m, nil

	case "enter":
		if m.CommandCursor >= 0 && m.CommandCursor < len(cmds) {
			selected := cmds[m.CommandCursor]
			m.ShowCommandMenu = false
			return m.executeCommand(selected.ID)
		}
		m.ShowCommandMenu = false
		return m, nil
	}

	// Type-to-filter: append printable runes
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		m.CommandQuery += string(msg.Runes)
		m.CommandCursor = 0
		m.CommandScroll = 0
		return m, nil
	}
	if msg.Type == tea.KeySpace {
		m.CommandQuery += " "
		m.CommandCursor = 0
		m.CommandScroll = 0
		return m, nil
	}

	return m, nil
}

func (m *AppModel) executeCommand(id string) (tea.Model, tea.Cmd) {
	bodyHeight := m.Height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	switch id {
	case "toggle_syntax":
		m.toggleSyntaxDisplay()
	case "toggle_filter":
		m.Browser.ToggleFilter()
	case "parent_dir":
		_ = m.Browser.GoParent()
	case "open_selected":
		return m.updateBrowser("enter")
	case "toggle_sidebar":
		if m.Reader != nil {
			m.Reader.ToggleSidebar(m.Width, m.Height)
		}
	case "focus_sidebar":
		if m.Reader != nil {
			m.Reader.ToggleFocus(m.Width, m.Height)
		}
	case "scroll_top":
		if m.Reader != nil {
			m.Reader.ScrollToTop()
		}
	case "scroll_bottom":
		if m.Reader != nil {
			m.Reader.ScrollToBottom(bodyHeight)
		}
	case "help":
		m.ShowHelpManual = true
	case "back_files":
		m.Mode = ModeBrowser
		m.Reader = nil
	case "quit_app":
		m.ShowQuitConfirm = true
	}
	return m, nil
}

func (m *AppModel) updateBrowser(key string) (tea.Model, tea.Cmd) {
	bodyHeight := m.Height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	switch key {
	case "esc", "q":
		// Base mode quit triggers the confirmation popup
		m.ShowQuitConfirm = true
		return m, nil

	case "up", "k":
		m.Browser.MoveCursor(-1, bodyHeight)
	case "down", "j":
		m.Browser.MoveCursor(1, bodyHeight)
	case "pgup", "u", "ctrl+u":
		m.Browser.MoveCursor(-bodyHeight/2-1, bodyHeight)
	case "pgdown", "d", "ctrl+d":
		m.Browser.MoveCursor(bodyHeight/2+1, bodyHeight)
	case "home", "g":
		m.Browser.MoveCursor(-len(m.Browser.Entries), bodyHeight)
	case "end", "G":
		m.Browser.MoveCursor(len(m.Browser.Entries), bodyHeight)

	case "f":
		m.Browser.ToggleFilter()

	case "backspace", "h", "left":
		_ = m.Browser.GoParent()

	case "enter", "l", "right":
		entry, ok := m.Browser.Selected()
		if !ok {
			return m, nil
		}
		if entry.IsParent {
			_ = m.Browser.GoParent()
			return m, nil
		}
		if entry.IsDir {
			_ = m.Browser.LoadDir(entry.Path, "")
			return m, nil
		}
		if !entry.Supported {
			m.Browser.StatusMsg = "Unsupported file type: " + entry.Name + " (only Markdown files can be opened)"
			return m, nil
		}
		reader, err := NewReaderState(entry.Path, m.RootDir, m.Width, m.Height, m.ShowSyntax)
		if err != nil {
			m.Browser.StatusMsg = "Error opening file: " + err.Error()
			return m, nil
		}
		m.Reader = reader
		m.Mode = ModeReader
	}

	return m, nil
}

func (m *AppModel) updateReader(key string) (tea.Model, tea.Cmd) {
	if m.Reader == nil {
		m.Mode = ModeBrowser
		return m, nil
	}
	bodyHeight := m.Height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// Quick sidebar expand/collapse key works in both mobile and desktop sizes
	if key == "t" {
		m.Reader.ToggleSidebar(m.Width, m.Height)
		return m, nil
	}

	if key == "tab" {
		m.Reader.ToggleFocus(m.Width, m.Height)
		return m, nil
	}

	// When sidebar is focused (including narrow mobile full-screen overlay mode):
	if m.Reader.SidebarFocused || (m.Reader.IsNarrow && m.Reader.SidebarOpen) {
		switch key {
		case "esc", "q":
			// Pop back from sidebar focus/overlay to the Reader view
			m.Reader.SidebarFocused = false
			if m.Reader.IsNarrow {
				m.Reader.SidebarOpen = false
			}
			return m, nil
		case "up", "k":
			m.Reader.MoveHeaderCursor(-1, bodyHeight)
		case "down", "j":
			m.Reader.MoveHeaderCursor(1, bodyHeight)
		case "pgup", "u":
			m.Reader.MoveHeaderCursor(-bodyHeight/2-1, bodyHeight)
		case "pgdown", "d":
			m.Reader.MoveHeaderCursor(bodyHeight/2+1, bodyHeight)
		case "home", "g":
			m.Reader.MoveHeaderCursor(-len(m.Reader.Rendered.Headers), bodyHeight)
		case "end", "G":
			m.Reader.MoveHeaderCursor(len(m.Reader.Rendered.Headers), bodyHeight)
		case "enter":
			m.Reader.JumpToSelectedHeader(bodyHeight)
		}
		return m, nil
	}

	// Main Reader navigation
	switch key {
	case "esc", "q":
		// Pop from Reader View back to Base Mode (File Browser)
		m.Mode = ModeBrowser
		m.Reader = nil
		return m, nil

	case "up", "k":
		m.Reader.ScrollBy(-1, bodyHeight)
	case "down", "j":
		m.Reader.ScrollBy(1, bodyHeight)
	case "pgup", "b":
		m.Reader.ScrollBy(-bodyHeight+1, bodyHeight)
	case "pgdown", " ", "f":
		m.Reader.ScrollBy(bodyHeight-1, bodyHeight)
	case "u", "ctrl+u":
		m.Reader.ScrollBy(-bodyHeight/2, bodyHeight)
	case "d", "ctrl+d":
		m.Reader.ScrollBy(bodyHeight/2, bodyHeight)
	case "home", "g":
		m.Reader.ScrollToTop()
	case "end", "G":
		m.Reader.ScrollToBottom(bodyHeight)
	}

	return m, nil
}

// View implements tea.Model.
func (m *AppModel) View() string {
	var lines []string
	switch m.Mode {
	case ModeReader:
		if m.Reader != nil {
			lines = m.Reader.RenderLines(m.Width, m.Height)
		} else {
			lines = m.Browser.RenderLines(m.Width, m.Height)
		}
	default:
		lines = m.Browser.RenderLines(m.Width, m.Height)
	}

	if m.ShowCommandMenu {
		lines = m.renderWithCommandMenu(lines)
	} else if m.ShowHelpManual {
		lines = m.renderWithHelpManual(lines)
	} else if m.ShowQuitConfirm {
		lines = m.renderWithQuitPopup(lines)
	}

	return strings.Join(lines, "\n")
}

func (m *AppModel) renderWithCommandMenu(baseLines []string) []string {
	popupW := 62
	if m.Width-4 < popupW {
		popupW = m.Width - 2
	}
	if popupW < 24 {
		popupW = m.Width
	}

	cmds := m.availableCommands()
	if m.CommandCursor >= len(cmds) {
		m.CommandCursor = len(cmds) - 1
	}
	if m.CommandCursor < 0 {
		m.CommandCursor = 0
	}

	maxListRows := m.Height - 8
	if maxListRows < 3 {
		maxListRows = 3
	}
	if maxListRows > 8 {
		maxListRows = 8
	}

	if m.CommandCursor < m.CommandScroll {
		m.CommandScroll = m.CommandCursor
	}
	if m.CommandCursor >= m.CommandScroll+maxListRows {
		m.CommandScroll = m.CommandCursor - maxListRows + 1
	}

	var popupLines []string
	popupLines = append(popupLines, styleBarWithRight(" ⚡ Commands", "[/: Close] ", popupW, bgBar, fgSidebarTitle, fgMuted))
	filterLine := " Filter: " + fgBright + m.CommandQuery + fgActiveCursor + "▎"
	popupLines = append(popupLines, styleLine(filterLine, popupW, bgSidebarHeader, fgMuted))

	if len(cmds) == 0 {
		popupLines = append(popupLines, styleLine("   (no matching commands)", popupW, bgSidebar, fgMuted))
	} else {
		visibleCount := len(cmds)
		if visibleCount > maxListRows {
			visibleCount = maxListRows
		}
		for i := 0; i < visibleCount; i++ {
			idx := m.CommandScroll + i
			if idx >= len(cmds) {
				break
			}
			c := cmds[idx]
			rowBg := bgSidebar
			leftFg := fgDefault
			prefix := "   "
			if idx == m.CommandCursor {
				rowBg = bgSidebarActive
				leftFg = fgActiveCursor
				prefix = " ▸ "
			}
			rightShortcut := ""
			if c.Shortcut != "" {
				rightShortcut = c.Shortcut + " "
			}
			popupLines = append(popupLines, styleBarWithRight(prefix+c.Label, rightShortcut, popupW, rowBg, leftFg, fgFilterBadge))
		}
	}

	popupLines = append(popupLines, styleLine(" ↑/↓: Select   Type: Filter   Enter: Run   Esc: Close", popupW, bgBar, fgMuted))

	overlaid := overlayPopup(baseLines, popupLines, m.Width, popupW, bgMain)
	if len(overlaid) > 0 {
		overlaid[len(overlaid)-1] = styleLine(" Enter: Open   /: Commands   Esc: Quit", m.Width, bgBar, fgMuted)
	}
	return overlaid
}

func (m *AppModel) renderWithHelpManual(baseLines []string) []string {
	popupW := 60
	if m.Width-4 < popupW {
		popupW = m.Width - 2
	}
	if popupW < 24 {
		popupW = m.Width
	}

	helpItems := []struct {
		left  string
		right string
	}{
		{" File Browser", ""},
		{"   Move Cursor", "[↑/↓ / j/k] "},
		{"   Open Folder or Markdown File", "[Enter / l] "},
		{"   Parent Directory", "[Bksp / h] "},
		{"   Toggle Markdown-Only Filter", "[f] "},
		{" Reader View", ""},
		{"   Scroll Line / Page", "[↑/↓ / PgUp/PgDn] "},
		{"   Toggle Markdown Syntax & Comments", "[m] "},
		{"   Toggle Header Sidebar", "[t] "},
		{"   Switch Focus (Reader / Sidebar)", "[Tab] "},
		{"   Commands Palette", "[/] "},
	}

	var popupLines []string
	popupLines = append(popupLines, styleBarWithRight(" 📖 Help Manual", "[Esc: Close] ", popupW, bgBar, fgActiveCursor, fgMuted))
	for _, item := range helpItems {
		if item.right == "" {
			popupLines = append(popupLines, styleLine(item.left, popupW, bgSidebarHeader, fgH3))
		} else {
			popupLines = append(popupLines, styleBarWithRight(item.left, item.right, popupW, bgSidebar, fgDefault, fgFilterBadge))
		}
	}
	popupLines = append(popupLines, styleLine(" Press Esc or Enter to close", popupW, bgBar, fgMuted))

	return overlayPopup(baseLines, popupLines, m.Width, popupW, bgMain)
}

func (m *AppModel) renderWithQuitPopup(baseLines []string) []string {
	popupW := 34
	if m.Width-4 < popupW {
		popupW = m.Width - 2
	}
	if popupW < 20 {
		popupW = m.Width
	}

	popupLines := []string{
		styleLine("", popupW, bgSidebarHeader, fgBright),
		styleLine("       "+ansiBold+fgBright+"Quit Application?", popupW, bgSidebarHeader, fgBright),
		styleLine("", popupW, bgSidebarHeader, fgBright),
		styleLine("     "+fgYes+"[Y] Yes"+ansiReset+bgSidebarHeader+"        "+fgNo+"[N] No", popupW, bgSidebarHeader, fgBright),
		styleLine("", popupW, bgSidebarHeader, fgBright),
	}

	overlaid := overlayPopup(baseLines, popupLines, m.Width, popupW, bgMain)
	if len(overlaid) > 0 {
		overlaid[len(overlaid)-1] = styleLine(" Press Y to confirm quit, or N to cancel", m.Width, bgBar, fgMuted)
	}
	return overlaid
}
