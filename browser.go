package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileEntry represents a directory or file item in the File Browser.
type FileEntry struct {
	Name      string
	Path      string
	IsDir     bool
	IsParent  bool // true for ".."
	Supported bool // true for directories and supported Markdown files
}

// BrowserState holds the state for the File Browser (base mode).
type BrowserState struct {
	Dir        string
	AllEntries []FileEntry
	Entries    []FileEntry
	Cursor     int
	Scroll     int
	FilterMD   bool   // When true, only show folders and supported Markdown files
	StatusMsg  string // Ephemeral status message (e.g., when pressing Enter on unsupported file)
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
func NewBrowserState(dir string) (*BrowserState, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	b := &BrowserState{
		Dir: absDir,
	}
	if err := b.LoadDir(absDir, ""); err != nil {
		return nil, err
	}
	return b, nil
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
	if parent != absDir {
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

// GoParent navigates to the parent directory and highlights the directory just exited.
func (b *BrowserState) GoParent() error {
	parent := filepath.Dir(b.Dir)
	if parent == b.Dir {
		return nil
	}
	prevName := filepath.Base(b.Dir)
	return b.LoadDir(parent, prevName)
}

// RenderLines renders the full-screen File Browser (header bar, content rows, footer bar).
func (b *BrowserState) RenderLines(width, height int) []string {
	if width < 10 {
		width = 10
	}
	if height < 3 {
		height = 3
	}
	bodyHeight := height - 2
	b.EnsureVisible(bodyHeight)

	var lines []string

	// 1. Top Header Bar
	filterLabel := "[ALL] "
	if b.FilterMD {
		filterLabel = "[MD ONLY] "
	}
	headerLeft := " 📁 " + b.Dir
	lines = append(lines, styleBarWithRight(headerLeft, filterLabel, width, bgBar, fgBright+ansiBold, fgFilterBadge))

	// 2. Content Area
	for row := 0; row < bodyHeight; row++ {
		idx := b.Scroll + row
		if idx >= len(b.Entries) {
			lines = append(lines, styleLine("", width, bgMain, fgDefault))
			continue
		}
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

		lines = append(lines, styleLine(itemText, width, rowBg, fgDefault))
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
