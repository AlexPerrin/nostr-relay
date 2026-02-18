package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Best-effort cleanup: close any open relay connection.
	if m, ok := finalModel.(model); ok && m.feed.client != nil {
		m.feed.client.Close()
	}
}
