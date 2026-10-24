package main

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileEntry represents a directory or file item in the File Browser.
type FileEntry struct {
	Name      string
	Path      string
	IsDir     bool
	IsParent  bool // true for ".."
	Supported bool // true for directories and supported Markdown files
}

// ItemMetadata holds the inspected metadata for the highlighted item in the File Browser sidebar.
type ItemMetadata struct {
	Name        string
	Kind        string
	MIMEType    string
	Size        string
	Permissions string
	Created     string
	Modified    string
}

// BrowserState holds the state for the File Browser (base mode) and its Info Sidebar.
type BrowserState struct {
	RootDir         string // Top-level bound directory; cannot navigate above this
	Dir             string
	AllEntries      []FileEntry
	Entries         []FileEntry
	Cursor          int
	Scroll          int
	FilterMD        bool   // When true, only show folders and supported Markdown files
	StatusMsg       string // Ephemeral status message (e.g., when pressing Enter on unsupported file)
	SidebarOpen     bool
	SidebarFocused  bool
	IsNarrow        bool
	CachedMetaPath  string
	CachedMetaMtime time.Time
	CachedMeta      ItemMetadata
}

// IsSupportedMarkdown checks whether a filename is a supported Markdown file.
func IsSupportedMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// NaturalLess compares two file or folder names using human-friendly natural sorting:
// - Splits stem and extension so base names are compared before extensions
//   (e.g., "chapter-8.md" comes before "chapter-8a.md" and "chapter-8-1.md").
// - Compares contiguous digit runs numerically (e.g., "chapter-8" < "chapter-9" < "chapter-10",
//   and "chapter-9-1" < "chapter-9-2" < "chapter-10-1").
func NaturalLess(a, b string) bool {
	stemA, extA := splitNameAndExt(a)
	stemB, extB := splitNameAndExt(b)

	if cmp := compareNaturalChunked(stemA, stemB); cmp != 0 {
		return cmp < 0
	}
	if cmp := compareNaturalChunked(extA, extB); cmp != 0 {
		return cmp < 0
	}
	return a < b
}

