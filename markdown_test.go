package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMarkdownANSIStylingAndLinks(t *testing.T) {
	src := strings.Join([]string{
		"# H1 Heading",
		"## H2 Heading",
		"### H3 Heading",
		"",
		"Paragraph with **bold**, *italic*, ***both***, <u>underlined</u>, and ~~strike~~.",
		"Check [Project Guide](https://example.com/guide \"Main Title\") for details.",
	}, "\n")

	doc := RenderMarkdown(src, 80)

	if len(doc.Headers) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(doc.Headers))
	}
	if doc.Headers[0].Level != 1 || doc.Headers[0].Title != "H1 Heading" {
		t.Errorf("unexpected H1 header: %+v", doc.Headers[0])
	}
	if doc.Headers[1].Level != 2 || doc.Headers[1].Title != "H2 Heading" {
		t.Errorf("unexpected H2 header: %+v", doc.Headers[1])
	}
	if doc.Headers[2].Level != 3 || doc.Headers[2].Title != "H3 Heading" {
		t.Errorf("unexpected H3 header: %+v", doc.Headers[2])
	}

	// Verify distinct header colors
	if !strings.Contains(doc.Lines[doc.Headers[0].RenderLine], fgH1) {
		t.Errorf("H1 line missing H1 color sequence")
	}
	if !strings.Contains(doc.Lines[doc.Headers[1].RenderLine], fgH2) {
		t.Errorf("H2 line missing H2 color sequence")
	}
	if !strings.Contains(doc.Lines[doc.Headers[2].RenderLine], fgH3) {
		t.Errorf("H3 line missing H3 color sequence")
	}

	fullRendered := strings.Join(doc.Lines, "\n")

	// Verify bold, italic, underline, strikethrough ANSI codes
	if !strings.Contains(fullRendered, ansiBold+"bold") {
		t.Errorf("expected ANSI bold for **bold**")
	}
	if !strings.Contains(fullRendered, ansiItalic+"italic") {
		t.Errorf("expected ANSI italic for *italic*")
	}
	if !strings.Contains(fullRendered, ansiBold+ansiItalic+"both") {
		t.Errorf("expected ANSI bold+italic for ***both***")
	}
	if !strings.Contains(fullRendered, ansiUnderline+"underlined") {
		t.Errorf("expected ANSI underline for <u>underlined</u>")
	}
	if !strings.Contains(fullRendered, ansiStrike+"strike") {
		t.Errorf("expected ANSI strikethrough for ~~strike~~")
	}

	// Verify link preserves markdown brackets, URL, and quotes and has ANSI underline
	expectedRawLink := `[Project Guide](https://example.com/guide "Main Title")`
	if !strings.Contains(fullRendered, expectedRawLink) {
		t.Errorf("expected link to preserve raw markdown syntax %q, got:\n%s", expectedRawLink, fullRendered)
	}
	if !strings.Contains(fullRendered, fgLink+ansiUnderline+expectedRawLink) {
		t.Errorf("expected link to have ANSI underline and link color")
	}
}

func TestCJKWrappingAndExactWidth(t *testing.T) {
	src := "# 孔乙己\n\n魯鎮的酒店的格局，是和別處不同的：都是當街一個曲尺形的大櫃檯，櫃裡面預備著熱水，可以隨時溫酒。"
	targetWidth := 36
	doc := RenderMarkdown(src, targetWidth)

	for i, line := range doc.Lines {
		vw := visibleWidth(line)
		if vw != targetWidth {
			t.Errorf("line %d visibleWidth = %d, want %d", i, vw, targetWidth)
		}
	}
	if len(doc.Lines) < 3 {
		t.Errorf("expected CJK paragraph to wrap across multiple lines at width %d, got %d lines", targetWidth, len(doc.Lines))
	}
}

func TestFileBrowserFilterAndNavigation(t *testing.T) {
	tmp := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmp, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(tmp, "readme.md"), []byte("# Hello"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "script.py"), []byte("print(1)"), 0o644)

	b, err := NewBrowserState(tmp)
	if err != nil {
		t.Fatalf("NewBrowserState failed: %v", err)
	}

	// Default mode is MD ONLY (FilterMD == true): at top-level bound (tmp), ".." should NOT be shown,
	// and only folders and supported markdown files ("sub", "readme.md") should be displayed.
	if !b.FilterMD {
		t.Fatalf("expected FilterMD to default to true")
	}
	if len(b.Entries) != 2 {
		t.Fatalf("expected 2 entries in default MD ONLY mode (sub, readme.md), got %d", len(b.Entries))
	}
	if !b.Entries[0].IsDir || b.Entries[0].Name != "sub" {
		t.Errorf("expected first entry to be directory sub, got %+v", b.Entries[0])
	}
	if !b.Entries[1].Supported || b.Entries[1].Name != "readme.md" {
		t.Errorf("expected second entry to be supported readme.md, got %+v", b.Entries[1])
	}

	// Toggle filter to ALL mode (FilterMD == false)
	b.ToggleFilter()
	if b.FilterMD {
		t.Errorf("expected FilterMD to be false after toggle")
	}
	if len(b.Entries) != 3 {
		t.Fatalf("expected 3 entries in ALL mode at top-level bound, got %d", len(b.Entries))
	}
	if !b.Entries[1].Supported {
		t.Errorf("expected readme.md to be supported")
	}
	if b.Entries[2].Supported {
		t.Errorf("expected script.py to be unsupported")
	}

	// Unsupported item can still be highlighted by cursor navigation in ALL mode
	b.MoveCursor(2, 20)
	sel, ok := b.Selected()
	if !ok || sel.Name != "script.py" {
		t.Errorf("expected cursor to highlight unsupported file script.py, got %+v", sel)
	}

	// Toggle filter back to MD ONLY
	b.ToggleFilter()
	if !b.FilterMD {
		t.Errorf("expected FilterMD to be true")
	}
	if len(b.Entries) != 2 {
		t.Fatalf("expected 2 entries in MD ONLY mode (sub, readme.md), got %d", len(b.Entries))
	}
}

