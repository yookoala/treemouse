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

	// At top-level bound (tmp), ".." should NOT be shown: expect "sub", "readme.md", "script.py"
	if len(b.Entries) != 3 {
		t.Fatalf("expected 3 entries in ALL mode at top-level bound, got %d", len(b.Entries))
	}
	if !b.Entries[1].Supported {
		t.Errorf("expected readme.md to be supported")
	}
	if b.Entries[2].Supported {
		t.Errorf("expected script.py to be unsupported")
	}

	// Unsupported item can still be highlighted by cursor navigation
	b.MoveCursor(2, 20)
	sel, ok := b.Selected()
	if !ok || sel.Name != "script.py" {
		t.Errorf("expected cursor to highlight unsupported file script.py, got %+v", sel)
	}

	// Toggle filter to MD ONLY
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

	// Check minimal footer in Reader view (without colons, using bright key + darker description)
	view := app.View()
	if !strings.Contains(view, "Commands") || !strings.Contains(view, "Quit") {
		t.Errorf("expected minimal footer with 'Commands' and 'Quit', got:\n%s", view)
	}
	if strings.Contains(view, "/:") || strings.Contains(view, "Esc:") {
		t.Errorf("expected footer key hints to not contain ':', got:\n%s", view)
	}
	if strings.Contains(view, "PgUp/PgDn") || strings.Contains(view, "Toggle Sidebar") {
		t.Errorf("expected secondary features to be hidden from bottom footer")
	}

	// Open Commands popup with "/"
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !app.ShowCommandMenu {
		t.Fatalf("expected '/' to open Command Menu popup")
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