func splitNameAndExt(name string) (string, string) {
	// Keep dotfiles like ".gitignore" as stem without treating the whole name as an extension
	ext := filepath.Ext(name)
	if ext != "" && ext != name {
		return name[:len(name)-len(ext)], ext
	}
	return name, ""
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func compareNaturalChunked(a, b string) int {
	ia, ib := 0, 0
	na, nb := len(a), len(b)

	for ia < na && ib < nb {
		da := isDigit(a[ia])
		db := isDigit(b[ib])

		if da && db {
			// Extract full numeric runs
			startA := ia
			for ia < na && isDigit(a[ia]) {
				ia++
			}
			startB := ib
			for ib < nb && isDigit(b[ib]) {
				ib++
			}

			numA := a[startA:ia]
			numB := b[startB:ib]

			// Strip leading zeros for magnitude comparison
			trimA := strings.TrimLeft(numA, "0")
			trimB := strings.TrimLeft(numB, "0")

			if len(trimA) != len(trimB) {
				if len(trimA) < len(trimB) {
					return -1
				}
				return 1
			}
			if trimA != trimB {
				if trimA < trimB {
					return -1
				}
				return 1
			}
			// Same numeric value (e.g. "08" vs "8"): fewer leading zeros or shorter run tie-break
			if len(numA) != len(numB) {
				if len(numA) < len(numB) {
					return -1
				}
				return 1
			}
			continue
		}

		// Compare non-digit characters case-insensitively
		ca := a[ia]
		cb := b[ib]
		la := toLowerASCII(ca)
		lb := toLowerASCII(cb)
		if la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
		ia++
		ib++
	}

	if ia < na {
		return 1
	}
	if ib < nb {
		return -1
	}
	return 0
}

func toLowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// NewBrowserState creates a BrowserState initialized at the given directory.
// An optional rootDir specifies the top-level bound directory (defaults to dir if omitted).
func NewBrowserState(dir string, rootDir ...string) (*BrowserState, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	boundRoot := absDir
	if len(rootDir) > 0 && rootDir[0] != "" {
		if absRoot, err := filepath.Abs(rootDir[0]); err == nil {
			boundRoot = absRoot
		}
	}
	b := &BrowserState{
		RootDir:     boundRoot,
		Dir:         absDir,
		SidebarOpen: true, // Default open on wide screens; OnResize adjusts if narrow
		IsNarrow:    false,
	}
	if err := b.LoadDir(absDir, ""); err != nil {
		return nil, err
	}
	return b, nil
}

// isWithinRoot reports whether targetPath is inside or equal to rootPath.
func isWithinRoot(targetPath, rootPath string) bool {
	if rootPath == "" {
		return true
	}
	rel, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// CanGoParent returns true if the current directory is strictly below RootDir.
func (b *BrowserState) CanGoParent() bool {
	parent := filepath.Dir(b.Dir)
	if parent == b.Dir || b.Dir == b.RootDir {
		return false
	}
	return isWithinRoot(parent, b.RootDir)
}

// OnResize updates responsive state for the File Browser sidebar when terminal dimensions change.
func (b *BrowserState) OnResize(width, height int) {
	wasNarrow := b.IsNarrow
	b.IsNarrow = width < ResponsiveBreakpoint

	if b.IsNarrow && !wasNarrow {
		// Automatically collapse sidebar when transitioning to narrow/mobile screen
		b.SidebarOpen = false
		b.SidebarFocused = false
	} else if !b.IsNarrow && wasNarrow {
		// Re-open sidebar on wide screen
		b.SidebarOpen = true
		b.SidebarFocused = false
	}
}

// ToggleSidebar expands or collapses the Item Info Sidebar on both mobile and desktop sizes.
func (b *BrowserState) ToggleSidebar() {
	b.SidebarOpen = !b.SidebarOpen
	if b.SidebarOpen {
		if b.IsNarrow {
			b.SidebarFocused = true
		}
	} else {
		b.SidebarFocused = false
	}
}

// ToggleFocus switches focus between the file list and the info sidebar.
func (b *BrowserState) ToggleFocus() {
	if !b.SidebarOpen {
		b.SidebarOpen = true
		b.SidebarFocused = true
		return
	}
	if b.IsNarrow {
		b.SidebarOpen = false
		b.SidebarFocused = false
		return
	}
	b.SidebarFocused = !b.SidebarFocused
}

func (b *BrowserState) sidebarWidth(totalWidth int) int {
	if !b.SidebarOpen || b.IsNarrow {
		return 0
	}
	sw := totalWidth * 32 / 100
	if sw < 28 {
		sw = 28
	}
	if sw > 40 {
		sw = 40
	}
	if sw > totalWidth/2 {
		sw = totalWidth / 2
	}
	return sw
}

// LoadDir reads the directory entries, sorts folders first then files, and optionally highlights selectName.
func (b *BrowserState) LoadDir(dir string, selectName string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	dirEntries, err := os.ReadDir(absDir)
	if err != nil {
		return err
	}

	b.Dir = absDir
	b.StatusMsg = ""
	b.CachedMetaPath = ""

	var dirs []FileEntry
	var files []FileEntry

	for _, de := range dirEntries {
		name := de.Name()
		fullPath := filepath.Join(absDir, name)
		isDir := de.IsDir()
		if de.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(fullPath); err == nil {
				isDir = info.IsDir()
			}
		}

		if isDir {
			dirs = append(dirs, FileEntry{
				Name:      name,
				Path:      fullPath,
				IsDir:     true,
				Supported: true,
			})
		} else {
			files = append(files, FileEntry{
				Name:      name,
				Path:      fullPath,
				IsDir:     false,
				Supported: IsSupportedMarkdown(name),
			})
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return NaturalLess(dirs[i].Name, dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return NaturalLess(files[i].Name, files[j].Name)
	})

	var all []FileEntry
	parent := filepath.Dir(absDir)
	if parent != absDir && absDir != b.RootDir && isWithinRoot(parent, b.RootDir) {
		all = append(all, FileEntry{
			Name:      "..",
			Path:      parent,
			IsDir:     true,
			IsParent:  true,
			Supported: true,
		})
	}
	all = append(all, dirs...)
	all = append(all, files...)

	b.AllEntries = all
	b.applyFilter(selectName)
	return nil
}

// ToggleFilter flips the Markdown-only filter while keeping the current item selected if visible.
func (b *BrowserState) ToggleFilter() {
	selectedName := ""
	if b.Cursor >= 0 && b.Cursor < len(b.Entries) {
		selectedName = b.Entries[b.Cursor].Name
	}
	b.FilterMD = !b.FilterMD
	b.StatusMsg = ""
	b.applyFilter(selectedName)
}

func (b *BrowserState) applyFilter(selectName string) {
	var filtered []FileEntry
	for _, e := range b.AllEntries {
		if b.FilterMD && !e.IsDir && !e.Supported {
			continue
		}
		filtered = append(filtered, e)
	}
	b.Entries = filtered
	b.Cursor = 0
	b.Scroll = 0
	if selectName != "" {
		for i, e := range b.Entries {
			if e.Name == selectName {
				b.Cursor = i
				break
			}
		}
	}
}

// MoveCursor moves the selection cursor by delta and adjusts viewport scroll for viewHeight.
func (b *BrowserState) MoveCursor(delta int, viewHeight int) {
	b.StatusMsg = ""
	if len(b.Entries) == 0 {
		b.Cursor = 0
		b.Scroll = 0
		return
	}
	b.Cursor += delta
	if b.Cursor < 0 {
		b.Cursor = 0
	}
	if b.Cursor >= len(b.Entries) {
		b.Cursor = len(b.Entries) - 1
	}
	b.EnsureVisible(viewHeight)
}

// EnsureVisible keeps Cursor within the visible [Scroll, Scroll+viewHeight) window.
func (b *BrowserState) EnsureVisible(viewHeight int) {
	if viewHeight < 1 {
		viewHeight = 1
	}
	if b.Cursor < b.Scroll {
		b.Scroll = b.Cursor
	}
	if b.Cursor >= b.Scroll+viewHeight {
		b.Scroll = b.Cursor - viewHeight + 1
	}
	maxScroll := len(b.Entries) - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if b.Scroll > maxScroll {
		b.Scroll = maxScroll
	}
	if b.Scroll < 0 {
		b.Scroll = 0
	}
}

// Selected returns the currently highlighted FileEntry, if any.
func (b *BrowserState) Selected() (FileEntry, bool) {
	if b.Cursor >= 0 && b.Cursor < len(b.Entries) {
		return b.Entries[b.Cursor], true
	}
	return FileEntry{}, false
}

// GoParent navigates to the parent directory and highlights the directory just exited,
// bounded by b.RootDir.
func (b *BrowserState) GoParent() error {
	if !b.CanGoParent() {
		return nil
	}
	parent := filepath.Dir(b.Dir)
	prevName := filepath.Base(b.Dir)
	return b.LoadDir(parent, prevName)
}

// SelectedMetadata returns the inspected ItemMetadata for the currently highlighted entry.
func (b *BrowserState) SelectedMetadata() ItemMetadata {
	entry, ok := b.Selected()
	if !ok {
		return ItemMetadata{}
	}
	info, err := os.Stat(entry.Path)
	if err == nil && b.CachedMetaPath == entry.Path && b.CachedMetaMtime.Equal(info.ModTime()) {
		return b.CachedMeta
	}

	meta := inspectEntryMetadata(entry, info, err)
	if err == nil {
		b.CachedMetaPath = entry.Path
		b.CachedMetaMtime = info.ModTime()
		b.CachedMeta = meta
	}
	return meta
}

func inspectEntryMetadata(entry FileEntry, info os.FileInfo, statErr error) ItemMetadata {
	name := entry.Name
	if entry.IsParent {
		name = ".. (" + filepath.Base(entry.Path) + ")"
	} else if entry.IsDir {
		name = entry.Name + "/"
	}

	if statErr != nil || info == nil {
		return ItemMetadata{
			Name:        name,
			Kind:        "Unknown",
			MIMEType:    "unknown",
			Size:        "-",
			Permissions: "-",
			Created:     "-",
			Modified:    "-",
		}
	}

	modTime := info.ModTime()
	createdTime := getCreationTime(entry.Path, modTime)
	perm := fmt.Sprintf("%s (%04o)", info.Mode().Perm().String(), info.Mode().Perm())

	var kind, mimeType, sizeStr string
	if info.IsDir() {
		if entry.IsParent {
			kind = "Parent Directory"
		} else {
			kind = "Directory"
		}
		mimeType = "inode/directory"
		if entries, err := os.ReadDir(entry.Path); err == nil {
			sizeStr = fmt.Sprintf("%d items (%s)", len(entries), formatByteSize(info.Size()))
		} else {
			sizeStr = formatByteSize(info.Size())
		}
	} else {
		if entry.Supported {
			kind = "Markdown Document"
		} else {
			kind = "File (Unsupported)"
		}
		mimeType = detectFileMIMEType(entry.Path)
		sizeStr = formatByteSize(info.Size())
	}

	return ItemMetadata{
		Name:        name,
		Kind:        kind,
		MIMEType:    mimeType,
		Size:        sizeStr,
		Permissions: perm,
		Created:     createdTime.Format("2006-01-02 15:04:05"),
		Modified:    modTime.Format("2006-01-02 15:04:05"),
	}
}

func detectFileMIMEType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return "text/markdown"
	case ".py":
		return "text/x-python"
	case ".go":
		return "text/x-go"
	case ".json":
		return "application/json"
	case ".toml":
		return "application/toml"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".sh":
		return "text/x-shellscript"
	case ".webvtt", ".vtt":
		return "text/vtt"
	case ".txt":
		return "text/plain"
	}

	if byExt := mime.TypeByExtension(ext); byExt != "" {
		if semi := strings.IndexByte(byExt, ';'); semi > 0 {
			return strings.TrimSpace(byExt[:semi])
		}
		return byExt
	}

	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()

	var buf [512]byte
	n, _ := f.Read(buf[:])
	if n == 0 {
		return "inode/x-empty"
	}
	detected := http.DetectContentType(buf[:n])
	if semi := strings.IndexByte(detected, ';'); semi > 0 {
		return strings.TrimSpace(detected[:semi])
	}
	return detected
}