func TestModeStackAndQuitConfirmation(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "story.md")
	_ = os.WriteFile(mdPath, []byte("# Chapter 1\n\nSome text.\n\n## Section 1.1\nMore text."), 0o644)

	// Launching directly with a file argument should set Browser as base mode and open Reader on top
	app, err := NewAppModel(mdPath)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}
	if app.Mode != ModeReader {
		t.Fatalf("expected initial mode ModeReader when file arg given, got %v", app.Mode)
	}

	// Resize to narrow mobile screen (< 80 cols): sidebar should auto-collapse
	app.Update(tea.WindowSizeMsg{Width: 45, Height: 20})
	if !app.Reader.IsNarrow || app.Reader.SidebarOpen {
		t.Errorf("expected sidebar to auto-collapse on narrow screen, got IsNarrow=%v SidebarOpen=%v", app.Reader.IsNarrow, app.Reader.SidebarOpen)
	}

	// Press 't' on mobile screen to expand sidebar as full-screen overlay
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if !app.Reader.SidebarOpen || !app.Reader.SidebarFocused {
		t.Errorf("expected 't' to open and focus sidebar overlay on mobile")
	}

	// Press Esc from mobile sidebar overlay -> returns to Reader view (does not exit Reader yet)
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.Mode != ModeReader || app.Reader.SidebarOpen {
		t.Errorf("expected Esc to close mobile sidebar overlay and stay in ModeReader")
	}

	// Press Left arrow in Reader view -> pops back to Base Mode (File Browser)
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if app.Mode != ModeBrowser {
		t.Errorf("expected Left arrow from Reader to return to ModeBrowser, got %v", app.Mode)
	}

	// Re-open the file with Right arrow, then press Esc in Reader view -> pops back to Base Mode (File Browser)
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.Mode != ModeReader {
		t.Fatalf("expected Right arrow on selected file to open ModeReader")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.Mode != ModeBrowser {
		t.Errorf("expected Esc from Reader to return to ModeBrowser, got %v", app.Mode)
	}

	// Press Esc in Base Mode (File Browser) -> opens Quit Confirmation Popup
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !app.ShowQuitConfirm {
		t.Errorf("expected Esc in ModeBrowser to show Quit Confirmation Popup")
	}

	// Press 'n' in Quit Confirmation Popup -> cancels and stays in File Browser
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if app.ShowQuitConfirm || cmd != nil {
		t.Errorf("expected 'n' to cancel Quit Confirmation Popup without quitting")
	}

	// Press Esc again, then 'Esc' -> cancels quit popup
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.ShowQuitConfirm || cmd != nil {
		t.Errorf("expected 'Esc' to cancel Quit Confirmation Popup without quitting")
	}

	// Press Esc again, then 'Enter' -> confirms quit
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Errorf("expected 'Enter' in Quit Confirmation Popup to return tea.Quit cmd")
	}

	// Re-open popup and press 'Y' -> confirms quit
	app.ShowQuitConfirm = true
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
	if cmd == nil {
		t.Errorf("expected 'Y' in Quit Confirmation Popup to return tea.Quit cmd")
	}
}

func TestNaturalSortingOrder(t *testing.T) {
	tmp := t.TempDir()
	names := []string{
		"chapter-10-1.md",
		"chapter-8b.md",
		"chapter-9.md",
		"chapter-8.md",
		"chapter-9-2.md",
		"chapter-10.md",
		"chapter-8a.md",
		"chapter-9-1.md",
	}
	for _, n := range names {
		_ = os.WriteFile(filepath.Join(tmp, n), []byte("# "+n), 0o644)
	}

	dirs := []string{
		"vol-10",
		"vol-2",
		"vol-1",
		"vol-2a",
	}
	for _, d := range dirs {
		_ = os.Mkdir(filepath.Join(tmp, d), 0o755)
	}

	b, err := NewBrowserState(tmp)
	if err != nil {
		t.Fatalf("NewBrowserState failed: %v", err)
	}

	// Entries[0] is "..", Entries[1..4] are dirs, Entries[5..] are files
	var gotDirs []string
	var gotFiles []string
	for _, e := range b.Entries {
		if e.IsParent {
			continue
		}
		if e.IsDir {
			gotDirs = append(gotDirs, e.Name)
		} else {
			gotFiles = append(gotFiles, e.Name)
		}
	}

	wantDirs := []string{"vol-1", "vol-2", "vol-2a", "vol-10"}
	if strings.Join(gotDirs, ",") != strings.Join(wantDirs, ",") {
		t.Errorf("dirs natural sort = %v, want %v", gotDirs, wantDirs)
	}

	wantFiles := []string{
		"chapter-8.md",
		"chapter-8a.md",
		"chapter-8b.md",
		"chapter-9.md",
		"chapter-9-1.md",
		"chapter-9-2.md",
		"chapter-10.md",
		"chapter-10-1.md",
	}
	if strings.Join(gotFiles, ",") != strings.Join(wantFiles, ",") {
		t.Errorf("files natural sort = %v, want %v", gotFiles, wantFiles)
	}
}

func TestMarkdownSyntaxAndHTMLCommentsToggle(t *testing.T) {
	src := strings.Join([]string{
		"<!-- Top secret comment -->",
		"# Heading One",
		"<!--",
		"Multi-line hidden note",
		"-->",
		"Text with **bold** and `code` and <!-- inline note --> [Link](https://example.com).",
	}, "\n")

	// Default (showSyntax = false): syntax markers and HTML comments are hidden, link URL stays
	hiddenDoc := RenderMarkdown(src, 80, false)
	hiddenOut := strings.Join(hiddenDoc.Lines, "\n")

	if strings.Contains(hiddenOut, "Top secret comment") ||
		strings.Contains(hiddenOut, "Multi-line hidden note") ||
		strings.Contains(hiddenOut, "inline note") {
		t.Errorf("expected HTML comments to be hidden by default, got:\n%s", hiddenOut)
	}
	if strings.Contains(hiddenOut, "# Heading") {
		t.Errorf("expected '#' prefix to be hidden by default, got:\n%s", hiddenOut)
	}
	if strings.Contains(hiddenOut, "**bold**") || strings.Contains(hiddenOut, "`code`") {
		t.Errorf("expected inline '**' and '`' syntax markers to be hidden by default")
	}
	if !strings.Contains(hiddenOut, "[Link](https://example.com)") {
		t.Errorf("expected link URL and brackets to remain visible by default")
	}

	// Toggled on (showSyntax = true): syntax markers and HTML comments are displayed
	shownDoc := RenderMarkdown(src, 80, true)
	shownOut := strings.Join(shownDoc.Lines, "\n")

	if !strings.Contains(shownOut, "<!-- Top secret comment -->") ||
		!strings.Contains(shownOut, "Multi-line hidden note") ||
		!strings.Contains(shownOut, "<!-- inline note -->") {
		t.Errorf("expected HTML comments to be visible when showSyntax=true, got:\n%s", shownOut)
	}
	if !strings.Contains(shownOut, "#") || !strings.Contains(shownOut, "**bold**") || !strings.Contains(shownOut, "`code`") {
		t.Errorf("expected Markdown syntax markers (#, **, `) to be visible when showSyntax=true, got:\n%s", shownOut)
	}
}

