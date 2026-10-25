package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

type fileCheckTickMsg time.Time

type editorFinishedMsg struct {
	err error
}

func watchFileTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return fileCheckTickMsg(t)
	})
}

func openEditorCmd(filePath string) tea.Cmd {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("VISUAL"))
	}
	if editor == "" {
		editor = "vi"
	}

	parts := strings.Fields(editor)
	bin := parts[0]
	args := append(parts[1:], filePath)
	c := exec.Command(bin, args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

// AppMode represents the active view in the mode stack.
type AppMode int

const (
	ModeBrowser     AppMode = iota // Base Mode: File Browser
	ModeReader                     // Pushed Mode: Markdown File Reader
	ModeTableViewer                // Pushed Mode: Full-screen Interactive Table Viewer
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
	TableViewer     *TableViewerState
	ShowSyntax      bool   // Whether Markdown syntax & HTML comments are displayed
	AutoReload      bool   // Whether modified files are automatically reloaded (default: false)
	ShowCommandMenu bool   // Whether the "/" Commands popup is open
	CommandQuery    string
	CommandCursor   int
	CommandScroll   int
	ShowHelpManual  bool   // Whether the Help Manual modal is open
	UnsupportedMsg  string // Non-empty when the "Unsupported filetype" popup is open
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
		browser, err := NewBrowserState(absCwd, absCwd)
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
		browser, err := NewBrowserState(absTarget, absTarget)
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

	// Target is a file:
	// - If the file is within the working directory (absCwd), use absCwd as the top-level bound.
	// - If the file is outside the working directory, use its immediate parent directory as the bound.
	parentDir := filepath.Dir(absTarget)
	fileName := filepath.Base(absTarget)
	boundRoot := parentDir
	if isWithinRoot(absTarget, absCwd) {
		boundRoot = absCwd
	}

	browser, err := NewBrowserState(parentDir, boundRoot)
	if err != nil {
		return nil, err
	}
	_ = browser.LoadDir(parentDir, fileName)

	m := &AppModel{
		Mode:    ModeBrowser,
		RootDir: boundRoot,
		Browser: browser,
		Width:   width,
		Height:  height,
	}

	if IsSupportedMarkdown(fileName) {
		reader, err := NewReaderState(absTarget, boundRoot, width, height, m.ShowSyntax, m.AutoReload)
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
	return watchFileTickCmd()
}

// Update implements tea.Model.
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fileCheckTickMsg:
		if m.Mode == ModeReader && m.Reader != nil {
			if m.Reader.CheckFileChanged() && m.AutoReload {
				_ = m.Reader.Reload(m.Width, m.Height)
			}
		}
		return m, watchFileTickCmd()

	case editorFinishedMsg:
		if m.Mode == ModeReader && m.Reader != nil {
			if m.Reader.CheckFileChanged() && m.AutoReload {
				_ = m.Reader.Reload(m.Width, m.Height)
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		if m.Browser != nil {
			m.Browser.OnResize(m.Width, m.Height)
		}
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
		// Y/y or Enter confirms quitting, and N/n or Esc cancels.
		if m.ShowQuitConfirm {
			switch strings.ToLower(key) {
			case "y", "enter":
				return m, tea.Quit
			case "n", "esc":
				m.ShowQuitConfirm = false
				return m, nil
			}
			return m, nil
		}

		// 2. If Unsupported Filetype Popup is open:
		if m.UnsupportedMsg != "" {
			switch key {
			case "esc", "q", "enter", " ":
				m.UnsupportedMsg = ""
			}
			return m, nil
		}

		// 3. If Help Manual Popup is open:
		if m.ShowHelpManual {
			switch key {
			case "esc", "q", "?", "enter":
				m.ShowHelpManual = false
			}
			return m, nil
		}

		// 4. If "/" Commands Popup is open:
		if m.ShowCommandMenu {
			return m.updateCommandMenu(msg)
		}

		// 5. Global "/" shortcut opens the Commands popup from any mode
		if key == "/" {
			m.openCommandMenu()
			return m, nil
		}

		// 6. Global "?" shortcut opens Help Manual
		if key == "?" {
			m.ShowHelpManual = true
			return m, nil
		}

		// 7. Global "m" shortcut toggles Markdown syntax & HTML comments display
		if key == "m" {
			m.toggleSyntaxDisplay()
			return m, nil
		}

		// 8. Global "a" shortcut toggles Auto-Reload mode
		if key == "a" {
			m.toggleAutoReload()
			return m, nil
		}

		// 9. Dispatch according to current mode in stack
		switch m.Mode {
		case ModeTableViewer:
			return m.updateTableViewer(key)
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
	if m.TableViewer != nil {
		m.TableViewer.ShowSyntax = m.ShowSyntax
	}
}

func (m *AppModel) toggleAutoReload() {
	m.AutoReload = !m.AutoReload
	if m.Reader != nil {
		m.Reader.SetAutoReload(m.AutoReload, m.Width, m.Height)
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
	autoReloadLabel := "Toggle Auto-Reload on File Change (Currently: OFF)"
	if m.AutoReload {
		autoReloadLabel = "Toggle Auto-Reload on File Change (Currently: ON)"
	}

	var all []CommandOption
	if m.Mode == ModeTableViewer && m.TableViewer != nil {
		all = []CommandOption{
			{ID: "table_view_cell", Label: "Inspect Full Cell Content Popup", Shortcut: "[Enter / Space]"},
			{ID: "table_open_cell", Label: "Open Link in Active Cell", Shortcut: "[Enter]"},
			{ID: "toggle_syntax", Label: syntaxLabel, Shortcut: "[m]"},
			{ID: "help", Label: "Open Help Manual", Shortcut: "[?]"},
			{ID: "close_table", Label: "Exit Table Viewer Back to Reader", Shortcut: "[Esc]"},
		}
	} else if m.Mode == ModeReader && m.Reader != nil {
		sidebarLabel := "Toggle Header Sidebar"
		if m.Reader.SidebarOpen {
			sidebarLabel = "Hide Header Sidebar"
		} else {
			sidebarLabel = "Expand Header Sidebar"
		}
		selectLabel := "Enter Selection Mode (Links & Tables)"
		if m.Reader.SelectionMode {
			selectLabel = "Exit Selection Mode"
		}
		all = []CommandOption{
			{ID: "toggle_selection", Label: selectLabel, Shortcut: "[s]"},
			{ID: "edit_file", Label: "Edit File in $EDITOR", Shortcut: "[e]"},
			{ID: "reload_file", Label: "Reload Current File from Disk", Shortcut: "[r]"},
			{ID: "toggle_autoreload", Label: autoReloadLabel, Shortcut: "[a]"},
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
		sidebarLabel := "Toggle Item Info Sidebar"
		if m.Browser.SidebarOpen {
			sidebarLabel = "Hide Item Info Sidebar"
		} else {
			sidebarLabel = "Expand Item Info Sidebar"
		}
		all = []CommandOption{
			{ID: "open_selected", Label: "Open Selected Item", Shortcut: "[Enter]"},
			{ID: "toggle_sidebar", Label: sidebarLabel, Shortcut: "[t]"},
			{ID: "focus_sidebar", Label: "Switch Focus: Files / Info Sidebar", Shortcut: "[Tab]"},
			{ID: "toggle_filter", Label: filterLabel, Shortcut: "[f]"},
			{ID: "parent_dir", Label: "Go to Parent Directory", Shortcut: "[Bksp]"},
			{ID: "toggle_autoreload", Label: autoReloadLabel, Shortcut: "[a]"},
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
	case "toggle_selection":
		if m.Reader != nil {
			m.Reader.ToggleSelectionMode(bodyHeight)
		}
	case "table_view_cell":
		if m.Mode == ModeTableViewer && m.TableViewer != nil {
			m.TableViewer.ShowCellPopup = true
			m.TableViewer.CellPopupScroll = 0
		}
	case "table_open_cell":
		if m.Mode == ModeTableViewer && m.TableViewer != nil {
			if _, dest, ok := m.TableViewer.ActiveCellLink(); ok {
				return m.openMarkdownLink(dest)
			}
			m.TableViewer.ShowCellPopup = true
			m.TableViewer.CellPopupScroll = 0
		}
	case "close_table":
		if m.Mode == ModeTableViewer {
			m.Mode = ModeReader
			m.TableViewer = nil
		}
	case "edit_file":
		if m.Reader != nil {
			return m, openEditorCmd(m.Reader.FilePath)
		}
	case "reload_file":
		if m.Reader != nil {
			_ = m.Reader.Reload(m.Width, m.Height)
		}
	case "toggle_autoreload":
		m.toggleAutoReload()
	case "toggle_syntax":
		m.toggleSyntaxDisplay()
	case "toggle_filter":
		m.Browser.ToggleFilter()
	case "parent_dir":
		_ = m.Browser.GoParent()
	case "open_selected":
		return m.updateBrowser("enter")
	case "toggle_sidebar":
		if m.Mode == ModeReader && m.Reader != nil {
			m.Reader.ToggleSidebar(m.Width, m.Height)
		} else if m.Browser != nil {
			m.Browser.ToggleSidebar()
		}
	case "focus_sidebar":
		if m.Mode == ModeReader && m.Reader != nil {
			m.Reader.ToggleFocus(m.Width, m.Height)
		} else if m.Browser != nil {
			m.Browser.ToggleFocus()
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
		m.TableViewer = nil
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

	// Quick sidebar expand/collapse key works in both mobile and desktop sizes
	if key == "t" {
		m.Browser.ToggleSidebar()
		return m, nil
	}

	if key == "tab" {
		m.Browser.ToggleFocus()
		return m, nil
	}

	// When sidebar is focused (or open as mobile full-screen overlay), Esc returns to the file list
	if m.Browser.SidebarFocused || (m.Browser.IsNarrow && m.Browser.SidebarOpen) {
		switch key {
		case "esc", "q":
			m.Browser.SidebarFocused = false
			if m.Browser.IsNarrow {
				m.Browser.SidebarOpen = false
			}
			return m, nil
		}
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
		reader, err := NewReaderState(entry.Path, m.RootDir, m.Width, m.Height, m.ShowSyntax, m.AutoReload)
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

	// Edit in $EDITOR shortcut in Reader View
	if key == "e" {
		return m, openEditorCmd(m.Reader.FilePath)
	}

	// Manual reload shortcut in Reader View
	if key == "r" {
		_ = m.Reader.Reload(m.Width, m.Height)
		return m, nil
	}

	// Toggle Selection Mode in Reader View
	if key == "s" || key == "v" {
		m.Reader.ToggleSelectionMode(bodyHeight)
		return m, nil
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

	// When Selection Mode is active in the Reader:
	if m.Reader.SelectionMode {
		switch key {
		case "esc", "q":
			m.Reader.SelectionMode = false
			m.Reader.StatusMsg = ""
			return m, nil
		case "up", "left", "k", "h":
			m.Reader.StatusMsg = ""
			m.Reader.MoveSelection(-1, bodyHeight)
			return m, nil
		case "down", "right", "j", "l":
			m.Reader.StatusMsg = ""
			m.Reader.MoveSelection(1, bodyHeight)
			return m, nil
		case "home", "g":
			m.Reader.StatusMsg = ""
			m.Reader.MoveSelection(-len(m.Reader.Rendered.Selectables), bodyHeight)
			return m, nil
		case "end", "G":
			m.Reader.StatusMsg = ""
			m.Reader.MoveSelection(len(m.Reader.Rendered.Selectables), bodyHeight)
			return m, nil
		case "pgup", "b":
			m.Reader.ScrollBy(-bodyHeight+1, bodyHeight)
			return m, nil
		case "pgdown", " ", "f":
			m.Reader.ScrollBy(bodyHeight-1, bodyHeight)
			return m, nil
		case "enter":
			return m.activateSelectedItem()
		}
		return m, nil
	}

	// Main Reader navigation
	switch key {
	case "esc", "q", "left":
		m.Reader.StatusMsg = ""
		// If we navigated here via a Markdown link, pop back to the previous document in history
		if m.Reader.PopHistory(m.RootDir, m.Width, m.Height) {
			return m, nil
		}
		// Otherwise pop from Reader View back to Base Mode (File Browser)
		m.Mode = ModeBrowser
		m.Reader = nil
		return m, nil

	case "up", "k":
		m.Reader.StatusMsg = ""
		m.Reader.ScrollBy(-1, bodyHeight)
	case "down", "j":
		m.Reader.StatusMsg = ""
		m.Reader.ScrollBy(1, bodyHeight)
	case "pgup", "b":
		m.Reader.StatusMsg = ""
		m.Reader.ScrollBy(-bodyHeight+1, bodyHeight)
	case "pgdown", " ", "f":
		m.Reader.StatusMsg = ""
		m.Reader.ScrollBy(bodyHeight-1, bodyHeight)
	case "u", "ctrl+u":
		m.Reader.StatusMsg = ""
		m.Reader.ScrollBy(-bodyHeight/2, bodyHeight)
	case "d", "ctrl+d":
		m.Reader.StatusMsg = ""
		m.Reader.ScrollBy(bodyHeight/2, bodyHeight)
	case "home", "g":
		m.Reader.StatusMsg = ""
		m.Reader.ScrollToTop()
	case "end", "G":
		m.Reader.StatusMsg = ""
		m.Reader.ScrollToBottom(bodyHeight)
	}

	return m, nil
}

func (m *AppModel) updateTableViewer(key string) (tea.Model, tea.Cmd) {
	if m.TableViewer == nil || m.Reader == nil {
		m.Mode = ModeReader
		m.TableViewer = nil
		return m, nil
	}
	bodyHeight := m.Height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// When the scrollable Cell Detail Popup is open over the Table Viewer:
	if m.TableViewer.ShowCellPopup {
		switch key {
		case "esc", "q":
			m.TableViewer.ShowCellPopup = false
			m.TableViewer.CellPopupScroll = 0
			return m, nil
		case "up", "k":
			m.TableViewer.ScrollCellPopup(-1, m.Width, m.Height)
			return m, nil
		case "down", "j":
			m.TableViewer.ScrollCellPopup(1, m.Width, m.Height)
			return m, nil
		case "pgup", "u", "b":
			m.TableViewer.ScrollCellPopup(-5, m.Width, m.Height)
			return m, nil
		case "pgdown", "d", " ":
			m.TableViewer.ScrollCellPopup(5, m.Width, m.Height)
			return m, nil
		case "home", "g":
			m.TableViewer.ScrollCellPopup(-10000, m.Width, m.Height)
			return m, nil
		case "end", "G":
			m.TableViewer.ScrollCellPopup(10000, m.Width, m.Height)
			return m, nil
		case "enter":
			if _, dest, ok := m.TableViewer.ActiveCellLink(); ok {
				m.TableViewer.ShowCellPopup = false
				m.TableViewer.CellPopupScroll = 0
				return m.openMarkdownLink(dest)
			}
			m.TableViewer.ShowCellPopup = false
			m.TableViewer.CellPopupScroll = 0
			return m, nil
		}
		return m, nil
	}

	switch key {
	case "esc", "q":
		m.Mode = ModeReader
		m.TableViewer = nil
		return m, nil
	case "up", "k":
		m.TableViewer.MoveRow(-1)
	case "down", "j":
		m.TableViewer.MoveRow(1)
	case "left", "h":
		m.TableViewer.MoveCol(-1)
	case "right", "l":
		m.TableViewer.MoveCol(1)
	case "pgup", "u", "b":
		m.TableViewer.MoveRow(-maxInt(1, bodyHeight/2))
	case "pgdown", "d":
		m.TableViewer.MoveRow(maxInt(1, bodyHeight/2))
	case "home", "g":
		m.TableViewer.MoveRow(-len(m.TableViewer.Table.Rows))
	case "end", "G":
		m.TableViewer.MoveRow(len(m.TableViewer.Table.Rows))
	case " ":
		m.TableViewer.ShowCellPopup = true
		m.TableViewer.CellPopupScroll = 0
	case "enter":
		if _, dest, ok := m.TableViewer.ActiveCellLink(); ok {
			return m.openMarkdownLink(dest)
		}
		m.TableViewer.ShowCellPopup = true
		m.TableViewer.CellPopupScroll = 0
	}
	return m, nil
}

func (m *AppModel) activateSelectedItem() (tea.Model, tea.Cmd) {
	if m.Reader == nil {
		return m, nil
	}
	item, ok := m.Reader.CurrentSelectable()
	if !ok {
		return m, nil
	}
	switch item.Kind {
	case SelectableTable:
		if item.TableIndex >= 0 && item.TableIndex < len(m.Reader.Rendered.Tables) {
			tbl := m.Reader.Rendered.Tables[item.TableIndex]
			m.TableViewer = NewTableViewerState(tbl, filepath.Base(m.Reader.FilePath), m.ShowSyntax)
			m.Mode = ModeTableViewer
		}
		return m, nil
	case SelectableLink:
		return m.openMarkdownLink(item.LinkTarget)
	}
	return m, nil
}

// openMarkdownLink resolves and opens a link destination from either the Reader Selection Mode
// or the Full-Screen Table Viewer:
// 1. In-document anchor (`#heading`) -> jumps to heading in current Reader.
// 2. Supported Markdown file within RootDir -> pushes history and opens in treemouse Reader.
// 3. Directory within RootDir -> opens in File Browser.
// 4. Local text file -> opens in $EDITOR.
// 5. External URL or non-text/out-of-bounds file -> opens with xdg-open if available,
//    or displays the "Unsupported filetype" popup.
func (m *AppModel) openMarkdownLink(rawTarget string) (tea.Model, tea.Cmd) {
	target := strings.TrimSpace(rawTarget)
	if target == "" {
		return m, nil
	}

	bodyHeight := m.Height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// 1. Pure in-document anchor `#heading`
	if strings.HasPrefix(target, "#") {
		if m.Reader != nil {
			m.Mode = ModeReader
			m.TableViewer = nil
			if !m.Reader.JumpToAnchor(target[1:], bodyHeight) {
				m.Reader.StatusMsg = "Anchor not found: " + target
			}
		}
		return m, nil
	}

	// 2. External URL scheme (http://, https://, mailto:, etc.)
	lowerTarget := strings.ToLower(target)
	if strings.HasPrefix(lowerTarget, "http://") ||
		strings.HasPrefix(lowerTarget, "https://") ||
		strings.HasPrefix(lowerTarget, "mailto:") ||
		strings.Contains(target, "://") {
		return m.openWithXdgOrUnsupported(target)
	}

	// Split optional `#anchor` fragment from local file path
	filePart := target
	anchorPart := ""
	if hashIdx := strings.IndexByte(target, '#'); hashIdx >= 0 {
		filePart = target[:hashIdx]
		anchorPart = target[hashIdx+1:]
	}
	filePart = strings.TrimPrefix(filePart, "file://")

	baseDir := m.RootDir
	if m.Reader != nil {
		baseDir = filepath.Dir(m.Reader.FilePath)
	}
	resolved := filePart
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(baseDir, resolved)
	}
	absResolved, err := filepath.Abs(resolved)
	if err == nil {
		resolved = absResolved
	}

	info, statErr := os.Stat(resolved)
	if statErr != nil {
		m.UnsupportedMsg = "File not found: " + filepath.Base(filePart)
		return m, nil
	}

	withinRoot := isWithinRoot(resolved, m.RootDir)

	if info.IsDir() {
		if withinRoot && m.Browser != nil {
			_ = m.Browser.LoadDir(resolved, "")
			m.Mode = ModeBrowser
			m.Reader = nil
			m.TableViewer = nil
			return m, nil
		}
		return m.openWithXdgOrUnsupported(resolved)
	}

	// 3. Supported Markdown file within base directory -> open inside treemouse with history stack
	if withinRoot && IsSupportedMarkdown(resolved) && m.Reader != nil {
		if err := m.Reader.NavigateToFile(resolved, m.RootDir, anchorPart, m.Width, m.Height); err != nil {
			m.UnsupportedMsg = "Cannot open file: " + err.Error()
			return m, nil
		}
		if m.Browser != nil {
			_ = m.Browser.LoadDir(filepath.Dir(resolved), filepath.Base(resolved))
		}
		m.Mode = ModeReader
		m.TableViewer = nil
		return m, nil
	}

	// 4. Local text file -> open with $EDITOR
	if isTextFile(resolved) {
		return m, openEditorCmd(resolved)
	}

	// 5. Fallback: xdg-open if present on system, otherwise "Unsupported filetype" popup
	return m.openWithXdgOrUnsupported(resolved)
}

func (m *AppModel) openWithXdgOrUnsupported(target string) (tea.Model, tea.Cmd) {
	if xdgPath, err := exec.LookPath("xdg-open"); err == nil && xdgPath != "" {
		cmd := exec.Command(xdgPath, target)
		if err := cmd.Start(); err == nil {
			go func() { _ = cmd.Wait() }()
			return m, nil
		}
	}
	m.UnsupportedMsg = "Unsupported filetype: " + filepath.Base(target)
	return m, nil
}

// isTextFile checks whether path is a readable text file (by known text extension, MIME type, or UTF-8 content without NUL bytes).
func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".markdown", ".py", ".go", ".js", ".ts", ".tsx", ".jsx",
		".json", ".toml", ".yaml", ".yml", ".sh", ".bash", ".zsh", ".c", ".cpp",
		".h", ".hpp", ".rs", ".rb", ".java", ".kt", ".css", ".scss", ".html",
		".htm", ".xml", ".csv", ".tsv", ".ini", ".cfg", ".conf", ".log", ".sql",
		".vtt", ".webvtt", ".srt", ".makefile", ".mk", ".dockerfile", ".gitignore":
		return true
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".pdf",
		".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".mp3", ".mp4", ".wav",
		".ogg", ".flac", ".avi", ".mkv", ".mov", ".woff", ".woff2", ".ttf", ".otf",
		".exe", ".dll", ".so", ".dylib", ".bin", ".o", ".a":
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var buf [512]byte
	n, _ := f.Read(buf[:])
	if n == 0 {
		return true // Empty file is editable as text
	}
	sample := buf[:n]
	for _, b := range sample {
		if b == 0 {
			return false
		}
	}
	return utf8.Valid(sample) || strings.HasPrefix(detectFileMIMEType(path), "text/")
}

// View implements tea.Model.
func (m *AppModel) View() string {
	var lines []string
	switch m.Mode {
	case ModeTableViewer:
		if m.TableViewer != nil {
			lines = m.TableViewer.RenderLines(m.Width, m.Height)
		} else if m.Reader != nil {
			lines = m.Reader.RenderLines(m.Width, m.Height)
		} else {
			lines = m.Browser.RenderLines(m.Width, m.Height)
		}
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
	} else if m.UnsupportedMsg != "" {
		lines = m.renderWithUnsupportedPopup(lines)
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
	popupLines = append(popupLines, styleBarWithRight(" ⚡ Commands", "[/ Close] ", popupW, bgBar, fgSidebarTitle, fgMuted))
	filterLine := " Filter " + fgBright + m.CommandQuery + fgActiveCursor + "▎"
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

	popupFooter := formatFooterHints(bgBar,
		HintPair{Key: "↑/↓", Desc: "Select"},
		HintPair{Key: "Type", Desc: "Filter"},
		HintPair{Key: "Enter", Desc: "Run"},
		HintPair{Key: "Esc", Desc: "Close"},
	)
	popupLines = append(popupLines, styleLine(popupFooter, popupW, bgBar, fgMuted))

	overlaid := overlayPopup(baseLines, popupLines, m.Width, popupW, bgMain)
	if len(overlaid) > 0 {
		baseFooter := formatFooterHints(bgBar,
			HintPair{Key: "Enter", Desc: "Open"},
			HintPair{Key: "/", Desc: "Commands"},
			HintPair{Key: "Esc", Desc: "Quit"},
		)
		overlaid[len(overlaid)-1] = styleLine(baseFooter, m.Width, bgBar, fgMuted)
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
		{"   Toggle Item Info Sidebar", "[t] "},
		{"   Toggle Markdown-Only Filter", "[f] "},
		{" Reader View", ""},
		{"   Scroll Line / Page", "[↑/↓ / PgUp/PgDn] "},
		{"   Toggle Selection Mode (Links/Tables)", "[s] "},
		{"   Open Focused Link or Table", "[Enter] "},
		{"   Edit File in $EDITOR", "[e] "},
		{"   Reload Modified File", "[r] "},
		{"   Toggle Auto-Reload on Change", "[a] "},
		{"   Toggle Markdown Syntax & Comments", "[m] "},
		{"   Toggle Header Sidebar", "[t] "},
		{"   Switch Focus (Reader / Sidebar)", "[Tab] "},
		{"   Commands Palette", "[/] "},
	}

	var popupLines []string
	popupLines = append(popupLines, styleBarWithRight(" 📖 Help Manual", "[Esc Close] ", popupW, bgBar, fgActiveCursor, fgMuted))
	for _, item := range helpItems {
		if item.right == "" {
			popupLines = append(popupLines, styleLine(item.left, popupW, bgSidebarHeader, fgH3))
		} else {
			popupLines = append(popupLines, styleBarWithRight(item.left, item.right, popupW, bgSidebar, fgDefault, fgFilterBadge))
		}
	}
	helpFooter := formatFooterHints(bgBar,
		HintPair{Key: "Enter", Desc: "Close"},
		HintPair{Key: "Esc", Desc: "Close"},
	)
	popupLines = append(popupLines, styleLine(helpFooter, popupW, bgBar, fgMuted))

	return overlayPopup(baseLines, popupLines, m.Width, popupW, bgMain)
}

func (m *AppModel) renderWithUnsupportedPopup(baseLines []string) []string {
	popupW := 46
	if m.Width-4 < popupW {
		popupW = m.Width - 2
	}
	if popupW < 22 {
		popupW = m.Width
	}

	popupLines := []string{
		styleBarWithRight(" ⚠ Unsupported Filetype", "[Esc Close] ", popupW, bgBar, fgWarn, fgMuted),
		styleLine("", popupW, bgSidebarHeader, fgBright),
		styleLine("  "+fgBright+m.UnsupportedMsg, popupW, bgSidebarHeader, fgBright),
		styleLine("", popupW, bgSidebarHeader, fgBright),
		styleLine(formatFooterHints(bgBar,
			HintPair{Key: "Enter", Desc: "OK"},
			HintPair{Key: "Esc", Desc: "Close"},
		), popupW, bgBar, fgMuted),
	}

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
		quitFooter := formatFooterHints(bgBar,
			HintPair{Key: "Y", Desc: "Confirm Quit"},
			HintPair{Key: "N", Desc: "Cancel"},
		)
		overlaid[len(overlaid)-1] = styleLine(quitFooter, m.Width, bgBar, fgMuted)
	}
	return overlaid
}