func formatByteSize(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(1024), 0
	for n := b / 1024; n >= 1024 && exp < 5; n /= 1024 {
		div *= 1024
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	return fmt.Sprintf("%.1f %s (%d B)", float64(b)/float64(div), units[exp], b)
}

// buildInfoSidebarRows formats the metadata lines for the given sidebar width.
func (b *BrowserState) buildInfoSidebarRows(sw int) []string {
	meta := b.SelectedMetadata()
	if meta.Name == "" {
		return []string{styleLine(" (no selection)", sw, bgSidebar, fgMuted)}
	}

	type field struct {
		label string
		val   string
		valFg string
	}

	nameFg := fgBright + ansiBold
	if entry, ok := b.Selected(); ok {
		if entry.IsDir {
			nameFg = fgFolder
		} else if !entry.Supported {
			nameFg = fgUnsupported
		} else {
			nameFg = fgActiveCursor
		}
	}

	fields := []field{
		{"Name", meta.Name, nameFg},
		{"Type", meta.Kind, fgDefault},
		{"MIME Type", meta.MIMEType, fgH2},
		{"Size", meta.Size, fgH3},
		{"Permissions", meta.Permissions, fgDefault},
		{"Created", meta.Created, fgDefault},
		{"Modified", meta.Modified, fgDefault},
	}

	var rows []string
	rows = append(rows, styleLine("", sw, bgSidebar, fgDefault))
	for _, f := range fields {
		rows = append(rows, styleLine(" "+fgMuted+f.label, sw, bgSidebar, fgMuted))
		rows = append(rows, styleLine("   "+f.valFg+f.val, sw, bgSidebar, fgDefault))
		rows = append(rows, styleLine("", sw, bgSidebar, fgDefault))
	}
	return rows
}

// RenderLines renders the full-screen File Browser (including wide split sidebar or narrow full-screen overlay).
func (b *BrowserState) RenderLines(width, height int) []string {
	if width < 10 {
		width = 10
	}
	if height < 3 {
		height = 3
	}
	b.OnResize(width, height)
	bodyHeight := height - 2

	// Case 1: Narrow / Mobile mode with Sidebar expanded -> Full-screen overlay
	if b.IsNarrow && b.SidebarOpen {
		return b.renderMobileSidebarOverlay(width, height)
	}

	sw := b.sidebarWidth(width)
	mainW := width - sw
	b.EnsureVisible(bodyHeight)

	var lines []string

	// 1. Top Header Bar
	filterLabel := "[ALL] "
	if b.FilterMD {
		filterLabel = "[MD ONLY] "
	}
	headerLeft := " 📁 " + b.Dir
	mainHeader := styleBarWithRight(headerLeft, filterLabel, mainW, bgBar, fgBright+ansiBold, fgFilterBadge)

	if sw > 0 {
		sbHeaderTitle := " Item Info"
		if b.SidebarFocused {
			sbHeaderTitle = " ▸ Item Info"
		}
		sbHeader := styleLine(sbHeaderTitle, sw, bgSidebarHeader, fgSidebarTitle)
		lines = append(lines, mainHeader+sbHeader)
	} else {
		lines = append(lines, mainHeader)
	}

	var sbRows []string
	if sw > 0 {
		sbRows = b.buildInfoSidebarRows(sw)
	}

	// 2. Content Area
	for row := 0; row < bodyHeight; row++ {
		idx := b.Scroll + row
		var mainLine string
		if idx >= len(b.Entries) {
			mainLine = styleLine("", mainW, bgMain, fgDefault)
		} else {
			entry := b.Entries[idx]
			isSelected := idx == b.Cursor

			rowBg := bgMain
			prefix := "   "
			if isSelected {
				rowBg = bgHighlight
				prefix = " " + fgActiveCursor + "▸ "
			}

			var itemText string
			if entry.IsParent {
				if isSelected {
					itemText = prefix + fgActiveCursor + "📁 .. (Parent Directory)"
				} else {
					itemText = prefix + fgFolder + "📁 .."
				}
			} else if entry.IsDir {
				if isSelected {
					itemText = prefix + fgActiveCursor + "📁 " + entry.Name + "/"
				} else {
					itemText = prefix + fgFolder + "📁 " + entry.Name + "/"
				}
			} else if entry.Supported {
				if isSelected {
					itemText = prefix + fgActiveCursor + "📄 " + entry.Name
				} else {
					itemText = prefix + fgDefault + "📄 " + entry.Name
				}
			} else {
				// Unsupported file: distinct dimmed color, still highlightable during navigation
				if isSelected {
					itemText = prefix + fgUnsupported + "📄 " + entry.Name + " (unsupported)"
				} else {
					itemText = prefix + fgUnsupported + "📄 " + entry.Name
				}
			}

			mainLine = styleLine(itemText, mainW, rowBg, fgDefault)
		}

		if sw > 0 {
			var sbLine string
			if row < len(sbRows) {
				sbLine = sbRows[row]
			} else {
				sbLine = styleLine("", sw, bgSidebar, fgDefault)
			}
			lines = append(lines, mainLine+sbLine)
		} else {
			lines = append(lines, mainLine)
		}
	}

	// 3. Bottom Footer Bar (only essential features: Open, Commands, Quit)
	var footerText string
	if b.StatusMsg != "" {
		footerText = " " + fgWarn + b.StatusMsg
	} else {
		footerText = formatFooterHints(bgBar,
			HintPair{Key: "Enter", Desc: "Open"},
			HintPair{Key: "/", Desc: "Commands"},
			HintPair{Key: "Esc", Desc: "Quit"},
		)
	}
	lines = append(lines, styleLine(footerText, width, bgBar, fgMuted))

	return lines
}

func (b *BrowserState) renderMobileSidebarOverlay(width, height int) []string {
	bodyHeight := height - 2
	var lines []string
	meta := b.SelectedMetadata()
	headerTitle := " Item Info " + meta.Name
	lines = append(lines, styleBarWithRight(headerTitle, "[Overlay] ", width, bgSidebarHeader, fgSidebarTitle, fgMuted))

	sbRows := b.buildInfoSidebarRows(width)
	for row := 0; row < bodyHeight; row++ {
		if row < len(sbRows) {
			lines = append(lines, sbRows[row])
		} else {
			lines = append(lines, styleLine("", width, bgSidebar, fgDefault))
		}
	}

	footer := formatFooterHints(bgSidebarHeader,
		HintPair{Key: "/", Desc: "Commands"},
		HintPair{Key: "Esc", Desc: "Quit"},
	)
	lines = append(lines, styleLine(footer, width, bgSidebarHeader, fgMuted))
	return lines
}