func TestCommandPalettePopupAndFooterHints(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "doc.md")
	_ = os.WriteFile(mdPath, []byte("# Title\n<!-- comment -->\nHello **world**"), 0o644)

	app, err := NewAppModel(mdPath)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Check minimal footer and top-right × in Reader view (without colons or Search hint)
	view := app.View()
	viewLines := strings.Split(view, "\n")
	if !strings.Contains(viewLines[0], "×") {
		t.Errorf("expected top-right of reading area to show '×', got:\n%s", viewLines[0])
	}
	footerLine := viewLines[len(viewLines)-1]
	if !strings.Contains(footerLine, "Commands") || !strings.Contains(footerLine, "Close") {
		t.Errorf("expected minimal footer with 'Commands' and 'Esc Close', got:\n%s", footerLine)
	}
	if strings.Contains(view, "Search") {
		t.Errorf("expected 'Search' hint not to appear in footer bar, got:\n%s", view)
	}
	if strings.Contains(view, "/:") || strings.Contains(view, "Esc:") {
		t.Errorf("expected footer key hints to not contain ':', got:\n%s", view)
	}
	if strings.Contains(view, "PgUp/PgDn") || strings.Contains(view, "Toggle Sidebar") {
		t.Errorf("expected secondary features to be hidden from bottom footer")
	}

	// Open Commands popup with ":"
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	if !app.ShowCommandMenu {
		t.Fatalf("expected ':' to open Command Menu popup")
	}

	// Filter commands by typing "syntax"
	for _, r := range "syntax" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	cmds := app.availableCommands()
	if len(cmds) != 1 || cmds[0].ID != "toggle_syntax" {
		t.Fatalf("expected filtering 'syntax' to match toggle_syntax, got %+v", cmds)
	}

	// Press Enter to execute the command -> toggles ShowSyntax on and closes Command Menu
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.ShowCommandMenu {
		t.Errorf("expected Enter to close Command Menu after executing option")
	}
	if !app.ShowSyntax || !app.Reader.ShowSyntax {
		t.Errorf("expected toggle_syntax command to enable ShowSyntax")
	}

	// Test "quit" / "q" command from Reader view
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !app.ShowQuitConfirm {
		t.Fatalf("expected ':q' command in Reader mode to show Quit Confirmation popup")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if app.ShowQuitConfirm {
		t.Fatalf("expected 'n' to cancel Quit Confirmation popup")
	}

	// Test ":quit" command in File Browser mode and check ":" vs "/" trigger consistency
	app.Mode = ModeBrowser
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if app.ShowCommandMenu {
		t.Fatalf("expected '/' not to open Command Menu in File Browser (only ':' should open it)")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	if !app.ShowCommandMenu {
		t.Fatalf("expected ':' to open Command Menu in File Browser")
	}
	for _, r := range "quit" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !app.ShowQuitConfirm {
		t.Fatalf("expected ':quit' command to show Quit Confirmation popup")
	}
	_, quitCmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if quitCmd == nil {
		t.Fatalf("expected confirming Quit Confirmation popup to quit the application")
	}
}

func TestContextualStickyHeaders(t *testing.T) {
	var lines []string
	lines = append(lines, "# Book One")
	lines = append(lines, "Intro line 1")
	lines = append(lines, "Intro line 2")
	lines = append(lines, "## Chapter Alpha")
	lines = append(lines, "Alpha line 1")
	lines = append(lines, "Alpha line 2")
	lines = append(lines, "### Scene 1")
	for i := 0; i < 30; i++ {
		lines = append(lines, "Body paragraph line")
	}
	lines = append(lines, "## Chapter Beta")
	for i := 0; i < 30; i++ {
		lines = append(lines, "Beta paragraph line")
	}

	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "book.md")
	_ = os.WriteFile(mdPath, []byte(strings.Join(lines, "\n")), 0o644)

	r, err := NewReaderState(mdPath, tmp, 80, 20, false)
	if err != nil {
		t.Fatalf("NewReaderState failed: %v", err)
	}

	// Scroll into Scene 1 body (past H1, H2, H3)
	r.Scroll = 10
	ctx := r.ActiveContextHeaders(18)
	if len(ctx) != 3 {
		t.Fatalf("expected 3 contextual headers (H1, H2, H3), got %d: %+v", len(ctx), ctx)
	}
	if ctx[0].Title != "Book One" || ctx[1].Title != "Chapter Alpha" || ctx[2].Title != "Scene 1" {
		t.Errorf("unexpected contextual hierarchy: %+v", ctx)
	}

	// Verify rendered view shows sticky context rows with bgContextBar
	renderedLines := r.RenderLines(80, 20)
	// Row 0 is top bar, Rows 1..3 should be sticky context headers with bgContextBar
	for i := 1; i <= 3; i++ {
		if !strings.Contains(renderedLines[i], bgContextBar) {
			t.Errorf("expected row %d to have sticky header background bgContextBar", i)
		}
	}

	// Scroll into Chapter Beta (H2): should pop Chapter Alpha & Scene 1, leaving [Book One, Chapter Beta]
	betaLine := r.Rendered.Headers[3].RenderLine
	r.Scroll = betaLine + 5
	ctxBeta := r.ActiveContextHeaders(18)
	if len(ctxBeta) != 2 {
		t.Fatalf("expected 2 contextual headers (H1 Book One, H2 Chapter Beta), got %d: %+v", len(ctxBeta), ctxBeta)
	}
	if ctxBeta[0].Title != "Book One" || ctxBeta[1].Title != "Chapter Beta" {
		t.Errorf("unexpected contextual hierarchy under Chapter Beta: %+v", ctxBeta)
	}
}

func TestFileChangeDetectionAndAutoReload(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "live.md")
	_ = os.WriteFile(mdPath, []byte("# Version 1\n\nInitial text."), 0o644)

	app, err := NewAppModel(mdPath)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	// Default auto-reload should be off
	if app.AutoReload || app.Reader.AutoReload {
		t.Fatalf("expected AutoReload to default to false")
	}

	// Modify file on disk
	_ = os.WriteFile(mdPath, []byte("# Version 2\n\nUpdated text content."), 0o644)

	// Trigger file check tick while AutoReload is off
	app.Update(fileCheckTickMsg{})
	if !app.Reader.FileModified {
		t.Fatalf("expected FileModified to be true after disk write")
	}

	// View should still show Version 1 and display the red "r reload" hint at the right end of the footer bar
	view := app.View()
	viewLines := strings.Split(view, "\n")
	footerLine := viewLines[len(viewLines)-1]
	if !strings.Contains(view, "Version 1") {
		t.Errorf("expected view not to auto-reload while AutoReload is off")
	}
	if !strings.Contains(footerLine, fgReloadKey+"↻ r") || !strings.Contains(footerLine, fgReloadDesc+"reload") {
		t.Errorf("expected bright red 'r' and darker red 'reload' hint in footer bar, got:\n%s", footerLine)
	}

	// Press 'r' to manually reload
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if app.Reader.FileModified {
		t.Errorf("expected FileModified to be cleared after manual reload")
	}
	viewAfterReload := app.View()
	if !strings.Contains(viewAfterReload, "Version 2") {
		t.Errorf("expected view to contain 'Version 2' after pressing 'r'")
	}

	// Toggle AutoReload ON with 'a', then modify file again
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !app.AutoReload || !app.Reader.AutoReload {
		t.Fatalf("expected 'a' to enable AutoReload")
	}
	_ = os.WriteFile(mdPath, []byte("# Version 3\n\nAuto reloaded text."), 0o644)
	app.Update(fileCheckTickMsg{})

	if app.Reader.FileModified {
		t.Errorf("expected FileModified to be false after auto-reload")
	}
	if !strings.Contains(app.View(), "Version 3") || !strings.Contains(app.View(), "Auto Reload") {
		t.Errorf("expected view to automatically update to 'Version 3' and show 'Auto Reload' when AutoReload is on")
	}
}

