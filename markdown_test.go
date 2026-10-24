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

	// Expect "..", "sub", "readme.md", "script.py"
	if len(b.Entries) != 4 {
		t.Fatalf("expected 4 entries in ALL mode, got %d", len(b.Entries))
	}
	if !b.Entries[2].Supported {
		t.Errorf("expected readme.md to be supported")
	}
	if b.Entries[3].Supported {
		t.Errorf("expected script.py to be unsupported")
	}

	// Unsupported item can still be highlighted by cursor navigation
	b.MoveCursor(3, 20)
	sel, ok := b.Selected()
	if !ok || sel.Name != "script.py" {
		t.Errorf("expected cursor to highlight unsupported file script.py, got %+v", sel)
	}

	// Toggle filter to MD ONLY
	b.ToggleFilter()
	if !b.FilterMD {
		t.Errorf("expected FilterMD to be true")
	}
	if len(b.Entries) != 3 {
		t.Fatalf("expected 3 entries in MD ONLY mode (.., sub, readme.md), got %d", len(b.Entries))
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

	// Press Esc in Reader view -> pops back to Base Mode (File Browser)
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

	// Press Esc again, then 'Y' -> confirms quit
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
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
