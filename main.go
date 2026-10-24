package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	target := ""
	if len(os.Args) > 1 {
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Println("Usage: treemouse [path/to/file.md | path/to/folder]")
			fmt.Println()
			fmt.Println("A responsive terminal folder & Markdown browser built with Bubble Tea.")
			fmt.Println("Without arguments, opens the full-screen File Browser in the current folder.")
			os.Exit(0)
		}
		target = os.Args[1]
	}

	model, err := NewAppModel(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running treemouse: %v\n", err)
		os.Exit(1)
	}
}