func TestBoundedFileBrowsing(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	// Create workspace tree:
	// root/
	//   work/
	//     nested/
	//       inside.md
	//   outside/
	//     external.md
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	nestedDir := filepath.Join(workDir, "nested")
	outsideDir := filepath.Join(root, "outside")
	_ = os.MkdirAll(nestedDir, 0o755)
	_ = os.MkdirAll(outsideDir, 0o755)

	insideFile := filepath.Join(nestedDir, "inside.md")
	externalFile := filepath.Join(outsideDir, "external.md")
	_ = os.WriteFile(insideFile, []byte("# Inside"), 0o644)
	_ = os.WriteFile(externalFile, []byte("# External"), 0o644)

	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	// Case 1: No argument -> bounded to working directory (workDir)
	app1, err := NewAppModel("")
	if err != nil {
		t.Fatalf("NewAppModel(''): %v", err)
	}
	if app1.Browser.RootDir != workDir {
		t.Errorf("Case 1 RootDir = %q, want %q", app1.Browser.RootDir, workDir)
	}
	if app1.Browser.CanGoParent() {
		t.Errorf("Case 1 expected CanGoParent() == false at top-level working directory")
	}

	// Case 2: Folder specified -> bounded to specified folder (nestedDir)
	app2, err := NewAppModel(nestedDir)
	if err != nil {
		t.Fatalf("NewAppModel(nestedDir): %v", err)
	}
	if app2.Browser.RootDir != nestedDir {
		t.Errorf("Case 2 RootDir = %q, want %q", app2.Browser.RootDir, nestedDir)
	}
	_ = app2.Browser.GoParent()
	if app2.Browser.Dir != nestedDir {
		t.Errorf("Case 2 expected GoParent() to stay at bound %q, got %q", nestedDir, app2.Browser.Dir)
	}

	// Case 3: File inside working directory -> bounded to working directory (workDir)
	app3, err := NewAppModel(insideFile)
	if err != nil {
		t.Fatalf("NewAppModel(insideFile): %v", err)
	}
	if app3.Browser.RootDir != workDir {
		t.Errorf("Case 3 RootDir = %q, want %q", app3.Browser.RootDir, workDir)
	}
	if !app3.Browser.CanGoParent() {
		t.Errorf("Case 3 expected CanGoParent() == true from nestedDir up to workDir")
	}
	_ = app3.Browser.GoParent()
	if app3.Browser.Dir != workDir {
		t.Errorf("Case 3 expected GoParent() to reach workDir %q, got %q", workDir, app3.Browser.Dir)
	}
	if app3.Browser.CanGoParent() {
		t.Errorf("Case 3 expected CanGoParent() == false once at workDir bound")
	}

	// Case 4: File outside working directory -> bounded to immediate parent of file (outsideDir)
	app4, err := NewAppModel(externalFile)
	if err != nil {
		t.Fatalf("NewAppModel(externalFile): %v", err)
	}
	if app4.Browser.RootDir != outsideDir {
		t.Errorf("Case 4 RootDir = %q, want %q", app4.Browser.RootDir, outsideDir)
	}
	if app4.Browser.CanGoParent() {
		t.Errorf("Case 4 expected CanGoParent() == false at immediate parent bound outsideDir")
	}
}

func TestBrowserInfoSidebarAndResponsiveBehavior(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "sample.md")
	_ = os.WriteFile(mdPath, []byte("# Title\n\nSome markdown content."), 0o644)

	app, err := NewAppModel(tmp)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}

	// Wide screen (>= 80 cols): Browser Info Sidebar should be open by default
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if app.Browser.IsNarrow || !app.Browser.SidebarOpen {
		t.Fatalf("expected Browser Info Sidebar to be open on wide screen")
	}

	// At top-level bound, "sample.md" is at index 0
	view := app.View()
	for _, expected := range []string{"Item Info", "MIME Type", "text/markdown", "Permissions", "Created", "Modified"} {
		if !strings.Contains(view, expected) {
			t.Errorf("expected wide browser sidebar to contain %q, got:\n%s", expected, view)
		}
	}

	// Resize to narrow mobile screen (< 80 cols): Browser Info Sidebar should auto-collapse
	app.Update(tea.WindowSizeMsg{Width: 45, Height: 20})
	if !app.Browser.IsNarrow || app.Browser.SidebarOpen {
		t.Errorf("expected Browser Info Sidebar to auto-collapse on narrow screen")
	}

	// Press 't' on narrow screen: expands full-screen Item Info overlay
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if !app.Browser.SidebarOpen || !app.Browser.SidebarFocused {
		t.Errorf("expected 't' to open and focus Browser Info Sidebar overlay on narrow screen")
	}
	overlayView := app.View()
	if !strings.Contains(overlayView, "[Overlay]") || !strings.Contains(overlayView, "text/markdown") {
		t.Errorf("expected mobile overlay to show Item Info and MIME type, got:\n%s", overlayView)
	}

	// Press Esc from mobile overlay -> closes overlay and returns to file list (without opening Quit popup yet)
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.Browser.SidebarOpen || app.ShowQuitConfirm {
		t.Errorf("expected Esc from mobile info overlay to return to file browser without triggering quit popup")
	}
}

func TestReaderEditCommand(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "editme.md")
	_ = os.WriteFile(mdPath, []byte("# Before Edit"), 0o644)

	t.Setenv("EDITOR", "true")

	app, err := NewAppModel(mdPath)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}

	// Verify "Edit File in $EDITOR" is in the "/" Command Palette
	cmds := app.availableCommands()
	foundEdit := false
	for _, c := range cmds {
		if c.ID == "edit_file" && c.Shortcut == "[e]" {
			foundEdit = true
			break
		}
	}
	if !foundEdit {
		t.Errorf("expected edit_file command in Reader command palette, got %+v", cmds)
	}

	// Pressing 'e' in Reader returns a non-nil ExecProcess Tea command and stays in ModeReader
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatalf("expected pressing 'e' in Reader to return tea.ExecProcess cmd")
	}
	if app.Mode != ModeReader {
		t.Errorf("expected app to remain in ModeReader during/after editor invocation, got %v", app.Mode)
	}

	// Simulate editor modifying the file and terminating (editorFinishedMsg)
	_ = os.WriteFile(mdPath, []byte("# After Edit"), 0o644)
	app.Update(editorFinishedMsg{err: nil})
	if !app.Reader.FileModified {
		t.Errorf("expected Reader to detect file modification immediately when editor exits")
	}
}

func TestCascadingEmptyLineCollapseAroundHiddenComments(t *testing.T) {
	// 7 lines total:
	// 0: "Paragraph A"
	// 1: "" (empty)
	// 2: "<!-- comment 1 -->"
	// 3: "" (empty)
	// 4: "<!-- multi-line\ncomment 2 -->"
	// 5: "" (empty)
	// 6: "Paragraph B"
	src := strings.Join([]string{
		"Paragraph A",
		"",
		"<!-- comment 1 -->",
		"",
		"<!-- multi-line",
		"comment 2 -->",
		"",
		"Paragraph B",
	}, "\n")

	doc := RenderMarkdown(src, 80, false)
	// All 5 middle lines (empty, comment, empty, comment, empty) should collapse into a single empty line,
	// resulting in 3 lines total: "Paragraph A", "", "Paragraph B".
	if len(doc.Lines) != 3 {
		t.Fatalf("expected 3 rendered lines after cascading empty-line collapse, got %d:\n%s", len(doc.Lines), strings.Join(doc.Lines, "\n"))
	}
	if !strings.Contains(doc.Lines[0], "Paragraph A") || !strings.Contains(doc.Lines[2], "Paragraph B") {
		t.Errorf("unexpected rendered lines: %+v", doc.Lines)
	}
}

