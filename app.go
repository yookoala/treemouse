package main

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// AppMode represents the active view in the mode stack.
type AppMode int

const (
	ModeBrowser AppMode = iota // Base Mode: File Browser
	ModeReader                 // Pushed Mode: Markdown File Reader
)

// AppModel is the root Bubble Tea model managing the mode stack and responsive layout.
type AppModel struct {
	Mode            AppMode
	RootDir         string
	Browser         *BrowserState
	Reader          *ReaderState
	InitialFile     string
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
		reader, err := NewReaderState(absTarget, absCwd, width, height)
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
			case "n":
				m.ShowQuitConfirm = false
				return m, nil
			}
			return m, nil
		}

		// 2. Dispatch according to current mode in stack
		switch m.Mode {
		case ModeReader:
			return m.updateReader(key)
		case ModeBrowser:
			return m.updateBrowser(key)
		}
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
		reader, err := NewReaderState(entry.Path, m.RootDir, m.Width, m.Height)
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

	if m.ShowQuitConfirm {
		lines = m.renderWithQuitPopup(lines)
	}

	return strings.Join(lines, "\n")
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