func TestHorizontalRuleRendering(t *testing.T) {
	src := strings.Join([]string{
		"Above",
		"",
		"---",
		"***",
		"___",
		"- - -",
		"",
		"Below",
	}, "\n")

	width := 50
	doc := RenderMarkdown(src, width, false)
	if len(doc.Lines) != 8 {
		t.Fatalf("expected 8 lines, got %d", len(doc.Lines))
	}

	expectedBar := strings.Repeat("─", width-2)
	for _, idx := range []int{2, 3, 4, 5} {
		line := doc.Lines[idx]
		if !strings.Contains(line, expectedBar) {
			t.Errorf("line %d expected full-width horizontal rule of %d '─' chars, got: %q", idx, width-2, line)
		}
		if strings.Contains(line, "…") {
			t.Errorf("line %d horizontal rule should not be truncated with ellipsis: %q", idx, line)
		}
		if visibleWidth(line) != width {
			t.Errorf("line %d visibleWidth = %d, want %d", idx, visibleWidth(line), width)
		}
	}

	// Ensure a file starting with "---" followed by normal Markdown (not YAML frontmatter) keeps its horizontal rules
	nonYAML := strings.Join([]string{
		"---",
		"",
		"# Heading: With Colon",
		"",
		"---",
		"",
		"　　---", // Full-width CJK space indented HR
	}, "\n")
	docNonYAML := RenderMarkdown(nonYAML, width, false)
	hrCount := 0
	for _, l := range docNonYAML.Lines {
		if strings.Contains(l, expectedBar) {
			hrCount++
		}
	}
	if hrCount != 3 {
		t.Errorf("expected 3 rendered horizontal rules in non-YAML document starting with '---', got %d", hrCount)
	}
}

func TestRelativeHeaderIndentation(t *testing.T) {
	// File with no H1, only H2 and H3: H2 should have Depth 0 (unindented), H3 should have Depth 1 (indented once)
	var lines []string
	lines = append(lines, "## Section Two")
	lines = append(lines, "Line 1")
	lines = append(lines, "### Subsection Three")
	for i := 0; i < 20; i++ {
		lines = append(lines, "Body text")
	}

	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "no_h1.md")
	_ = os.WriteFile(mdPath, []byte(strings.Join(lines, "\n")), 0o644)

	r, err := NewReaderState(mdPath, tmp, 80, 20, false)
	if err != nil {
		t.Fatalf("NewReaderState failed: %v", err)
	}

	if len(r.Rendered.Headers) != 2 {
		t.Fatalf("expected 2 headers, got %d", len(r.Rendered.Headers))
	}
	if r.Rendered.Headers[0].Depth != 0 {
		t.Errorf("expected H2 (top-most level in file) to have Depth 0, got %d", r.Rendered.Headers[0].Depth)
	}
	if r.Rendered.Headers[1].Depth != 1 {
		t.Errorf("expected H3 (second level in file) to have Depth 1, got %d", r.Rendered.Headers[1].Depth)
	}

	// Verify sticky context header lines use relative indentation:
	// H2 has 1 leading margin space + 0 indent spaces; H3 has 1 leading margin space + 2 indent spaces
	r.Scroll = 5
	ctx := r.ActiveContextHeaders(18)
	if len(ctx) != 2 {
		t.Fatalf("expected 2 sticky context headers, got %d", len(ctx))
	}
	h2Sticky := r.renderContextHeaderLine(ctx[0], 50)
	h3Sticky := r.renderContextHeaderLine(ctx[1], 50)
	if !strings.Contains(h2Sticky, " "+fgH2+"Section Two") {
		t.Errorf("expected H2 sticky header to have 0 relative indentation, got: %q", h2Sticky)
	}
	if !strings.Contains(h3Sticky, "   "+fgH3+"Subsection Three") {
		t.Errorf("expected H3 sticky header to have 1 relative indentation (2 spaces), got: %q", h3Sticky)
	}

	// Verify sidebar rows also use relative indentation
	sbRow0 := r.renderSidebarRow(0, 30)
	sbRow1 := r.renderSidebarRow(1, 30)
	if !strings.Contains(sbRow0, "▸ "+fgH2+"Section Two") {
		t.Errorf("expected H2 sidebar row to have 0 relative indentation, got: %q", sbRow0)
	}
	if !strings.Contains(sbRow1, "     "+fgH3+"Subsection Three") {
		t.Errorf("expected H3 sidebar row to have 1 relative indentation, got: %q", sbRow1)
	}
}

func TestMarkdownTableRenderingAndMobileCropping(t *testing.T) {
	src := strings.Join([]string{
		"# Character Table",
		"",
		"| Character | Role | Status | Location | Notes |",
		"| :--- | :---: | ---: | :--- | :--- |",
		"| 孔乙己 | Scholar | Active | 魯鎮咸亨酒店 | [Profile](chapter-2.md) |",
		"| 掌櫃 | Shopkeeper | Active | 酒店大櫃檯 | Controls the tavern |",
	}, "\n")

	// 1. Wide screen (100 columns): fits without cropping
	docWide := RenderMarkdown(src, 100, false)
	if len(docWide.Tables) != 1 {
		t.Fatalf("expected 1 parsed table, got %d", len(docWide.Tables))
	}
	tblWide := docWide.Tables[0]
	if tblWide.IsCropped {
		t.Errorf("expected table not to be cropped at width 100")
	}
	if len(tblWide.Headers) != 5 || len(tblWide.Rows) != 2 {
		t.Fatalf("expected 5 columns and 2 rows, got %d cols × %d rows", len(tblWide.Headers), len(tblWide.Rows))
	}
	if tblWide.Alignments[0] != AlignLeft || tblWide.Alignments[1] != AlignCenter || tblWide.Alignments[2] != AlignRight {
		t.Errorf("unexpected column alignments: %+v", tblWide.Alignments)
	}
	for i, line := range docWide.Lines {
		if vw := visibleWidth(line); vw != 100 {
			t.Errorf("wide line %d visibleWidth = %d, want 100", i, vw)
		}
	}

	// 2. Narrow mobile screen (36 columns): cells wrap cleanly and wide columns are cropped with '▸'
	docNarrow := RenderMarkdown(src, 36, false)
	if len(docNarrow.Tables) != 1 {
		t.Fatalf("expected 1 parsed table on narrow width, got %d", len(docNarrow.Tables))
	}
	tblNarrow := docNarrow.Tables[0]
	if !tblNarrow.IsCropped {
		t.Errorf("expected wide 5-column table to be marked IsCropped at width 36")
	}
	foundCropMarker := false
	for i := tblNarrow.StartRenderLine; i <= tblNarrow.EndRenderLine; i++ {
		line := docNarrow.Lines[i]
		if vw := visibleWidth(line); vw != 36 {
			t.Errorf("narrow table line %d visibleWidth = %d, want 36", i, vw)
		}
		if strings.Contains(line, "▸") {
			foundCropMarker = true
		}
	}
	if !foundCropMarker {
		t.Errorf("expected cropped narrow table lines to display right-edge '▸' indicator")
	}
}

func TestSelectionModeAndInteractiveTableViewer(t *testing.T) {
	tmp := t.TempDir()
	ch1Path := filepath.Join(tmp, "chapter-1.md")
	ch2Path := filepath.Join(tmp, "chapter-2.md")
	notePath := filepath.Join(tmp, "notes.txt")
	binPath := filepath.Join(tmp, "image.bin")

	ch1Content := strings.Join([]string{
		"# Chapter 1",
		"",
		"Read [Chapter 2](chapter-2.md) or edit [Notes](notes.txt) or open [Binary](image.bin).",
		"",
		"| Item | Target | Description |",
		"| :--- | :--- | :--- |",
		"| First | [Go Ch2](chapter-2.md) | Link inside table cell |",
		"| Second | Plain cell | Normal cell text |",
	}, "\n")
	_ = os.WriteFile(ch1Path, []byte(ch1Content), 0o644)
	_ = os.WriteFile(ch2Path, []byte("# Chapter 2\n\nWelcome to chapter 2."), 0o644)
	_ = os.WriteFile(notePath, []byte("Plain text notes"), 0o644)
	_ = os.WriteFile(binPath, []byte{0x00, 0x01, 0x02, 0xff}, 0o644)

	app, err := NewAppModel(ch1Path)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Expect 4 selectable items in Chapter 1: 3 links + 1 table
	if len(app.Reader.Rendered.Selectables) != 4 {
		t.Fatalf("expected 4 selectable items (3 links + 1 table), got %d", len(app.Reader.Rendered.Selectables))
	}

	// 1. Toggle Selection Mode on with 's'
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !app.Reader.SelectionMode {
		t.Fatalf("expected SelectionMode to be true after pressing 's'")
	}
	if app.Reader.SelectedIdx != 0 {
		t.Fatalf("expected first selectable item (index 0) to be focused, got %d", app.Reader.SelectedIdx)
	}
	if !strings.Contains(app.View(), "[SELECT 1/4]") {
		t.Errorf("expected top bar to show [SELECT 1/4] badge in Selection Mode")
	}

	// 2. Press Enter on the first link (`[Chapter 2](chapter-2.md)`) -> should open chapter-2.md in treemouse
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.Reader.FilePath != ch2Path {
		t.Fatalf("expected Reader to navigate to %s, got %s", ch2Path, app.Reader.FilePath)
	}
	if len(app.Reader.History) != 1 {
		t.Fatalf("expected 1 entry in Reader back-history stack, got %d", len(app.Reader.History))
	}

	// 3. Press Esc in chapter-2.md -> should pop back to chapter-1.md (with SelectionMode restored)
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.Mode != ModeReader || app.Reader.FilePath != ch1Path {
		t.Fatalf("expected Esc to pop Reader history back to %s, got mode=%v path=%s", ch1Path, app.Mode, app.Reader.FilePath)
	}
	if !app.Reader.SelectionMode {
		t.Errorf("expected SelectionMode to be restored from history")
	}

	// 4. Move right/down to the second link (`[Notes](notes.txt)`) and press Enter -> should return openEditorCmd
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.Reader.SelectedIdx != 1 {
		t.Fatalf("expected SelectedIdx=1, got %d", app.Reader.SelectedIdx)
	}
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Errorf("expected pressing Enter on a text file link (notes.txt) to return $EDITOR tea.Cmd")
	}

	// 5. Move to the third link (`[Binary](image.bin)`) and press Enter with empty PATH (no xdg-open) -> Unsupported popup
	origPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", origPath) }()

	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.Reader.SelectedIdx != 2 {
		t.Fatalf("expected SelectedIdx=2, got %d", app.Reader.SelectedIdx)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.UnsupportedMsg == "" {
		t.Errorf("expected UnsupportedMsg popup when opening binary file without xdg-open")
	}
	if !strings.Contains(app.View(), "Unsupported Filetype") {
		t.Errorf("expected View() to render Unsupported Filetype popup")
	}
	// Dismiss popup with Esc
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.UnsupportedMsg != "" {
		t.Errorf("expected Esc to dismiss Unsupported Filetype popup")
	}

	// 6. Move to the 4th item (the Table) and press Enter -> should enter ModeTableViewer
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if app.Reader.SelectedIdx != 3 {
		t.Fatalf("expected SelectedIdx=3 (table), got %d", app.Reader.SelectedIdx)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.Mode != ModeTableViewer || app.TableViewer == nil {
		t.Fatalf("expected Enter on table to open ModeTableViewer, got mode=%v", app.Mode)
	}
	tvView := app.View()
	if !strings.Contains(tvView, "Table Viewer") {
		t.Errorf("expected Table Viewer header in View()")
	}

	// Pressing Enter on a non-link cell (Column 0: "First") opens the scrollable Cell Detail Popup
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !app.TableViewer.ShowCellPopup {
		t.Fatalf("expected Enter on non-link cell to open ShowCellPopup")
	}
	if !strings.Contains(app.View(), "Row 1 — Item") || !strings.Contains(app.View(), "First") {
		t.Errorf("expected Cell Detail Popup to show column title and full cell content")
	}
	// Pressing Esc closes the Cell Detail Popup and stays in ModeTableViewer
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.TableViewer.ShowCellPopup || app.Mode != ModeTableViewer {
		t.Fatalf("expected Esc to close Cell Detail Popup and remain in ModeTableViewer")
	}

	// Navigate right to Column 1 (`[Go Ch2](chapter-2.md)`) and press Enter inside Table Viewer -> opens chapter-2.md
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.TableViewer.CursorCol != 1 {
		t.Fatalf("expected TableViewer CursorCol=1, got %d", app.TableViewer.CursorCol)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.Mode != ModeReader || app.Reader.FilePath != ch2Path {
		t.Fatalf("expected Enter on link cell inside TableViewer to open %s in ModeReader, got mode=%v path=%s", ch2Path, app.Mode, app.Reader.FilePath)
	}
}

func TestYAMLFrontmatterHiddenByDefaultAndShownInSyntaxMode(t *testing.T) {
	src := strings.Join([]string{
		"---",
		"title: 狂人日記",
		"description: Meta header description",
		"tags:",
		"  - 小說",
		"  - 經典",
		"---",
		"",
		"# 狂人日記",
		"",
		"Body paragraph here.",
	}, "\n")

	// 1. Default mode (showSyntax == false): YAML frontmatter and trailing blank line are hidden
	docClean := RenderMarkdown(src, 80, false)
	joinedClean := strings.Join(docClean.Lines, "\n")
	if strings.Contains(joinedClean, "description: Meta header") || strings.Contains(joinedClean, "tags:") {
		t.Errorf("expected YAML frontmatter to be hidden by default, got:\n%s", joinedClean)
	}
	if len(docClean.Lines) != 3 {
		t.Fatalf("expected 3 rendered lines (H1, empty line, body paragraph), got %d:\n%s", len(docClean.Lines), joinedClean)
	}
	if !strings.Contains(docClean.Lines[0], "狂人日記") {
		t.Errorf("expected first rendered line to be H1 title without leading blank gap, got: %q", docClean.Lines[0])
	}

	// 2. Syntax mode (showSyntax == true): YAML frontmatter is displayed in dimmed style
	docSyntax := RenderMarkdown(src, 80, true)
	joinedSyntax := strings.Join(docSyntax.Lines, "\n")
	if !strings.Contains(joinedSyntax, "title: 狂人日記") || !strings.Contains(joinedSyntax, "tags:") {
		t.Errorf("expected YAML frontmatter to be visible when showSyntax is true, got:\n%s", joinedSyntax)
	}
	if !strings.Contains(docSyntax.Lines[0], "---") {
		t.Errorf("expected first line in syntax mode to be '---', got: %q", docSyntax.Lines[0])
	}
}

func TestMouseScrollHoverAndClickSupport(t *testing.T) {
	tmp := t.TempDir()
	aPath := filepath.Join(tmp, "a.md")
	bPath := filepath.Join(tmp, "b.md")

	var aLines []string
	aLines = append(aLines, "# Doc A")
	aLines = append(aLines, "")
	aLines = append(aLines, "Go to [Doc B](b.md) now.")
	aLines = append(aLines, "")
	aLines = append(aLines, "| Col1 | Col2 |")
	aLines = append(aLines, "| :--- | :--- |")
	aLines = append(aLines, "| CellA | [LinkB](b.md) |")
	aLines = append(aLines, "| CellC | PlainD |")
	for i := 0; i < 30; i++ {
		aLines = append(aLines, "Line content")
	}

	_ = os.WriteFile(aPath, []byte(strings.Join(aLines, "\n")), 0o644)
	_ = os.WriteFile(bPath, []byte("# Doc B\n\nSecond document."), 0o644)

	// Start in File Browser mode
	app, err := NewAppModel(tmp)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	// 1. File Browser: Hover on row Y=2 (second file entry `b.md`, 0-based index 1) highlights it
	app.Update(tea.MouseMsg{X: 5, Y: 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if app.Browser.Cursor != 1 {
		t.Fatalf("expected Browser hover at Y=2 to highlight entry index 1, got %d", app.Browser.Cursor)
	}

	// Simple mouse click (MouseActionPress) on row Y=1 (first file entry `a.md`, index 0) immediately opens `a.md` in Reader
	app.Update(tea.MouseMsg{X: 5, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if app.Mode != ModeReader || app.Reader == nil || app.Reader.FilePath != aPath {
		t.Fatalf("expected Browser mouse click at Y=1 to open a.md in ModeReader, got mode=%v", app.Mode)
	}

	// 2. Markdown Reader: Wheel down scrolls the document down, wheel up scrolls back to top
	app.Update(tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if app.Reader.Scroll <= 0 {
		t.Errorf("expected MouseButtonWheelDown in Reader to scroll down, got Scroll=%d", app.Reader.Scroll)
	}
	app.Update(tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if app.Reader.Scroll != 0 {
		t.Errorf("expected MouseButtonWheelUp in Reader to scroll back to 0, got Scroll=%d", app.Reader.Scroll)
	}

	// Hover over the table (rendered around line 4..8, so screen Y = docIdx + 1 = 6) highlights the table
	tblItem := app.Reader.Rendered.Selectables[1]
	tableScreenY := tblItem.StartLine + 1
	app.Update(tea.MouseMsg{X: 5, Y: tableScreenY, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if !app.Reader.SelectionMode || app.Reader.SelectedIdx != 1 {
		t.Fatalf("expected hovering over table at Y=%d to enable SelectionMode and focus table (idx 1), got mode=%v idx=%d",
			tableScreenY, app.Reader.SelectionMode, app.Reader.SelectedIdx)
	}

	// Simple mouse click (MouseActionPress) on the table opens ModeTableViewer immediately
	app.Update(tea.MouseMsg{X: 5, Y: tableScreenY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if app.Mode != ModeTableViewer || app.TableViewer == nil {
		t.Fatalf("expected mouse click on table to open ModeTableViewer, got mode=%v", app.Mode)
	}

	// 3. Table Viewer: Render once to populate cell spans, then hover & click on Row 1, Col 0 ("CellC")
	_ = app.View()
	if len(app.TableViewer.LastRowSpans) < 2 || len(app.TableViewer.LastColSpans) < 2 {
		t.Fatalf("expected TableViewer to record row and col spans, got rows=%d cols=%d",
			len(app.TableViewer.LastRowSpans), len(app.TableViewer.LastColSpans))
	}
	row1Y := app.TableViewer.LastRowSpans[1].StartY
	col0X := app.TableViewer.LastColSpans[0].StartX + 1

	app.Update(tea.MouseMsg{X: col0X, Y: row1Y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if app.TableViewer.CursorRow != 1 || app.TableViewer.CursorCol != 0 {
		t.Fatalf("expected TableViewer hover to highlight (row=1, col=0), got (%d, %d)",
			app.TableViewer.CursorRow, app.TableViewer.CursorCol)
	}

	// Simple mouse click (MouseActionPress) on (row=1, col=0) opens the Cell Detail Popup immediately
	app.Update(tea.MouseMsg{X: col0X, Y: row1Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !app.TableViewer.ShowCellPopup {
		t.Fatalf("expected clicking non-link cell in TableViewer to open ShowCellPopup")
	}

	// 4. Close Cell Detail Popup and TableViewer via their top-right × buttons, then click top-right × in Reader
	popupW, bodyRows := app.TableViewer.cellPopupDimensions(80, 20)
	startRow, _, rightCol := popupBounds(20, bodyRows+4, 80, popupW)
	app.Update(tea.MouseMsg{X: rightCol - 2, Y: startRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if app.TableViewer.ShowCellPopup {
		t.Fatalf("expected clicking top-right × of Cell Detail Popup to close it")
	}

	app.Update(tea.MouseMsg{X: 78, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if app.Mode != ModeReader || app.Reader == nil {
		t.Fatalf("expected clicking top-right × in TableViewer to return to ModeReader, got %v", app.Mode)
	}

	closeX := 80 - app.Reader.sidebarWidth(80) - 2
	app.Update(tea.MouseMsg{X: closeX, Y: 0, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if !app.Reader.CloseHovered {
		t.Errorf("expected hovering over top-right × at X=%d,Y=0 to set CloseHovered=true", closeX)
	}
	app.Update(tea.MouseMsg{X: closeX, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if app.Mode != ModeBrowser {
		t.Fatalf("expected clicking top-right × in Reader to close back to ModeBrowser, got %v", app.Mode)
	}

	// 5. Toggle Mouse Mode OFF via ":" command palette -> mouse events are no longer captured
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	for _, r := range "mouse" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, mouseCmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.MouseEnabled {
		t.Fatalf("expected MouseEnabled to be false after toggling mouse mode")
	}
	if mouseCmd == nil {
		t.Fatalf("expected toggling mouse mode off to return tea.DisableMouse cmd")
	}
	// Hovering or clicking in File Browser while MouseEnabled is false should have no effect
	app.Browser.Cursor = 0
	app.Update(tea.MouseMsg{X: 5, Y: 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	app.Update(tea.MouseMsg{X: 5, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if app.Browser.Cursor != 0 || app.Mode != ModeBrowser {
		t.Errorf("expected mouse events to be ignored when MouseEnabled is false, got Cursor=%d Mode=%v", app.Browser.Cursor, app.Mode)
	}
}

func TestReaderLiveTextSearchAndNavigation(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "search.md")

	var lines []string
	lines = append(lines, "# Search Document")
	lines = append(lines, "First line with TargetWord here.")
	for i := 0; i < 25; i++ {
		lines = append(lines, "Filler paragraph line.")
	}
	lines = append(lines, "Middle line with targetword in lowercase.")
	for i := 0; i < 25; i++ {
		lines = append(lines, "More filler paragraph line.")
	}
	lines = append(lines, "Final line with TARGETWORD in uppercase.")

	_ = os.WriteFile(mdPath, []byte(strings.Join(lines, "\n")), 0o644)

	app, err := NewAppModel(mdPath)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 16})

	// 1. Pressing "/" in Reader opens the bottom search bar (and does NOT open Command Menu)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if app.ShowCommandMenu {
		t.Fatalf("expected '/' in Reader not to open Command Menu")
	}
	if !app.Reader.SearchActive {
		t.Fatalf("expected '/' in Reader to activate bottom search bar")
	}
	if app.Reader.SearchCaseSensitive {
		t.Fatalf("expected search to be case-insensitive by default")
	}

	// 2. Search as user types "TargetWord" -> should match all 3 occurrences (case-insensitive)
	for _, r := range "TargetWord" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(app.Reader.SearchMatches) != 3 {
		t.Fatalf("expected 3 case-insensitive matches for 'TargetWord', got %d", len(app.Reader.SearchMatches))
	}
	if app.Reader.SearchMatchIdx != 0 {
		t.Fatalf("expected initial match index 0, got %d", app.Reader.SearchMatchIdx)
	}

	view := app.View()
	if !strings.Contains(stripANSI(view), "/ Search ") {
		t.Errorf("expected bottom search bar to display '/ Search ' label, got:\n%s", view)
	}
	if !strings.Contains(view, "1/3 matches") {
		t.Errorf("expected bottom search bar to display '1/3 matches', got:\n%s", view)
	}
	if !strings.Contains(view, "Prev/Next") {
		t.Errorf("expected bottom search bar to hint Prev/Next navigation, got:\n%s", view)
	}

	// 3. Navigate to next occurrence (Down arrow) and third occurrence (Enter)
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if app.Reader.SearchMatchIdx != 1 {
		t.Fatalf("expected Down arrow to advance to match index 1, got %d", app.Reader.SearchMatchIdx)
	}
	if app.Reader.Scroll == 0 {
		t.Errorf("expected Reader to scroll down to reveal second match")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.Reader.SearchMatchIdx != 2 {
		t.Fatalf("expected Enter to advance to match index 2, got %d", app.Reader.SearchMatchIdx)
	}
	scrollAtThirdMatch := app.Reader.Scroll

	// Navigate to previous occurrence (Up arrow) and back to third (Down arrow)
	app.Update(tea.KeyMsg{Type: tea.KeyUp})
	if app.Reader.SearchMatchIdx != 1 {
		t.Fatalf("expected Up arrow to go back to match index 1, got %d", app.Reader.SearchMatchIdx)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if app.Reader.SearchMatchIdx != 2 {
		t.Fatalf("expected Down arrow to return to match index 2, got %d", app.Reader.SearchMatchIdx)
	}
	scrollAtThirdMatch = app.Reader.Scroll

	// 4. Toggle case sensitivity with Tab -> only "TargetWord" (1 match) should match
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !app.Reader.SearchCaseSensitive {
		t.Fatalf("expected Tab in search bar to enable case-sensitive search")
	}
	if len(app.Reader.SearchMatches) != 1 {
		t.Fatalf("expected 1 exact-case match for 'TargetWord', got %d", len(app.Reader.SearchMatches))
	}

	// Toggle case sensitivity back off -> 3 matches again, jump to match 3
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if app.Reader.SearchCaseSensitive || len(app.Reader.SearchMatches) != 3 {
		t.Fatalf("expected Tab to restore case-insensitive search with 3 matches")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	scrollAtThirdMatch = app.Reader.Scroll

	// 5. Pressing Esc closes the search bar but keeps the final navigation scroll position
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.Reader.SearchActive {
		t.Fatalf("expected Esc to close the bottom search bar")
	}
	if app.Mode != ModeReader {
		t.Fatalf("expected Esc in search bar to stay in ModeReader, got %v", app.Mode)
	}
	if app.Reader.Scroll != scrollAtThirdMatch {
		t.Errorf("expected Esc to preserve final navigation scroll position %d, got %d", scrollAtThirdMatch, app.Reader.Scroll)
	}
}

func TestOSCWindowTitleReporting(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "docs")
	_ = os.MkdirAll(subDir, 0o755)
	mdPath := filepath.Join(subDir, "guide.md")
	_ = os.WriteFile(mdPath, []byte("# Guide\n\nContent."), 0o644)

	app, err := NewAppModel(tmp)
	if err != nil {
		t.Fatalf("NewAppModel failed: %v", err)
	}

	// In File Browser mode at tmp: title should be "treemouse | <base of tmp>"
	wantBrowserTitle := "treemouse | " + filepath.Base(tmp)
	if got := app.WindowTitle(); got != wantBrowserTitle {
		t.Fatalf("WindowTitle() in Browser = %q, want %q", got, wantBrowserTitle)
	}

	// Enter "docs" subfolder -> title updates to "treemouse | docs" and returns SetWindowTitle cmd
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := app.WindowTitle(); got != "treemouse | docs" {
		t.Fatalf("WindowTitle() after entering docs = %q, want %q", got, "treemouse | docs")
	}
	if cmd == nil {
		t.Errorf("expected non-nil tea.SetWindowTitle cmd when directory changes")
	}

	// Move cursor down past ".." to "guide.md" and press Enter -> title updates to "treemouse | guide.md"
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := app.WindowTitle(); got != "treemouse | guide.md" {
		t.Fatalf("WindowTitle() in Reader = %q, want %q", got, "treemouse | guide.md")
	}
	if cmd == nil {
		t.Errorf("expected non-nil tea.SetWindowTitle cmd when opening markdown file")
	}

	// Press Esc to close Reader back to File Browser -> title reverts to "treemouse | docs"
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := app.WindowTitle(); got != "treemouse | docs" {
		t.Fatalf("WindowTitle() after closing Reader = %q, want %q", got, "treemouse | docs")
	}
	if cmd == nil {
		t.Errorf("expected non-nil tea.SetWindowTitle cmd when returning to File Browser")
	}
}
